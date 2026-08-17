import type { Logger } from '../../../services/log.js'

export interface DnsProvider {
  createTxt(name: string, content: string): Promise<void>
  removeCreatedRecords(): Promise<void>
}

export class DnsError extends Error {
  constructor(public readonly code: string, message: string) {
    super(message)
  }
}

const API_BASE = 'https://api.cloudflare.com/client/v4'
const PROPAGATION_WAIT_MS = 10_000

export class CloudflareDns implements DnsProvider {
  private readonly created: Array<{ zone: string; id: string }> = []

  constructor(
    private readonly token: string,
    private readonly log: Logger
  ) {}

  async createTxt(name: string, content: string): Promise<void> {
    const zone = await this.zoneIdFor(name)
    const response = await this.call(`/zones/${zone}/dns_records`, {
      method: 'POST',
      body: JSON.stringify({ type: 'TXT', name, content, ttl: 60 }),
    })
    this.created.push({ zone, id: response.result.id })
    this.log.info(`cloudflare TXT created: ${name}`)
    await new Promise((resolve) => setTimeout(resolve, PROPAGATION_WAIT_MS))
  }

  async removeCreatedRecords(): Promise<void> {
    for (const { zone, id } of this.created.splice(0)) {
      await this.call(`/zones/${zone}/dns_records/${id}`, { method: 'DELETE' }).catch(() => {})
    }
  }

  private async zoneIdFor(recordName: string): Promise<string> {
    const parts = recordName.split('.')
    for (let i = parts.length - 2; i >= 0; i--) {
      const candidate = parts.slice(i).join('.')
      const response = await this.call(`/zones?name=${candidate}`, { method: 'GET' })
      if (response.result?.length) return response.result[0].id
    }
    throw new DnsError('DNS_ZONE_NOT_FOUND', `No Cloudflare zone found for ${recordName}`)
  }

  private async call(path: string, init: RequestInit): Promise<any> {
    const response = await fetch(`${API_BASE}${path}`, {
      ...init,
      headers: { Authorization: `Bearer ${this.token}`, 'Content-Type': 'application/json' },
    })
    const json: any = await response.json()
    if (!json.success) {
      throw new DnsError('CLOUDFLARE_ERROR', json.errors?.[0]?.message || `Cloudflare API failed (${response.status})`)
    }
    return json
  }
}
