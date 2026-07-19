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

/**
 * 코어 내장 리버스 프록시 시스템 (Nginx Proxy Manager 역할).
 *
 *   ProxySystem
 *   ├── ProxyHostRepository / SslCertRepository / AccessLogRepository  (데이터)
 *   ├── ProxyServer   — 80/443 리스너, SNI, WebSocket, ACME 챌린지
 *   ├── SslManager    — Let's Encrypt 발급/갱신, 수동 업로드
 *   └── ProxyController — /api/proxy + /admin/proxy 라우트
 *
 * 다른 시스템/모듈은 events.emit('proxy:register-host', {...})로 호스트를 등록한다.
 */
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
    this.controller = new ProxyController(this.hosts, this.certs, this.accessLogs, this.server, this.ssl, events, this.log)

    this.server.start()
    this.scheduler.register('0 3 * * *', 'ssl-renewal-check', () => this.ssl.renewDueCertificates())

    // 모듈/시스템 간 연동: 이벤트로 프록시 호스트 등록 (예: deploy가 도메인 연결)
    events.on('proxy:register-host', (payload: any) => {
      if (!payload?.domain || !payload?.target_port) return
      const host = this.hosts.upsert(payload)
      this.server.reloadHosts()
      this.log.info(`registered proxy host ${host.domain} -> ${host.target_host}:${host.target_port} (via event)`)
    })

    this.log.info('proxy system ready')
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
