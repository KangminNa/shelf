import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import type { Logger } from '../../../services/log.js'
import { CertificateStore } from './certificate-store.js'
import { SslError, type CertificateFiles, type CertificateIssuer, type IssueRequest } from './types.js'

const exec = promisify(execFile)
const VALID_DAYS = 3650
const OPENSSL_TIMEOUT_MS = 30_000

export class SelfSignedIssuer implements CertificateIssuer {
  readonly provider = 'selfsigned'

  constructor(
    private readonly store: CertificateStore,
    private readonly log: Logger
  ) {}

  async issue(request: IssueRequest): Promise<CertificateFiles> {
    const domain = request.domains[0]
    const { certPath, keyPath } = this.store.prepareDir(domain)

    try {
      await exec(
        'openssl',
        [
          'req', '-x509', '-newkey', 'rsa:2048', '-sha256', '-nodes', '-days', String(VALID_DAYS),
          '-keyout', keyPath, '-out', certPath,
          '-subj', `/CN=${domain}`,
          '-addext', `subjectAltName=DNS:${domain},DNS:*.${domain}`,
        ],
        { timeout: OPENSSL_TIMEOUT_MS }
      )
    } catch (err: any) {
      const detail = (err.stderr || err.message || '').toString().trim().split('\n').pop()
      throw new SslError('OPENSSL_FAILED', `openssl failed: ${detail}`)
    }

    this.log.info(`self-signed certificate created for ${domain} (+wildcard)`)
    return {
      certPath,
      keyPath,
      expiresAt: Math.floor(Date.now() / 1000) + VALID_DAYS * 86400,
    }
  }

  static coveredDomains(domain: string): string[] {
    return [domain, `*.${domain}`]
  }
}
