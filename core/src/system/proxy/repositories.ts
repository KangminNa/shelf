import type Database from 'better-sqlite3'
import { Repository } from '../../db/repository.js'

export interface ProxyHost {
  id: number
  domain: string
  target_scheme: string
  target_host: string
  target_port: number
  ssl_enabled: number
  force_ssl: number
  enabled: number
  description: string
  created_at: number
  updated_at: number
}

export interface SslCert {
  id: number
  domain: string
  cert_path: string
  key_path: string
  provider: string
  expires_at: number
  auto_renew: number
  created_at: number
}

export interface AccessLog {
  id: number
  domain: string
  method: string
  path: string
  status: number
  duration_ms: number
  ip: string
  user_agent: string
  created_at: number
}

export class ProxyHostRepository extends Repository<ProxyHost> {
  constructor(db: Database.Database) {
    super(db, 'proxy_hosts')
  }

  allSorted(): ProxyHost[] {
    return this.query().orderBy('domain').all()
  }

  enabled(): ProxyHost[] {
    return this.query().where('enabled', 1).all()
  }

  findByDomain(domain: string): ProxyHost | undefined {
    return this.findBy({ domain } as Partial<ProxyHost>)
  }

  toggle(id: number): ProxyHost | undefined {
    const host = this.find(id)
    if (!host) return undefined
    return this.update(id, { enabled: host.enabled ? 0 : 1 })
  }

  setSslEnabled(domain: string, enabled: boolean): void {
    const host = this.findByDomain(domain)
    if (host) this.update(host.id, { ssl_enabled: enabled ? 1 : 0 })
  }

  /** 있으면 타깃 갱신, 없으면 생성 (deploy 연동용) */
  upsert(data: { domain: string; target_scheme?: string; target_host?: string; target_port: number; description?: string }): ProxyHost {
    const existing = this.findByDomain(data.domain)
    if (existing) {
      return this.update(existing.id, {
        target_host: data.target_host || '127.0.0.1',
        target_port: data.target_port,
        enabled: 1,
      }) as ProxyHost
    }
    return this.create({
      domain: data.domain,
      target_scheme: data.target_scheme || 'http',
      target_host: data.target_host || '127.0.0.1',
      target_port: data.target_port,
      description: data.description || '',
    })
  }
}

export class SslCertRepository extends Repository<SslCert> {
  constructor(db: Database.Database) {
    super(db, 'ssl_certs')
  }

  allSorted(): SslCert[] {
    return this.query().orderBy('domain').all()
  }

  findByDomain(domain: string): SslCert | undefined {
    return this.findBy({ domain } as Partial<SslCert>)
  }

  /** 자동 갱신 대상 중 만료 임박(days 이내) 인증서 */
  dueForRenewal(days: number): SslCert[] {
    const deadline = Math.floor(Date.now() / 1000) + days * 86400
    return this.query()
      .where('auto_renew', 1)
      .where('expires_at', '>', 0)
      .where('expires_at', '<', deadline)
      .all()
  }

  upsert(domain: string, data: Partial<SslCert>): SslCert {
    const existing = this.findByDomain(domain)
    if (existing) return this.update(existing.id, data) as SslCert
    return this.create({ domain, ...data })
  }
}

export class AccessLogRepository extends Repository<AccessLog> {
  constructor(db: Database.Database) {
    super(db, 'access_logs')
  }

  record(entry: Omit<AccessLog, 'id' | 'created_at'>): void {
    try {
      this.query().insert(entry as Partial<AccessLog>)
    } catch {
      // 로그 실패는 요청 처리에 영향 주지 않음
    }
  }

  recent(limit = 100, domain?: string): AccessLog[] {
    const qb = this.query().orderBy('created_at', 'desc').limit(limit)
    if (domain) qb.where('domain', domain)
    return qb.all()
  }
}
