import * as http from 'node:http'
import * as https from 'node:https'
import * as tls from 'node:tls'
import * as net from 'node:net'
import { readFileSync, existsSync } from 'node:fs'
import type { Logger } from '../../services/log.js'
import type { ProxyHostRepository, SslCertRepository, AccessLogRepository, ProxyHost } from './repositories.js'

/**
 * 리버스 프록시 서버.
 * - HTTP(기본 80) / HTTPS(기본 443, SNI로 도메인별 인증서)
 * - WebSocket 업그레이드 프록시
 * - ACME HTTP-01 챌린지 응답 (/.well-known/acme-challenge/*)
 * - 도메인 라우팅은 proxy_hosts 테이블 기반 (메모리 캐시)
 */
export class ProxyServer {
  readonly httpPort: number
  readonly httpsPort: number

  private hosts: ProxyHost[] = []
  private secureContexts = new Map<string, tls.SecureContext>()
  private readonly acmeChallenges = new Map<string, string>()
  private httpServer?: http.Server
  private httpsServer?: https.Server

  constructor(
    private readonly hostRepo: ProxyHostRepository,
    private readonly certRepo: SslCertRepository,
    private readonly logRepo: AccessLogRepository,
    private readonly log: Logger
  ) {
    this.httpPort = Number(process.env.PROXY_HTTP_PORT || 80)
    this.httpsPort = Number(process.env.PROXY_HTTPS_PORT || 443)
  }

  get httpsActive(): boolean {
    return !!this.httpsServer
  }

  get certificateCount(): number {
    return this.secureContexts.size
  }

  start(): void {
    this.reloadHosts()
    this.startHttp()
    this.reloadCertificates()
  }

  stop(): void {
    this.httpServer?.close()
    this.httpsServer?.close()
  }

  /** proxy_hosts 변경 후 호출 — 라우팅 캐시 갱신 */
  reloadHosts(): void {
    this.hosts = this.hostRepo.enabled()
    this.log.info(`loaded ${this.hosts.length} proxy hosts`)
  }

  /** ssl_certs 변경 후 호출 — 인증서 로드, 필요 시 HTTPS 서버 기동 */
  reloadCertificates(): void {
    this.secureContexts = this.loadSecureContexts()
    if (this.secureContexts.size === 0) {
      this.log.info('no SSL certs, HTTPS server not started')
      return
    }
    if (this.httpsServer) {
      this.log.info(`reloaded ${this.secureContexts.size} SSL certificates`)
      return
    }
    this.startHttps()
  }

  // --- ACME 챌린지 (SslManager가 사용) ---

  setAcmeChallenge(token: string, keyAuthorization: string): void {
    this.acmeChallenges.set(token, keyAuthorization)
  }

  removeAcmeChallenge(token: string): void {
    this.acmeChallenges.delete(token)
  }

  // --- 내부 ---

  private startHttp(): void {
    this.httpServer = http.createServer((req, res) => this.handleRequest(req, res))
    this.httpServer.on('upgrade', (req, socket, head) => this.handleUpgrade(req, socket as net.Socket, head))
    this.httpServer
      .listen(this.httpPort, '0.0.0.0', () => this.log.info(`proxy HTTP listening on :${this.httpPort}`))
      .on('error', (err: NodeJS.ErrnoException) => {
        if (err.code === 'EACCES') this.log.warn(`cannot bind :${this.httpPort} (permission denied)`)
        else if (err.code === 'EADDRINUSE') this.log.warn(`port ${this.httpPort} already in use`)
        else this.log.error(`HTTP server error: ${err.message}`)
      })
  }

  private startHttps(): void {
    const anyCert = this.certRepo.allSorted().find((c) => existsSync(c.cert_path) && existsSync(c.key_path))
    if (!anyCert) return
    try {
      this.httpsServer = https.createServer(
        {
          cert: readFileSync(anyCert.cert_path),
          key: readFileSync(anyCert.key_path),
          SNICallback: (servername, cb) => cb(null, this.secureContexts.get(servername) || undefined),
        },
        (req, res) => this.handleRequest(req, res)
      )
      this.httpsServer.on('upgrade', (req, socket, head) => this.handleUpgrade(req, socket as net.Socket, head))
      this.httpsServer.listen(this.httpsPort, '0.0.0.0', () => this.log.info(`proxy HTTPS listening on :${this.httpsPort}`))
    } catch (err: any) {
      this.log.warn(`HTTPS start failed: ${err.message}`)
    }
  }

  private loadSecureContexts(): Map<string, tls.SecureContext> {
    const contexts = new Map<string, tls.SecureContext>()
    for (const cert of this.certRepo.all()) {
      if (!cert.cert_path || !existsSync(cert.cert_path) || !cert.key_path || !existsSync(cert.key_path)) continue
      try {
        contexts.set(cert.domain, tls.createSecureContext({
          cert: readFileSync(cert.cert_path),
          key: readFileSync(cert.key_path),
        }))
      } catch (err: any) {
        this.log.warn(`failed to load cert for ${cert.domain}: ${err.message}`)
      }
    }
    return contexts
  }

  private findHost(hostname: string): ProxyHost | undefined {
    const bare = hostname.split(':')[0].toLowerCase()
    return this.hosts.find((h) => h.domain.toLowerCase() === bare)
  }

  private handleRequest(req: http.IncomingMessage, res: http.ServerResponse): void {
    // ACME HTTP-01 챌린지
    if (req.url?.startsWith('/.well-known/acme-challenge/')) {
      const token = req.url.split('/').pop() || ''
      const answer = this.acmeChallenges.get(token)
      if (answer) {
        res.writeHead(200, { 'content-type': 'text/plain' })
        res.end(answer)
        return
      }
    }

    const start = Date.now()
    const hostname = req.headers.host || ''
    const host = this.findHost(hostname)

    if (!host) {
      res.writeHead(502, { 'content-type': 'text/html; charset=utf-8' })
      res.end(`<h1>502 Bad Gateway</h1><p>No proxy host configured for <code>${hostname}</code></p>`)
      return
    }

    const encrypted = (req.socket as tls.TLSSocket).encrypted === true

    if (host.force_ssl && !encrypted) {
      res.writeHead(301, { Location: `https://${host.domain}${req.url}` })
      res.end()
      return
    }

    const targetUrl = `${host.target_scheme}://${host.target_host}:${host.target_port}${req.url}`
    const transport = host.target_scheme === 'https' ? https : http

    const proxyReq = transport.request(
      targetUrl,
      {
        method: req.method,
        headers: {
          ...req.headers,
          host: `${host.target_host}:${host.target_port}`,
          'x-real-ip': req.socket.remoteAddress || '',
          'x-forwarded-for': req.socket.remoteAddress || '',
          'x-forwarded-proto': encrypted ? 'https' : 'http',
          'x-forwarded-host': host.domain,
        },
      },
      (proxyRes) => {
        res.writeHead(proxyRes.statusCode || 502, proxyRes.headers)
        proxyRes.pipe(res)
        this.recordAccess(host, req, proxyRes.statusCode || 502, Date.now() - start)
      }
    )

    proxyReq.on('error', (err) => {
      this.log.error(`proxy error for ${host.domain}: ${err.message}`)
      res.writeHead(502, { 'content-type': 'text/html; charset=utf-8' })
      res.end(`<h1>502 Bad Gateway</h1><p>Cannot reach <code>${host.target_host}:${host.target_port}</code></p>`)
      this.recordAccess(host, req, 502, Date.now() - start)
    })

    req.pipe(proxyReq)
  }

  private handleUpgrade(req: http.IncomingMessage, socket: net.Socket, head: Buffer): void {
    const host = this.findHost(req.headers.host || '')
    if (!host) {
      socket.end()
      return
    }
    const target = net.connect(host.target_port, host.target_host, () => {
      const reqLine = `${req.method} ${req.url} HTTP/${req.httpVersion}\r\n`
      const headers = Object.entries(req.headers).map(([k, v]) => `${k}: ${v}`).join('\r\n')
      target.write(reqLine + headers + '\r\n\r\n')
      if (head.length) target.write(head)
      target.pipe(socket)
      socket.pipe(target)
    })
    target.on('error', () => socket.end())
    socket.on('error', () => target.end())
  }

  private recordAccess(host: ProxyHost, req: http.IncomingMessage, status: number, durationMs: number): void {
    this.logRepo.record({
      domain: host.domain,
      method: req.method || 'GET',
      path: req.url || '/',
      status,
      duration_ms: durationMs,
      ip: req.socket.remoteAddress || '',
      user_agent: (req.headers['user-agent'] as string) || '',
    })
  }
}
