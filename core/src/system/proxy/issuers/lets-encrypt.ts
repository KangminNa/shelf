import type { Logger } from '../../../services/log.js'
import { CloudflareDns, type DnsProvider } from '../dns/cloudflare.js'
import { CertificateStore } from './certificate-store.js'
import { SslError, type CertificateFiles, type CertificateIssuer, type IssueRequest } from './types.js'

export interface AcmeChallengeSink {
  setAcmeChallenge(token: string, keyAuthorization: string): void
  removeAcmeChallenge(token: string): void
}

export class LetsEncryptIssuer implements CertificateIssuer {
  readonly provider = 'letsencrypt'

  private readonly directoryUrl = process.env.ACME_DIRECTORY_URL || 'https://acme-v02.api.letsencrypt.org/directory'

  constructor(
    private readonly store: CertificateStore,
    private readonly challenges: AcmeChallengeSink,
    private readonly log: Logger
  ) {}

  async issue(request: IssueRequest): Promise<CertificateFiles> {
    const { domains, email } = request
    const challenge = LetsEncryptIssuer.resolveChallenge(request)
    if (!email) {
      throw new SslError('VALIDATION', "email is required for Let's Encrypt. Set ACME_EMAIL env var or pass email in request.")
    }

    this.log.info(`issuing certificate for [${domains.join(', ')}] via ${challenge}-01...`)
    const dns = challenge === 'dns' ? new CloudflareDns(request.dnsToken!, this.log) : null
    const acme = await import('acme-client')

    const client = new acme.Client({
      directoryUrl: this.directoryUrl,
      accountKey: await acme.crypto.createPrivateKey(),
    })
    await client.createAccount({ termsOfServiceAgreed: true, contact: [`mailto:${email}`] })

    const [key, csr] = await acme.crypto.createCsr({
      commonName: domains[0],
      altNames: domains.slice(1),
    })

    const certificate = await client.auto({
      csr,
      challengePriority: challenge === 'dns' ? ['dns-01'] : ['http-01'],
      challengeCreateFn: (authz: any, ch: any, keyAuthorization: string) =>
        this.publishChallenge(dns, authz, ch, keyAuthorization),
      challengeRemoveFn: (_authz: any, ch: any) => this.retractChallenge(dns, ch),
    })

    const pem = certificate.toString()
    const { certPath, keyPath } = this.store.save(domains[0], pem, key.toString())
    this.log.info(`certificate issued for [${domains.join(', ')}]`)
    return { certPath, keyPath, expiresAt: CertificateStore.expiryOf(pem) }
  }

  static resolveChallenge(request: IssueRequest): 'http' | 'dns' {
    const hasWildcard = request.domains.some((domain) => domain.startsWith('*.'))
    const challenge = request.challenge || (hasWildcard ? 'dns' : 'http')
    if (hasWildcard && challenge !== 'dns') {
      throw new SslError('VALIDATION', 'Wildcard certificates require the DNS-01 challenge (Cloudflare)')
    }
    if (challenge === 'dns' && !request.dnsToken) {
      throw new SslError('VALIDATION', 'DNS-01 requires a Cloudflare API token (Zone.DNS edit permission)')
    }
    return challenge
  }

  private async publishChallenge(dns: DnsProvider | null, authz: any, ch: any, keyAuthorization: string): Promise<void> {
    if (ch.type === 'dns-01') {
      await dns!.createTxt(`_acme-challenge.${authz.identifier.value}`, keyAuthorization)
    } else {
      this.challenges.setAcmeChallenge(ch.token, keyAuthorization)
    }
  }

  private async retractChallenge(dns: DnsProvider | null, ch: any): Promise<void> {
    if (ch.type === 'dns-01') {
      await dns!.removeCreatedRecords().catch(() => {})
    } else {
      this.challenges.removeAcmeChallenge(ch.token)
    }
  }
}
