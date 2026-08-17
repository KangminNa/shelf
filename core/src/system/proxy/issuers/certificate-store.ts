import * as crypto from 'node:crypto'
import { writeFileSync, mkdirSync } from 'node:fs'
import { join } from 'node:path'

export class CertificateStore {
  constructor(private readonly rootDir: string) {
    mkdirSync(rootDir, { recursive: true })
  }

  pathsFor(domain: string): { dir: string; certPath: string; keyPath: string } {
    const dir = join(this.rootDir, CertificateStore.storeName(domain))
    return { dir, certPath: join(dir, 'cert.pem'), keyPath: join(dir, 'key.pem') }
  }

  save(domain: string, certPem: string, keyPem: string): { certPath: string; keyPath: string } {
    const { dir, certPath, keyPath } = this.pathsFor(domain)
    mkdirSync(dir, { recursive: true })
    writeFileSync(certPath, certPem)
    writeFileSync(keyPath, keyPem)
    return { certPath, keyPath }
  }

  prepareDir(domain: string): { certPath: string; keyPath: string } {
    const { dir, certPath, keyPath } = this.pathsFor(domain)
    mkdirSync(dir, { recursive: true })
    return { certPath, keyPath }
  }

  static expiryOf(certPem: string): number {
    try {
      return Math.floor(new Date(new crypto.X509Certificate(certPem).validTo).getTime() / 1000)
    } catch {
      return 0
    }
  }

  private static storeName(domain: string): string {
    return domain.replace(/^\*\./, 'wildcard.')
  }
}
