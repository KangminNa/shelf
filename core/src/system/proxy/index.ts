import { join } from 'node:path'
import type { Hono } from 'hono'
import { AppDatabase } from '../../db/database.js'
import { Logger } from '../../services/log.js'
import { Scheduler } from '../../services/scheduler.js'
import type { EventBus } from '../../services/events.js'
import { ProxyHostRepository, SslCertRepository, AccessLogRepository } from './repositories.js'
import { ProxyServer } from './proxy-server.js'
import { SslManager } from './ssl-manager.js'
import { ProxyController } from './controller.js'

export type { ProxyHost, SslCert, AccessLog } from './repositories.js'

const ACCESS_LOG_RETENTION_DAYS = Number(process.env.ACCESS_LOG_RETENTION_DAYS || 14)

export class ProxySystem {
  readonly hosts: ProxyHostRepository
  readonly certs: SslCertRepository
  readonly accessLogs: AccessLogRepository
  readonly server: ProxyServer
  readonly ssl: SslManager
  private readonly controller: ProxyController
  private readonly scheduler = new Scheduler('proxy')
  private readonly log = new Logger('proxy')

  constructor(events: EventBus) {
    const db = new AppDatabase('proxy', join(process.cwd(), 'core', 'migrations', 'proxy'))

    this.hosts = new ProxyHostRepository(db.raw)
    this.certs = new SslCertRepository(db.raw)
    this.accessLogs = new AccessLogRepository(db.raw)

    this.server = new ProxyServer(this.hosts, this.certs, this.accessLogs, this.log)
    this.ssl = new SslManager(this.certs, this.hosts, this.server, events, this.log.scope('ssl'))
    this.controller = new ProxyController(this, events)

    this.server.start()
    this.scheduler.register('0 3 * * *', 'ssl-renewal-check', () => this.ssl.renewDueCertificates())
    this.scheduler.register('0 4 * * *', 'access-log-rotation', async () => {
      const removed = this.accessLogs.forget(ACCESS_LOG_RETENTION_DAYS)
      if (removed) this.log.info(`pruned ${removed} access log rows older than ${ACCESS_LOG_RETENTION_DAYS} days`)
    })

    events.on('proxy:register-host', (payload: any) => {
      if (!payload?.domain || !payload?.target_port) return
      const host = this.hosts.upsert(payload)
      this.server.reloadHosts()
      this.log.info(`registered proxy host ${host.domain} -> ${host.target_host}:${host.target_port} (via event)`)
    })

    events.on('proxy:release-target', (payload: any) => {
      if (!payload?.target_host) return
      const removed = this.hosts.deleteByTarget(payload.target_host)
      if (removed.length) {
        this.server.reloadHosts()
        this.log.info(`removed ${removed.length} proxy host(s) for ${payload.target_host}: ${removed.join(', ')}`)
      }
    })

    this.log.info('proxy system ready')
  }

  get logger(): Logger {
    return this.log
  }

  get api(): Hono {
    return this.controller.api
  }

  get pages(): Hono {
    return this.controller.pages
  }

  shutdown(): void {
    this.scheduler.stopAll()
    this.server.stop()
  }
}
