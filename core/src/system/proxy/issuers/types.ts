export class SslError extends Error {
  constructor(public readonly code: string, message: string) {
    super(message)
  }
}

export interface CertificateFiles {
  certPath: string
  keyPath: string
  expiresAt: number
}

export interface IssueRequest {
  domains: string[]
  email?: string
  challenge?: 'http' | 'dns'
  dnsToken?: string
}

export interface CertificateIssuer {
  readonly provider: string
  issue(request: IssueRequest): Promise<CertificateFiles>
}
