import * as crypto from 'node:crypto'
import { writeFileSync, mkdirSync } from 'node:fs'
import { join } from 'node:path'
import type { Logger } from '../../services/log.js'
import type { EventBus } from '../../services/events.js'
import type { ProxyServer } from './proxy-server.js'
import type { ProxyHostRepository, SslCertRepository, SslCert } from './repositories.js'

export class SslError extends Error {
  constructor(public code: string, message: string) {
    super(message)
  }
}

/**
 * SSL 인증서 발급/갱신/업로드 관리자.
 * - Let's Encrypt (ACME HTTP-01, acme-client)
 * - 수동 PEM 업로드
 * - 만료 30일 전 자동 갱신
 * 인증서 파일은 data/ssl/{domain}/에 저장된다.
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

  /** Let's Encrypt로 발급하고 DB/서버에 반영 */
  async issue(domain: string, email?: string): Promise<SslCert> {
    const contact = email || this.defaultEmail
    if (!contact) {
      throw new SslError('VALIDATION', "email is required for Let's Encrypt. Set ACME_EMAIL env var or pass email in request.")
    }

    this.log.info(`issuing certificate for ${domain}...`)
    const { certPath, keyPath, expiresAt } = await this.acmeIssue(domain, contact)

    const cert = this.certRepo.upsert(domain, {
      cert_path: certPath,
      key_path: keyPath,
      provider: 'letsencrypt',
      expires_at: expiresAt,
      auto_renew: 1,
    })

    this.applyCertificate(domain)
    this.events.emit('proxy:cert-issued', { domain })
    return cert
  }

  /** 수동 PEM 업로드 */
  upload(domain: string, certPem: string, keyPem: string): SslCert {
    const dir = join(this.sslDir, domain)
    mkdirSync(dir, { recursive: true })
    const certPath = join(dir, 'cert.pem')
    const keyPath = join(dir, 'key.pem')
    writeFileSync(certPath, certPem)
    writeFileSync(keyPath, keyPem)

    const cert = this.certRepo.upsert(domain, {
      cert_path: certPath,
      key_path: keyPath,
      provider: 'manual',
      expires_at: SslManager.parseExpiry(certPem),
      auto_renew: 0,
    })

    this.applyCertificate(domain)
    return cert
  }

  /** 기존 인증서 재발급 (Let's Encrypt 전용) */
  async renew(certId: number): Promise<SslCert> {
    const cert = this.certRepo.find(certId)
    if (!cert) throw new SslError('NOT_FOUND', 'Certificate not found')
    if (cert.provider !== 'letsencrypt') {
      throw new SslError('NOT_SUPPORTED', "Only Let's Encrypt certificates can be renewed automatically")
    }
    return this.issue(cert.domain)
  }

  remove(certId: number): void {
    const cert = this.certRepo.find(certId)
    if (cert) {
      this.hostRepo.setSslEnabled(cert.domain, false)
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
        await this.issue(cert.domain)
        this.events.emit('proxy:cert-renewed', { domain: cert.domain })
      } catch (err: any) {
        this.log.error(`auto-renewal failed for ${cert.domain}: ${err.message}`)
        this.events.emit('proxy:cert-renewal-failed', { domain: cert.domain, error: err.message })
      }
    }
  }

  // --- 내부 ---

  /** 발급/업로드 후 프록시에 반영: 해당 도메인 SSL 활성화 + 인증서 리로드 */
  private applyCertificate(domain: string): void {
    this.hostRepo.setSslEnabled(domain, true)
    this.server.reloadHosts()
    this.server.reloadCertificates()
  }

  private async acmeIssue(domain: string, email: string): Promise<{ certPath: string; keyPath: string; expiresAt: number }> {
    const acme = await import('acme-client')

    const client = new acme.Client({
      directoryUrl: this.directoryUrl,
      accountKey: await acme.crypto.createPrivateKey(),
    })
    await client.createAccount({ termsOfServiceAgreed: true, contact: [`mailto:${email}`] })

    const [key, csr] = await acme.crypto.createCsr({ commonName: domain })

    const cert = await client.auto({
      csr,
      challengeCreateFn: async (_authz: unknown, challenge: any, keyAuthorization: string) => {
        this.server.setAcmeChallenge(challenge.token, keyAuthorization)
        this.log.info(`ACME challenge set for ${domain}`)
      },
      challengeRemoveFn: async (_authz: unknown, challenge: any) => {
        this.server.removeAcmeChallenge(challenge.token)
      },
    })

    const dir = join(this.sslDir, domain)
    mkdirSync(dir, { recursive: true })
    const certPath = join(dir, 'cert.pem')
    const keyPath = join(dir, 'key.pem')
    writeFileSync(certPath, cert)
    writeFileSync(keyPath, key)

    const expiresAt = SslManager.parseExpiry(cert.toString())
    this.log.info(`certificate issued for ${domain}`)
    return { certPath, keyPath, expiresAt }
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
