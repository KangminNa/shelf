import { join } from 'node:path'
import type { Logger } from '../../services/log.js'
import type { EventBus } from '../../services/events.js'
import type { ProxyServer } from './proxy-server.js'
import { certDomains, type ProxyHostRepository, type SslCertRepository, type SslCert } from './repositories.js'
import { CertificateStore } from './issuers/certificate-store.js'
import { LetsEncryptIssuer } from './issuers/lets-encrypt.js'
import { SelfSignedIssuer } from './issuers/self-signed.js'
import { ManualUploadIssuer } from './issuers/manual-upload.js'
import { SslError, type IssueRequest } from './issuers/types.js'

export { SslError } from './issuers/types.js'
export type { IssueRequest as IssueOptions } from './issuers/types.js'

export class SslManager {
  static readonly RENEWAL_WINDOW_DAYS = 30

  readonly defaultEmail = process.env.ACME_EMAIL || ''

  covers(domain: string): boolean {
    const name = domain.toLowerCase()
    const dot = name.indexOf('.')
    const wildcard = dot === -1 ? null : `*${name.slice(dot)}`
    for (const cert of this.certRepo.all()) {
      for (const covered of certDomains(cert)) {
        const known = covered.toLowerCase()
        if (known === name || (wildcard && known === wildcard)) return true
      }
    }
    return false
  }

  private readonly store = new CertificateStore(join(process.cwd(), 'data', 'ssl'))
  private readonly letsEncrypt: LetsEncryptIssuer
  private readonly selfSigned: SelfSignedIssuer
  private readonly manual: ManualUploadIssuer

  constructor(
    private readonly certRepo: SslCertRepository,
    private readonly hostRepo: ProxyHostRepository,
    private readonly server: ProxyServer,
    private readonly events: EventBus,
    private readonly log: Logger
  ) {
    this.letsEncrypt = new LetsEncryptIssuer(this.store, server, log)
    this.selfSigned = new SelfSignedIssuer(this.store, log)
    this.manual = new ManualUploadIssuer(this.store)
  }

  async issue(request: IssueRequest): Promise<SslCert> {
    const domains = SslManager.normalizeDomains(request.domains)
    const email = request.email || this.defaultEmail
    const challenge = LetsEncryptIssuer.resolveChallenge({ ...request, domains })

    const files = await this.letsEncrypt.issue({ ...request, domains, email })
    const cert = this.certRepo.upsert(domains[0], {
      domains: domains.join('\n'),
      cert_path: files.certPath,
      key_path: files.keyPath,
      provider: this.letsEncrypt.provider,
      dns_provider: challenge === 'dns' ? 'cloudflare' : '',
      dns_token: challenge === 'dns' ? request.dnsToken! : '',
      expires_at: files.expiresAt,
      auto_renew: 1,
    })

    this.activate(cert)
    this.events.emit('proxy:cert-issued', { domain: domains[0], domains })
    return cert
  }

  async selfSign(domain: string): Promise<SslCert> {
    const [name] = SslManager.normalizeDomains([domain])
    const files = await this.selfSigned.issue({ domains: [name] })
    const cert = this.certRepo.upsert(name, {
      domains: SelfSignedIssuer.coveredDomains(name).join('\n'),
      cert_path: files.certPath,
      key_path: files.keyPath,
      provider: this.selfSigned.provider,
      dns_provider: '',
      dns_token: '',
      expires_at: files.expiresAt,
      auto_renew: 0,
    })
    this.activate(cert)
    return cert
  }

  upload(domain: string, certPem: string, keyPem: string): SslCert {
    const [name] = SslManager.normalizeDomains([domain])
    const files = this.manual.accept(name, certPem, keyPem)
    const cert = this.certRepo.upsert(name, {
      domains: name,
      cert_path: files.certPath,
      key_path: files.keyPath,
      provider: this.manual.provider,
      dns_provider: '',
      dns_token: '',
      expires_at: files.expiresAt,
      auto_renew: 0,
    })
    this.activate(cert)
    return cert
  }

  async renew(certId: number): Promise<SslCert> {
    const cert = this.certRepo.find(certId)
    if (!cert) throw new SslError('NOT_FOUND', 'Certificate not found')
    if (cert.provider !== this.letsEncrypt.provider) {
      throw new SslError('NOT_SUPPORTED', "Only Let's Encrypt certificates can be renewed automatically")
    }
    return this.issue({
      domains: certDomains(cert),
      challenge: cert.dns_provider ? 'dns' : 'http',
      dnsToken: cert.dns_token || undefined,
    })
  }

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

  remove(certId: number): void {
    const cert = this.certRepo.find(certId)
    if (!cert) return
    for (const domain of certDomains(cert)) this.hostRepo.setSslEnabled(domain, false)
    this.certRepo.delete(certId)
    this.reloadServer()
  }

  private activate(cert: SslCert): void {
    for (const domain of certDomains(cert)) {
      if (!domain.startsWith('*.')) this.hostRepo.setSslEnabled(domain, true)
    }
    this.reloadServer()
  }

  private reloadServer(): void {
    this.server.reloadHosts()
    this.server.reloadCertificates()
  }

  static normalizeDomains(domains: string[]): string[] {
    const normalized = domains.map((domain) => domain.trim().toLowerCase()).filter(Boolean)
    if (!normalized.length) throw new SslError('VALIDATION', 'At least one domain is required')
    return normalized
  }
}
