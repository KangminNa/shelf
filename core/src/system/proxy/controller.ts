import { Hono } from 'hono'
import type { Context } from 'hono'
import type { Logger } from '../../services/log.js'
import type { EventBus } from '../../services/events.js'
import type { ProxyServer } from './proxy-server.js'
import { SslError, type SslManager } from './ssl-manager.js'
import type { ProxyHostRepository, SslCertRepository, AccessLogRepository, ProxyHost } from './repositories.js'
import { hostsPage, sslPage, logsPage } from './views.js'

const HOST_FIELDS = ['domain', 'target_scheme', 'target_host', 'target_port', 'ssl_enabled', 'force_ssl', 'enabled', 'description'] as const

/**
 * Proxy 관리 HTTP 인터페이스.
 * - api: /api/proxy/* (JSON)
 * - pages: /admin/proxy/* (셸 래핑되는 HTML 조각)
 */
export class ProxyController {
  readonly api = new Hono()
  readonly pages = new Hono()

  constructor(
    private readonly hosts: ProxyHostRepository,
    private readonly certs: SslCertRepository,
    private readonly logs: AccessLogRepository,
    private readonly server: ProxyServer,
    private readonly ssl: SslManager,
    private readonly events: EventBus,
    private readonly log: Logger
  ) {
    this.registerHostApi()
    this.registerSslApi()
    this.registerMiscApi()
    this.registerPages()
  }

  // --- /api/proxy/hosts ---

  private registerHostApi(): void {
    this.api.get('/hosts', (c) => c.json({ ok: true, data: this.hosts.allSorted() }))

    this.api.get('/hosts/:id', (c) => {
      const host = this.hosts.find(c.req.param('id'))
      if (!host) return this.notFound(c, 'Host not found')
      return c.json({ ok: true, data: host })
    })

    this.api.post('/hosts', async (c) => {
      const body = await c.req.json()
      if (!body.domain || !body.target_host || !body.target_port) {
        return this.badRequest(c, 'domain, target_host, and target_port are required')
      }
      if (this.hosts.findByDomain(body.domain)) {
        return c.json({ ok: false, error: { code: 'CONFLICT', message: `Domain "${body.domain}" already exists` } }, 409)
      }
      const host = this.hosts.create({
        domain: body.domain,
        target_scheme: body.target_scheme || 'http',
        target_host: body.target_host,
        target_port: body.target_port,
        ssl_enabled: body.ssl_enabled ? 1 : 0,
        force_ssl: body.force_ssl ? 1 : 0,
        description: body.description || '',
      })
      this.server.reloadHosts()
      this.events.emit('proxy:host-created', { id: host.id, domain: host.domain })
      return c.json({ ok: true, data: host }, 201)
    })

    this.api.patch('/hosts/:id', async (c) => {
      const body = await c.req.json()
      const patch: Partial<ProxyHost> = {}
      for (const field of HOST_FIELDS) {
        if (body[field] !== undefined) {
          patch[field] = typeof body[field] === 'boolean' ? (body[field] ? 1 : 0) : body[field]
        }
      }
      if (!Object.keys(patch).length) return this.badRequest(c, 'No fields to update')
      const host = this.hosts.update(c.req.param('id'), patch)
      if (!host) return this.notFound(c, 'Host not found')
      this.server.reloadHosts()
      return c.json({ ok: true, data: host })
    })

    this.api.delete('/hosts/:id', (c) => {
      this.hosts.delete(c.req.param('id'))
      this.server.reloadHosts()
      return c.json({ ok: true, data: null })
    })

    this.api.post('/hosts/:id/toggle', (c) => {
      const host = this.hosts.toggle(Number(c.req.param('id')))
      if (!host) return this.notFound(c, 'Host not found')
      this.server.reloadHosts()
      return c.json({ ok: true, data: host })
    })
  }

  // --- /api/proxy/certs ---

  private registerSslApi(): void {
    this.api.get('/certs', (c) => c.json({ ok: true, data: this.certs.allSorted() }))

    this.api.post('/certs/issue', async (c) => {
      const { domain, email } = await c.req.json()
      if (!domain) return this.badRequest(c, 'domain is required')
      try {
        const cert = await this.ssl.issue(domain, email)
        return c.json({ ok: true, data: { domain, expiresAt: cert.expires_at, provider: cert.provider } })
      } catch (err: any) {
        return this.sslFailure(c, err, `certificate issuance failed for ${domain}`)
      }
    })

    this.api.post('/certs/upload', async (c) => {
      const { domain, cert, key } = await c.req.json()
      if (!domain || !cert || !key) return this.badRequest(c, 'domain, cert, and key are required')
      try {
        const saved = this.ssl.upload(domain, cert, key)
        return c.json({ ok: true, data: { domain, provider: saved.provider, expiresAt: saved.expires_at } })
      } catch (err: any) {
        return c.json({ ok: false, error: { code: 'UPLOAD_ERROR', message: err.message } }, 500)
      }
    })

    this.api.post('/certs/:id/renew', async (c) => {
      try {
        const cert = await this.ssl.renew(Number(c.req.param('id')))
        return c.json({ ok: true, data: { domain: cert.domain, expiresAt: cert.expires_at } })
      } catch (err: any) {
        return this.sslFailure(c, err, 'certificate renewal failed')
      }
    })

    this.api.delete('/certs/:id', (c) => {
      this.ssl.remove(Number(c.req.param('id')))
      return c.json({ ok: true, data: null })
    })

    this.api.post('/certs/check-renewals', async (c) => {
      await this.ssl.renewDueCertificates()
      return c.json({ ok: true, data: { message: 'Renewal check complete' } })
    })
  }

  private registerMiscApi(): void {
    this.api.get('/logs', (c) => {
      const limit = Number(c.req.query('limit') || 50)
      return c.json({ ok: true, data: this.logs.recent(limit, c.req.query('domain') || undefined) })
    })

    this.api.post('/reload', (c) => {
      this.server.reloadHosts()
      this.server.reloadCertificates()
      return c.json({ ok: true, data: { hosts: this.hosts.enabled().length } })
    })
  }

  // --- /admin/proxy pages ---

  private registerPages(): void {
    this.pages.get('/', (c) => {
      const hosts = this.hosts.allSorted()
      const certDomains = new Set(this.certs.query().pluck<string>('domain'))
      return c.html(hostsPage(hosts, certDomains, {
        httpPort: this.server.httpPort,
        httpsPort: this.server.httpsPort,
        httpsActive: this.server.httpsActive,
        certificateCount: this.server.certificateCount,
      }))
    })

    this.pages.get('/ssl', (c) => {
      const certs = this.certs.allSorted()
      const certDomains = new Set(certs.map((cert) => cert.domain))
      const domainsWithoutCert = this.hosts.query().pluck<string>('domain').filter((d) => !certDomains.has(d))
      return c.html(sslPage(certs, domainsWithoutCert, this.ssl.defaultEmail))
    })

    this.pages.get('/logs', (c) => {
      const domain = c.req.query('domain') || ''
      return c.html(logsPage(this.logs.recent(100, domain || undefined), domain))
    })
  }

  // --- 응답 헬퍼 ---

  private notFound(c: Context, message: string) {
    return c.json({ ok: false, error: { code: 'NOT_FOUND', message } }, 404)
  }

  private badRequest(c: Context, message: string) {
    return c.json({ ok: false, error: { code: 'VALIDATION', message } }, 400)
  }

  private sslFailure(c: Context, err: unknown, logMessage: string) {
    if (err instanceof SslError) {
      const status = err.code === 'NOT_FOUND' ? 404 : err.code === 'VALIDATION' || err.code === 'NOT_SUPPORTED' ? 400 : 500
      return c.json({ ok: false, error: { code: err.code, message: err.message } }, status as any)
    }
    const message = err instanceof Error ? err.message : String(err)
    this.log.error(`${logMessage}: ${message}`)
    return c.json({ ok: false, error: { code: 'ACME_ERROR', message } }, 500)
  }
}
