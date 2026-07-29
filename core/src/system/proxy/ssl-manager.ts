import * as crypto from 'node:crypto'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { writeFileSync, mkdirSync } from 'node:fs'
import { join } from 'node:path'
import type { Logger } from '../../services/log.js'
import type { EventBus } from '../../services/events.js'
import type { ProxyServer } from './proxy-server.js'
import { certDomains, type ProxyHostRepository, type SslCertRepository, type SslCert } from './repositories.js'

const exec = promisify(execFile)

export class SslError extends Error {
  constructor(public code: string, message: string) {
    super(message)
  }
}

export interface IssueOptions {
  domains: string[] // 첫 항목이 대표 도메인. *.example.com 포함 가능(DNS-01 필수)
  email?: string
  challenge?: 'http' | 'dns'
  dnsProvider?: 'cloudflare'
  dnsToken?: string
}

/**
 * SSL 인증서 관리자 (Nginx Proxy Manager 수준).
 * - Let's Encrypt HTTP-01 (기본) / DNS-01 Cloudflare (와일드카드 지원)
 * - SAN(여러 도메인) 인증서
 * - 수동 PEM 업로드 / 자체서명(로컬·LAN용)
 * - 만료 30일 전 자동 갱신 (DNS-01 자격증명은 저장해 재사용)
 * 인증서 파일은 data/ssl/{대표도메인}/에 저장된다.
 */
export class SslManager {
  static readonly RENEWAL_WINDOW_DAYS = 30

  private readonly sslDir = join(process.cwd(), 'data', 'ssl')
  private readonly directoryUrl = process.env.ACME_DIRECTORY_URL || 'https://acme-v02.api.letsencrypt.org/directory'
  readonly defaultEmail = process.env.ACME_EMAIL || ''

  constructor(
    private readonly certRepo: SslCertRepository,
    private readonly hostRepo: ProxyHostRepository,
    private readonly server: ProxyServer,
    private readonly events: EventBus,
    private readonly log: Logger
  ) {
    mkdirSync(this.sslDir, { recursive: true })
  }

  /** Let's Encrypt 발급 (HTTP-01 또는 DNS-01/와일드카드) */
  async issue(opts: IssueOptions): Promise<SslCert> {
    const domains = opts.domains.map((d) => d.trim().toLowerCase()).filter(Boolean)
    if (!domains.length) throw new SslError('VALIDATION', 'At least one domain is required')

    const contact = opts.email || this.defaultEmail
    if (!contact) {
      throw new SslError('VALIDATION', "email is required for Let's Encrypt. Set ACME_EMAIL env var or pass email in request.")
    }

    const hasWildcard = domains.some((d) => d.startsWith('*.'))
    const challenge = opts.challenge || (hasWildcard ? 'dns' : 'http')
    if (hasWildcard && challenge !== 'dns') {
      throw new SslError('VALIDATION', 'Wildcard certificates require the DNS-01 challenge (Cloudflare)')
    }
    if (challenge === 'dns' && !opts.dnsToken) {
      throw new SslError('VALIDATION', 'DNS-01 requires a Cloudflare API token (Zone.DNS edit permission)')
    }

    const primary = domains[0].replace(/^\*\./, 'wildcard.')
    this.log.info(`issuing certificate for [${domains.join(', ')}] via ${challenge}-01...`)
    const { certPath, keyPath, expiresAt } = await this.acmeIssue(domains, contact, challenge, opts.dnsToken)

    const cert = this.certRepo.upsert(domains[0], {
      domains: domains.join('\n'),
      cert_path: certPath,
      key_path: keyPath,
      provider: 'letsencrypt',
      dns_provider: challenge === 'dns' ? 'cloudflare' : '',
      dns_token: challenge === 'dns' ? opts.dnsToken! : '',
      expires_at: expiresAt,
      auto_renew: 1,
    })

    this.applyCertificate(cert)
    this.events.emit('proxy:cert-issued', { domain: domains[0], domains })
    return cert
  }

  /** 수동 PEM 업로드 */
  upload(domain: string, certPem: string, keyPem: string): SslCert {
    const { certPath, keyPath } = this.writePair(domain, certPem, keyPem)
    const cert = this.certRepo.upsert(domain, {
      domains: domain,
      cert_path: certPath,
      key_path: keyPath,
      provider: 'manual',
      dns_provider: '',
      dns_token: '',
      expires_at: SslManager.parseExpiry(certPem),
      auto_renew: 0,
    })
    this.applyCertificate(cert)
    return cert
  }

  /** 자체서명 인증서 (도메인 없는 로컬/LAN 환경용, 10년) */
  async selfSigned(domain: string): Promise<SslCert> {
    const name = domain.trim().toLowerCase()
    if (!name) throw new SslError('VALIDATION', 'domain is required')

    const dir = join(this.sslDir, name)
    mkdirSync(dir, { recursive: true })
    const certPath = join(dir, 'cert.pem')
    const keyPath = join(dir, 'key.pem')

    try {
      await exec('openssl', [
        'req', '-x509', '-newkey', 'rsa:2048', '-sha256', '-nodes', '-days', '3650',
        '-keyout', keyPath, '-out', certPath,
        '-subj', `/CN=${name}`,
        '-addext', `subjectAltName=DNS:${name},DNS:*.${name}`,
      ], { timeout: 30_000 })
    } catch (err: any) {
      throw new SslError('OPENSSL_FAILED', `openssl failed: ${(err.stderr || err.message || '').toString().trim().split('\n').pop()}`)
    }

    const cert = this.certRepo.upsert(name, {
      domains: `${name}\n*.${name}`,
      cert_path: certPath,
      key_path: keyPath,
      provider: 'selfsigned',
      dns_provider: '',
      dns_token: '',
      expires_at: Math.floor(Date.now() / 1000) + 3650 * 86400,
      auto_renew: 0,
    })
    this.applyCertificate(cert)
    this.log.info(`self-signed certificate created for ${name} (+wildcard)`)
    return cert
  }

  /** 재발급 (Let's Encrypt 전용 — 저장된 도메인/DNS 자격증명 재사용) */
  async renew(certId: number): Promise<SslCert> {
    const cert = this.certRepo.find(certId)
    if (!cert) throw new SslError('NOT_FOUND', 'Certificate not found')
    if (cert.provider !== 'letsencrypt') {
      throw new SslError('NOT_SUPPORTED', "Only Let's Encrypt certificates can be renewed automatically")
    }
    return this.issue({
      domains: certDomains(cert),
      challenge: cert.dns_provider ? 'dns' : 'http',
      dnsToken: cert.dns_token || undefined,
    })
  }

  remove(certId: number): void {
    const cert = this.certRepo.find(certId)
    if (cert) {
      for (const domain of certDomains(cert)) this.hostRepo.setSslEnabled(domain, false)
      this.certRepo.delete(certId)
      this.server.reloadHosts()
      this.server.reloadCertificates()
    }
  }

  /** 스케줄러가 매일 호출 — 만료 임박 인증서 자동 갱신 */
  async renewDueCertificates(): Promise<void> {
    for (const cert of this.certRepo.dueForRenewal(SslManager.RENEWAL_WINDOW_DAYS)) {
      this.log.info(`auto-renewing certificate for ${cert.domain}`)
      try {
        await this.renew(cert.id)
        this.events.emit('proxy:cert-renewed', { domain: cert.domain })
      } catch (err: any) {
        this.log.error(`auto-renewal failed for ${cert.domain}: ${err.message}`)
        this.events.emit('proxy:cert-renewal-failed', { domain: cert.domain, error: err.message })
      }
    }
  }

  // --- 내부 ---

  /** 발급/업로드 후 반영: 커버 도메인의 호스트 SSL 활성화 + 인증서 리로드 */
  private applyCertificate(cert: SslCert): void {
    for (const domain of certDomains(cert)) {
      if (!domain.startsWith('*.')) this.hostRepo.setSslEnabled(domain, true)
    }
    this.server.reloadHosts()
    this.server.reloadCertificates()
  }

  private writePair(domain: string, certPem: string, keyPem: string): { certPath: string; keyPath: string } {
    const dir = join(this.sslDir, domain)
    mkdirSync(dir, { recursive: true })
    const certPath = join(dir, 'cert.pem')
    const keyPath = join(dir, 'key.pem')
    writeFileSync(certPath, certPem)
    writeFileSync(keyPath, keyPem)
    return { certPath, keyPath }
  }

  private async acmeIssue(
    domains: string[],
    email: string,
    challenge: 'http' | 'dns',
    dnsToken?: string
  ): Promise<{ certPath: string; keyPath: string; expiresAt: number }> {
    const acme = await import('acme-client')
    const cloudflare = dnsToken ? new CloudflareDns(dnsToken, this.log) : null

    const client = new acme.Client({
      directoryUrl: this.directoryUrl,
      accountKey: await acme.crypto.createPrivateKey(),
    })
    await client.createAccount({ termsOfServiceAgreed: true, contact: [`mailto:${email}`] })

    const [key, csr] = await acme.crypto.createCsr({
      commonName: domains[0],
      altNames: domains.slice(1),
    })

    const cert = await client.auto({
      csr,
      challengePriority: challenge === 'dns' ? ['dns-01'] : ['http-01'],
      challengeCreateFn: async (authz: any, ch: any, keyAuthorization: string) => {
        if (ch.type === 'dns-01') {
          await cloudflare!.createTxt(`_acme-challenge.${authz.identifier.value}`, keyAuthorization)
        } else {
          this.server.setAcmeChallenge(ch.token, keyAuthorization)
        }
      },
      challengeRemoveFn: async (authz: any, ch: any) => {
        if (ch.type === 'dns-01') {
          await cloudflare!.removeTxt(`_acme-challenge.${authz.identifier.value}`).catch(() => {})
        } else {
          this.server.removeAcmeChallenge(ch.token)
        }
      },
    })

    const storeName = domains[0].replace(/^\*\./, 'wildcard.')
    const { certPath, keyPath } = this.writePair(storeName, cert.toString(), key.toString())
    this.log.info(`certificate issued for [${domains.join(', ')}]`)
    return { certPath, keyPath, expiresAt: SslManager.parseExpiry(cert.toString()) }
  }

  private static parseExpiry(certPem: string): number {
    try {
      const info = new crypto.X509Certificate(certPem)
      return Math.floor(new Date(info.validTo).getTime() / 1000)
    } catch {
      return 0
    }
  }
}

/** Cloudflare DNS API — DNS-01 챌린지용 TXT 레코드 관리 */
class CloudflareDns {
  private static readonly API = 'https://api.cloudflare.com/client/v4'
  private readonly createdIds: Array<{ zone: string; id: string }> = []

  constructor(private readonly token: string, private readonly log: Logger) {}

  async createTxt(name: string, content: string): Promise<void> {
    const zone = await this.zoneIdFor(name)
    const res = await this.api(`/zones/${zone}/dns_records`, {
      method: 'POST',
      body: JSON.stringify({ type: 'TXT', name, content, ttl: 60 }),
    })
    this.createdIds.push({ zone, id: res.result.id })
    this.log.info(`cloudflare TXT created: ${name}`)
    // DNS 전파 대기
    await new Promise((r) => setTimeout(r, 10_000))
  }

  async removeTxt(_name: string): Promise<void> {
    for (const { zone, id } of this.createdIds.splice(0)) {
      await this.api(`/zones/${zone}/dns_records/${id}`, { method: 'DELETE' }).catch(() => {})
    }
  }

  private async zoneIdFor(recordName: string): Promise<string> {
    // _acme-challenge.a.b.example.com → example.com 순으로 zone 탐색
    const parts = recordName.split('.')
    for (let i = parts.length - 2; i >= 0; i--) {
      const candidate = parts.slice(i).join('.')
      const res = await this.api(`/zones?name=${candidate}`, { method: 'GET' })
      if (res.result?.length) return res.result[0].id
    }
    throw new SslError('DNS_ZONE_NOT_FOUND', `No Cloudflare zone found for ${recordName}`)
  }

  private async api(path: string, init: RequestInit): Promise<any> {
    const res = await fetch(`${CloudflareDns.API}${path}`, {
      ...init,
      headers: { Authorization: `Bearer ${this.token}`, 'Content-Type': 'application/json' },
    })
    const json: any = await res.json()
    if (!json.success) {
      throw new SslError('CLOUDFLARE_ERROR', json.errors?.[0]?.message || `Cloudflare API failed (${res.status})`)
    }
    return json
  }
}
