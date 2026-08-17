import { CertificateStore } from './certificate-store.js'
import { SslError, type CertificateFiles } from './types.js'

export class ManualUploadIssuer {
  readonly provider = 'manual'

  constructor(private readonly store: CertificateStore) {}

  accept(domain: string, certPem: string, keyPem: string): CertificateFiles {
    if (!certPem.includes('BEGIN CERTIFICATE')) {
      throw new SslError('VALIDATION', 'cert must be a PEM certificate')
    }
    if (!keyPem.includes('PRIVATE KEY')) {
      throw new SslError('VALIDATION', 'key must be a PEM private key')
    }
    const { certPath, keyPath } = this.store.save(domain, certPem, keyPem)
    return { certPath, keyPath, expiresAt: CertificateStore.expiryOf(certPem) }
  }
}
