import { Controller, fields, notFound, invalid, conflict, type Request } from '../../kernel/controller.js'
import type { EventBus } from '../../services/events.js'
import { certDomains, type ProxyHost, type SslCert } from './repositories.js'
import type { ProxySystem } from './index.js'
import { HostsPage, SslPage, AccessLogsPage } from './views.js'

const HOST_FIELDS = ['domain', 'target_scheme', 'target_host', 'target_port', 'ssl_enabled', 'force_ssl', 'hsts_enabled', 'hsts_subdomains', 'enabled', 'description'] as const

function sanitizeCert(cert: SslCert): Omit<SslCert, 'dns_token'> & { has_dns_token: boolean } {
  const { dns_token, ...rest } = cert
  return { ...rest, has_dns_token: !!dns_token }
}

function domainList(body: Record<string, any>): string[] {
  const raw = Array.isArray(body.domains)
    ? body.domains
    : String(body.domains || body.domain || '').split(/[\n,]/)
  const domains = raw.map((d: string) => d.trim()).filter(Boolean)
  return domains.length ? domains : (invalid('domain(s) required') as never)
}

export class ProxyController extends Controller {
  constructor(
    private readonly proxy: ProxySystem,
    private readonly events: EventBus
  ) {
    super(proxy.logger)
    this.registerHostApi()
    this.registerSslApi()
    this.registerMiscApi()
    this.registerPages()
  }

  private host(id: string): ProxyHost {
    return this.proxy.hosts.find(id) ?? notFound('Host')
  }

  private applied<T>(result: T): T {
    this.proxy.server.reloadHosts()
    return result
  }

  private registerHostApi(): void {
    this.get('/hosts', () => this.proxy.hosts.allSorted())

    this.get('/hosts/:id', (req) => this.host(req.id))

    this.post('/hosts', (req) => {
      const { body } = req
      if (!body.domain || !body.target_host || !body.target_port) {
        invalid('domain, target_host, and target_port are required')
      }
      if (this.proxy.hosts.findByDomain(body.domain)) conflict(`Domain "${body.domain}" already exists`)

      const host = this.proxy.hosts.create({
        domain: body.domain,
        target_scheme: body.target_scheme || 'http',
        target_host: body.target_host,
        target_port: body.target_port,
        ssl_enabled: body.ssl_enabled ? 1 : 0,
        force_ssl: body.force_ssl ? 1 : 0,
        description: body.description || '',
      })
      this.events.emit('proxy:host-created', { id: host.id, domain: host.domain })
      return this.applied(host)
    }, 201)

    this.patch('/hosts/:id', (req) => {
      const patch = fields<ProxyHost>(req.body, HOST_FIELDS)
      if (!Object.keys(patch).length) invalid('No fields to update')
      return this.applied(this.proxy.hosts.update(req.id, patch) ?? notFound('Host'))
    })

    this.delete('/hosts/:id', (req) => {
      this.proxy.hosts.delete(req.id)
      return this.applied(null)
    })

    this.post('/hosts/:id/toggle', (req) => this.applied(this.proxy.hosts.toggle(Number(req.id)) ?? notFound('Host')))
  }

  private registerSslApi(): void {
    this.get('/certs', () => this.proxy.certs.allSorted().map(sanitizeCert))

    this.post('/certs/issue', async (req) => {
      const { body } = req
      const cert = await this.proxy.ssl.issue({
        domains: domainList(body),
        email: body.email,
        challenge: body.challenge === 'dns' ? 'dns' : body.challenge === 'http' ? 'http' : undefined,
        dnsToken: body.dns_token || undefined,
      })
      return { domains: certDomains(cert), expiresAt: cert.expires_at, provider: cert.provider }
    })

    this.post('/certs/selfsigned', async ({ body }) => {
      const cert = await this.proxy.ssl.selfSign(body.domain || invalid('domain is required'))
      return { domains: certDomains(cert), provider: cert.provider }
    })

    this.post('/certs/upload', ({ body }) => {
      if (!body.domain || !body.cert || !body.key) invalid('domain, cert, and key are required')
      const saved = this.proxy.ssl.upload(body.domain, body.cert, body.key)
      return { domain: body.domain, provider: saved.provider, expiresAt: saved.expires_at }
    })

    this.post('/certs/:id/renew', async (req) => {
      const cert = await this.proxy.ssl.renew(Number(req.id))
      return { domain: cert.domain, expiresAt: cert.expires_at }
    })

    this.delete('/certs/:id', (req) => {
      this.proxy.ssl.remove(Number(req.id))
      return null
    })

    this.post('/certs/check-renewals', async () => {
      await this.proxy.ssl.renewDueCertificates()
      return { message: 'Renewal check complete' }
    })
  }

  private registerMiscApi(): void {
    this.get('/logs', (req) => this.proxy.accessLogs.recent(req.number('limit', 50), req.query('domain') || undefined))

    this.post('/reload', () => {
      this.proxy.server.reloadCertificates()
      return this.applied({ hosts: this.proxy.hosts.enabled().length })
    })
  }

  private registerPages(): void {
    this.page('/', () => {
      const hosts = this.proxy.hosts.allSorted()
      return new HostsPage({
        hosts,
        certDomains: new Set(hosts.filter((h) => this.proxy.ssl.covers(h.domain)).map((h) => h.domain)),
        status: {
          httpPort: this.proxy.server.httpPort,
          httpsPort: this.proxy.server.httpsPort,
          httpsActive: this.proxy.server.httpsActive,
          certificateCount: this.proxy.server.certificateCount,
        },
      }).render()
    })

    this.page('/ssl', () => {
      return new SslPage({
        certs: this.proxy.certs.allSorted().map(sanitizeCert),
        domainsWithoutCert: this.proxy.hosts.query().pluck<string>('domain').filter((d) => !this.proxy.ssl.covers(d)),
        defaultEmail: this.proxy.ssl.defaultEmail,
      }).render()
    })

    this.page('/logs', (req: Request) => {
      const domain = req.query('domain')
      return new AccessLogsPage({
        logs: this.proxy.accessLogs.recent(100, domain || undefined),
        selectedDomain: domain,
      }).render()
    })
  }
}
