export class PublicAddress {
  constructor(
    private readonly domain: string | null,
    private readonly hasCertificate: (domain: string) => boolean
  ) {}

  get configured(): boolean {
    return !!this.domain
  }

  get secure(): boolean {
    return !!this.domain && this.hasCertificate(this.domain)
  }

  urlFor(path: string): string | null {
    if (!this.domain) return null
    return `${this.secure ? 'https' : 'http'}://${this.domain}${path}`
  }
}
