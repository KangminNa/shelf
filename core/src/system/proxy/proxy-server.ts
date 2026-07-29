import * as http from 'node:http'
import * as https from 'node:https'
import * as tls from 'node:tls'
import * as net from 'node:net'
import { readFileSync, existsSync } from 'node:fs'
import type { Logger } from '../../services/log.js'
import { certDomains, type ProxyHostRepository, type SslCertRepository, type AccessLogRepository, type ProxyHost } from './repositories.js'

const ACME_CHALLENGE_PREFIX = '/.well-known/acme-challenge/'
const HSTS_MAX_AGE_SECONDS = 63072000

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

  reloadHosts(): void {
    this.hosts = this.hostRepo.enabled()
    this.log.info(`loaded ${this.hosts.length} proxy hosts`)
  }

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

  setAcmeChallenge(token: string, keyAuthorization: string): void {
    this.acmeChallenges.set(token, keyAuthorization)
  }

  removeAcmeChallenge(token: string): void {
    this.acmeChallenges.delete(token)
  }

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
    const bootstrapCert = this.certRepo.allSorted().find((c) => existsSync(c.cert_path) && existsSync(c.key_path))
    if (!bootstrapCert) return
    try {
      this.httpsServer = https.createServer(
        {
          cert: readFileSync(bootstrapCert.cert_path),
          key: readFileSync(bootstrapCert.key_path),
          SNICallback: (servername, cb) => cb(null, this.contextFor(servername)),
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
        const context = tls.createSecureContext({
          cert: readFileSync(cert.cert_path),
          key: readFileSync(cert.key_path),
        })
        for (const domain of certDomains(cert)) contexts.set(domain.toLowerCase(), context)
      } catch (err: any) {
        this.log.warn(`failed to load cert for ${cert.domain}: ${err.message}`)
      }
    }
    return contexts
  }

  private contextFor(servername: string): tls.SecureContext | undefined {
    const name = servername.toLowerCase()
    return this.secureContexts.get(name) ?? this.secureContexts.get(ProxyServer.wildcardOf(name))
  }

  private static wildcardOf(name: string): string {
    const dot = name.indexOf('.')
    return dot === -1 ? name : `*${name.slice(dot)}`
  }

  private findHost(hostname: string): ProxyHost | undefined {
    const bare = hostname.split(':')[0].toLowerCase()
    return this.hosts.find((h) => h.domain.toLowerCase() === bare)
  }

  private handleRequest(req: http.IncomingMessage, res: http.ServerResponse): void {
    if (this.serveAcmeChallenge(req, res)) return

    const hostname = req.headers.host || ''
    const host = this.findHost(hostname)
    if (!host) {
      this.rejectUnknownHost(res, hostname)
      return
    }

    const encrypted = (req.socket as tls.TLSSocket).encrypted === true
    if (host.force_ssl && !encrypted) {
      this.redirectToHttps(req, res, host)
      return
    }

    this.forward(req, res, host, encrypted)
  }

  private serveAcmeChallenge(req: http.IncomingMessage, res: http.ServerResponse): boolean {
    if (!req.url?.startsWith(ACME_CHALLENGE_PREFIX)) return false
    const answer = this.acmeChallenges.get(req.url.split('/').pop() || '')
    if (!answer) return false
    res.writeHead(200, { 'content-type': 'text/plain' })
    res.end(answer)
    return true
  }

  private rejectUnknownHost(res: http.ServerResponse, hostname: string): void {
    const safe = escapeHtml(hostname)
    res.writeHead(502, { 'content-type': 'text/html; charset=utf-8' })
    res.end(`<h1>502 Bad Gateway</h1><p>No proxy host configured for <code>${safe}</code></p>`)
  }

  private redirectToHttps(req: http.IncomingMessage, res: http.ServerResponse, host: ProxyHost): void {
    res.writeHead(301, { Location: `https://${host.domain}${req.url}` })
    res.end()
  }

  private forward(req: http.IncomingMessage, res: http.ServerResponse, host: ProxyHost, encrypted: boolean): void {
    const startedAt = Date.now()
    const targetUrl = `${host.target_scheme}://${host.target_host}:${host.target_port}${req.url}`
    const transport = host.target_scheme === 'https' ? https : http

    const upstream = transport.request(
      targetUrl,
      { method: req.method, headers: this.forwardHeaders(req, host, encrypted) },
      (upstreamRes) => {
        res.writeHead(upstreamRes.statusCode || 502, this.responseHeaders(upstreamRes.headers, host, encrypted))
        upstreamRes.pipe(res)
        this.recordAccess(host, req, upstreamRes.statusCode || 502, Date.now() - startedAt)
      }
    )

    upstream.on('error', (err) => {
      this.log.error(`proxy error for ${host.domain}: ${err.message}`)
      res.writeHead(502, { 'content-type': 'text/html; charset=utf-8' })
      res.end(`<h1>502 Bad Gateway</h1><p>Cannot reach <code>${host.target_host}:${host.target_port}</code></p>`)
      this.recordAccess(host, req, 502, Date.now() - startedAt)
    })

    req.pipe(upstream)
  }

  private forwardHeaders(req: http.IncomingMessage, host: ProxyHost, encrypted: boolean): http.OutgoingHttpHeaders {
    return {
      ...req.headers,
      host: `${host.target_host}:${host.target_port}`,
      'x-real-ip': req.socket.remoteAddress || '',
      'x-forwarded-for': req.socket.remoteAddress || '',
      'x-forwarded-proto': encrypted ? 'https' : 'http',
      'x-forwarded-host': host.domain,
    }
  }

  private responseHeaders(upstream: http.IncomingHttpHeaders, host: ProxyHost, encrypted: boolean): http.IncomingHttpHeaders {
    if (!encrypted || !host.hsts_enabled) return upstream
    return {
      ...upstream,
      'strict-transport-security': `max-age=${HSTS_MAX_AGE_SECONDS}${host.hsts_subdomains ? '; includeSubDomains' : ''}`,
    }
  }

  private handleUpgrade(req: http.IncomingMessage, socket: net.Socket, head: Buffer): void {
    const host = this.findHost(req.headers.host || '')
    if (!host) {
      socket.end()
      return
    }
    const target = net.connect(host.target_port, host.target_host, () => {
      const requestLine = `${req.method} ${req.url} HTTP/${req.httpVersion}\r\n`
      const headers = Object.entries(req.headers).map(([k, v]) => `${k}: ${v}`).join('\r\n')
      target.write(requestLine + headers + '\r\n\r\n')
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

function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (ch) => `&#${ch.charCodeAt(0)};`)
}
