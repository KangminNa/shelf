import { Hono } from 'hono'
import { serve } from '@hono/node-server'
import { cors } from 'hono/cors'
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

/**
 * Shelf 애플리케이션 — 전역 인스턴스 (ShelfApplication.instance).
 *
 * 앱 = Docker 컨테이너. 코어는 오케스트레이션만 담당한다:
 *  - DeploySystem: git → docker build → run, webhook CI/CD
 *  - ProxySystem: 80/443 리버스 프록시, 도메인 라우팅, SSL
 *  - Admin UI: 대시보드 / 앱·프록시 관리 / 가이드
 *
 * URL 구조:
 *  /admin/*        관리 화면 (deploy, proxy, system, settings, guide)
 *  /api/deploy/*   앱 관리 API
 *  /api/proxy/*    프록시 관리 API
 */
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
    this.hono.use('*', cors())
    this.hono.use('*', requestLogger)
    this.hono.onError(errorBoundary)
  }

  async start(port: number): Promise<void> {
    this.auth = new AuthSystem()
    this.proxy = new ProxySystem(this.events)
    this.deploy = new DeploySystem(this.events)

    this.registerRoutes()

    serve({ fetch: this.hono.fetch, port }, (info) => this.printBanner(info.port))

    process.on('SIGINT', () => this.shutdown())
    process.on('SIGTERM', () => this.shutdown())
  }

  private registerRoutes(): void {
    this.hono.get('/health', (c) => c.json({ ok: true, uptime: process.uptime() }))
    this.hono.get('/', (c) => c.redirect('/admin'))

    // 인증: 공개 라우트(/login, /setup, /api/auth) + 나머지 전부 보호
    this.hono.route('/', this.auth.routes)
    this.hono.use('/admin/*', this.auth.requireAuth())
    this.hono.use('/admin', this.auth.requireAuth())
    this.hono.use('/api/proxy/*', this.auth.requireAuth())
    this.hono.use('/api/deploy/*', this.auth.requireAuth())

    // API
    this.hono.route('/api/proxy', this.proxy.api)
    this.hono.route('/api/deploy', this.deploy.api)

    // 관리 화면 (셸 자동 래핑)
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
