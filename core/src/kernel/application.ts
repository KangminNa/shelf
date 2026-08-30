import { Hono } from 'hono'
import { serve } from '@hono/node-server'
import { EventBus } from '../services/events.js'
import { Logger } from '../services/log.js'
import { ensureDataDir } from '../db/database.js'
import { errorBoundary } from '../middleware/error-boundary.js'
import { requestLogger } from '../middleware/request-logger.js'
import { createShellWrap } from '../middleware/shell-wrap.js'
import { createAdminRoutes } from '../admin/routes.js'
import { ProxySystem } from '../system/proxy/index.js'
import { DeploySystem } from '../system/deploy/index.js'
import { AuthSystem } from '../system/auth/index.js'
import { PublicAddress } from './public-address.js'

export class ShelfApplication {
  private static _instance?: ShelfApplication

  static get instance(): ShelfApplication {
    return (this._instance ??= new ShelfApplication())
  }

  readonly hono = new Hono()
  readonly events = new EventBus()
  readonly log = new Logger('shelf')

  auth!: AuthSystem
  proxy!: ProxySystem
  deploy!: DeploySystem

  private constructor() {
    ensureDataDir()
    this.hono.use('*', requestLogger)
    this.hono.onError(errorBoundary)
  }

  async start(port: number): Promise<void> {
    this.auth = new AuthSystem()
    this.proxy = new ProxySystem(this.events)
    this.deploy = new DeploySystem(
      this.events,
      new PublicAddress(process.env.ADMIN_DOMAIN || null, (domain) => this.proxy.ssl.covers(domain))
    )

    this.registerAdminDomain(port)
    this.registerRoutes()

    serve({ fetch: this.hono.fetch, port }, (info) => this.printBanner(info.port))

    process.on('SIGINT', () => this.shutdown())
    process.on('SIGTERM', () => this.shutdown())
  }

  private registerAdminDomain(port: number): void {
    const domain = process.env.ADMIN_DOMAIN
    if (!domain) return
    this.proxy.hosts.upsert({
      domain,
      target_host: '127.0.0.1',
      target_port: port,
      description: 'Shelf admin (auto-registered via ADMIN_DOMAIN)',
    })
    this.proxy.server.reloadHosts()
    this.log.info(`admin UI registered on proxy: ${domain} -> :${port}`)
  }

  private registerRoutes(): void {
    this.hono.get('/health', (c) => c.json({ ok: true, uptime: process.uptime() }))
    this.hono.get('/', (c) => c.redirect('/admin'))

    this.hono.route('/hooks', this.deploy.webhookRoutes)

    this.hono.route('/', this.auth.routes)
    this.hono.use('/admin/*', this.auth.requireAuth())
    this.hono.use('/admin', this.auth.requireAuth())
    this.hono.use('/api/proxy/*', this.auth.requireAuth())
    this.hono.use('/api/deploy/*', this.auth.requireAuth())

    this.hono.route('/api/proxy', this.proxy.api)
    this.hono.route('/api/deploy', this.deploy.api)

    const apps = () => this.deploy.appSummaries()
    this.hono.route('/admin/deploy', this.wrapInShell('Apps', this.deploy.pages))
    this.hono.route('/admin/proxy', this.wrapInShell('Proxy Manager', this.proxy.pages))
    this.hono.route(
      '/admin',
      createAdminRoutes({
        apps,
        proxyHostCount: () => this.proxy.hosts.count(),
        dockerAvailable: () => this.deploy.docker.available(),
      })
    )
  }

  private wrapInShell(title: string, pages: Hono): Hono {
    const wrapped = new Hono()
    wrapped.use('*', createShellWrap(title, () => this.deploy.appSummaries()))
    wrapped.route('/', pages)
    return wrapped
  }

  private printBanner(port: number): void {
    console.log('')
    console.log(`  ┌─────────────────────────────────────┐`)
    console.log(`  │                                     │`)
    console.log(`  │   Shelf v0.2.0 (docker)             │`)
    console.log(`  │   http://localhost:${port}            │`)
    console.log(`  │                                     │`)
    console.log(`  │   Apps: ${this.deploy.projects.count().toString().padEnd(28)}│`)
    console.log(`  │   Admin:  http://localhost:${port}/admin │`)
    console.log(`  │                                     │`)
    console.log(`  └─────────────────────────────────────┘`)
    console.log('')
  }

  async shutdown(): Promise<void> {
    console.log('\n[shelf] shutting down...')
    this.proxy?.shutdown()
    this.deploy?.shutdown()
    process.exit(0)
  }
}
