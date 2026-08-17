import { Hono } from 'hono'
import type { Context } from 'hono'
import type { EventBus } from '../../services/events.js'
import { SslError } from './ssl-manager.js'
import { certDomains, type ProxyHost, type SslCert } from './repositories.js'
import type { ProxySystem } from './index.js'
import { HostsPage, SslPage, AccessLogsPage } from './views.js'

const HOST_FIELDS = ['domain', 'target_scheme', 'target_host', 'target_port', 'ssl_enabled', 'force_ssl', 'hsts_enabled', 'hsts_subdomains', 'enabled', 'description'] as const

function sanitizeCert(cert: SslCert): Omit<SslCert, 'dns_token'> & { has_dns_token: boolean } {
  const { dns_token, ...rest } = cert
  return { ...rest, has_dns_token: !!dns_token }
}

export class ProxyController {
  readonly api = new Hono()
  readonly pages = new Hono()

  constructor(
    private readonly proxy: ProxySystem,
    private readonly events: EventBus
  ) {
    this.registerHostApi()
    this.registerSslApi()
    this.registerMiscApi()
    this.registerPages()
  }

  private registerHostApi(): void {
    this.api.get('/hosts', (c) => c.json({ ok: true, data: this.proxy.hosts.allSorted() }))

    this.api.get('/hosts/:id', (c) => {
      const host = this.proxy.hosts.find(c.req.param('id'))
      if (!host) return this.notFound(c, 'Host not found')
      return c.json({ ok: true, data: host })
    })

    this.api.post('/hosts', async (c) => {
      const body = await c.req.json()
      if (!body.domain || !body.target_host || !body.target_port) {
        return this.badRequest(c, 'domain, target_host, and target_port are required')
      }
      if (this.proxy.hosts.findByDomain(body.domain)) {
        return c.json({ ok: false, error: { code: 'CONFLICT', message: `Domain "${body.domain}" already exists` } }, 409)
      }
      const host = this.proxy.hosts.create({
        domain: body.domain,
        target_scheme: body.target_scheme || 'http',
        target_host: body.target_host,
        target_port: body.target_port,
        ssl_enabled: body.ssl_enabled ? 1 : 0,
        force_ssl: body.force_ssl ? 1 : 0,
        description: body.description || '',
      })
      this.proxy.server.reloadHosts()
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
      const host = this.proxy.hosts.update(c.req.param('id'), patch)
      if (!host) return this.notFound(c, 'Host not found')
      this.proxy.server.reloadHosts()
      return c.json({ ok: true, data: host })
    })

    this.api.delete('/hosts/:id', (c) => {
      this.proxy.hosts.delete(c.req.param('id'))
      this.proxy.server.reloadHosts()
      return c.json({ ok: true, data: null })
    })

    this.api.post('/hosts/:id/toggle', (c) => {
      const host = this.proxy.hosts.toggle(Number(c.req.param('id')))
      if (!host) return this.notFound(c, 'Host not found')
      this.proxy.server.reloadHosts()
      return c.json({ ok: true, data: host })
    })
  }

  private registerSslApi(): void {
    this.api.get('/certs', (c) => c.json({ ok: true, data: this.proxy.certs.allSorted().map(sanitizeCert) }))

    this.api.post('/certs/issue', async (c) => {
      const body = await c.req.json()
      const domains: string[] = Array.isArray(body.domains)
        ? body.domains
        : String(body.domains || body.domain || '').split(/[\n,]/).map((d: string) => d.trim()).filter(Boolean)
      if (!domains.length) return this.badRequest(c, 'domain(s) required')
      try {
        const cert = await this.proxy.ssl.issue({
          domains,
          email: body.email,
          challenge: body.challenge === 'dns' ? 'dns' : body.challenge === 'http' ? 'http' : undefined,
          dnsToken: body.dns_token || undefined,
        })
        return c.json({ ok: true, data: { domains: certDomains(cert), expiresAt: cert.expires_at, provider: cert.provider } })
      } catch (err: any) {
        return this.sslFailure(c, err, `certificate issuance failed for ${domains.join(',')}`)
      }
    })

    this.api.post('/certs/selfsigned', async (c) => {
      const { domain } = await c.req.json()
      if (!domain) return this.badRequest(c, 'domain is required')
      try {
        const cert = await this.proxy.ssl.selfSigned(domain)
        return c.json({ ok: true, data: { domains: certDomains(cert), provider: cert.provider } })
      } catch (err: any) {
        return this.sslFailure(c, err, `self-signed generation failed for ${domain}`)
      }
    })

    this.api.post('/certs/upload', async (c) => {
      const { domain, cert, key } = await c.req.json()
      if (!domain || !cert || !key) return this.badRequest(c, 'domain, cert, and key are required')
      try {
        const saved = this.proxy.ssl.upload(domain, cert, key)
        return c.json({ ok: true, data: { domain, provider: saved.provider, expiresAt: saved.expires_at } })
      } catch (err: any) {
        return c.json({ ok: false, error: { code: 'UPLOAD_ERROR', message: err.message } }, 500)
      }
    })

    this.api.post('/certs/:id/renew', async (c) => {
      try {
        const cert = await this.proxy.ssl.renew(Number(c.req.param('id')))
        return c.json({ ok: true, data: { domain: cert.domain, expiresAt: cert.expires_at } })
      } catch (err: any) {
        return this.sslFailure(c, err, 'certificate renewal failed')
      }
    })

    this.api.delete('/certs/:id', (c) => {
      this.proxy.ssl.remove(Number(c.req.param('id')))
      return c.json({ ok: true, data: null })
    })

    this.api.post('/certs/check-renewals', async (c) => {
      await this.proxy.ssl.renewDueCertificates()
      return c.json({ ok: true, data: { message: 'Renewal check complete' } })
    })
  }

  private registerMiscApi(): void {
    this.api.get('/logs', (c) => {
      const limit = Number(c.req.query('limit') || 50)
      return c.json({ ok: true, data: this.proxy.accessLogs.recent(limit, c.req.query('domain') || undefined) })
    })

    this.api.post('/reload', (c) => {
      this.proxy.server.reloadHosts()
      this.proxy.server.reloadCertificates()
      return c.json({ ok: true, data: { hosts: this.proxy.hosts.enabled().length } })
    })
  }

  private coveredDomains(): (domain: string) => boolean {
    const covered = new Set<string>()
    for (const cert of this.proxy.certs.all()) {
      for (const d of certDomains(cert)) covered.add(d.toLowerCase())
    }
    return (domain: string) => {
      const name = domain.toLowerCase()
      if (covered.has(name)) return true
      const dot = name.indexOf('.')
      return dot !== -1 && covered.has(`*${name.slice(dot)}`)
    }
  }

  private registerPages(): void {
    this.pages.get('/', (c) => {
      const hosts = this.proxy.hosts.allSorted()
      const isCovered = this.coveredDomains()
      const coveredSet = new Set(hosts.filter((h) => isCovered(h.domain)).map((h) => h.domain))
      return c.html(new HostsPage({
        hosts,
        certDomains: coveredSet,
        status: {
          httpPort: this.proxy.server.httpPort,
          httpsPort: this.proxy.server.httpsPort,
          httpsActive: this.proxy.server.httpsActive,
          certificateCount: this.proxy.server.certificateCount,
        },
      }).render())
    })

    this.pages.get('/ssl', (c) => {
      const certs = this.proxy.certs.allSorted()
      const isCovered = this.coveredDomains()
      const domainsWithoutCert = this.proxy.hosts.query().pluck<string>('domain').filter((d) => !isCovered(d))
      return c.html(new SslPage({ certs: certs.map(sanitizeCert), domainsWithoutCert, defaultEmail: this.proxy.ssl.defaultEmail }).render())
    })

    this.pages.get('/logs', (c) => {
      const domain = c.req.query('domain') || ''
      return c.html(new AccessLogsPage({ logs: this.proxy.accessLogs.recent(100, domain || undefined), selectedDomain: domain }).render())
    })
  }

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
    this.proxy.logger.error(`${logMessage}: ${message}`)
    return c.json({ ok: false, error: { code: 'ACME_ERROR', message } }, 500)
  }
}
