import type { ExternalApiClient, ExternalApiClients, ExternalApiConfig } from '../types.js'
import type { ConfigService } from '../types.js'

export async function createExternalApiClients(
  configs: ExternalApiConfig[],
  configService: ConfigService
): Promise<ExternalApiClients> {
  const clients: ExternalApiClients = {}

  for (const config of configs) {
    const secret = await configService.get<string>(`api_key_${config.key}`)
    clients[config.key] = createClient(config, secret || '')
  }

  return clients
}

function createClient(config: ExternalApiConfig, secret: string): ExternalApiClient {
  async function request<T>(method: string, path: string, body?: any, params?: Record<string, any>): Promise<T> {
    let url = `${config.baseUrl}${path}`

    const queryParams = new URLSearchParams(params || {})
    if (config.auth.type === 'query') {
      queryParams.set(config.auth.param, secret)
    }
    const qs = queryParams.toString()
    if (qs) url += `?${qs}`

    const headers: Record<string, string> = { 'Content-Type': 'application/json' }

    if (config.auth.type === 'header') {
      const prefix = config.auth.prefix ? `${config.auth.prefix} ` : ''
      headers[config.auth.header] = `${prefix}${secret}`
    } else if (config.auth.type === 'basic') {
      headers['Authorization'] = `Basic ${Buffer.from(secret).toString('base64')}`
    }

    const res = await fetch(url, {
      method,
      headers,
      ...(body ? { body: JSON.stringify(body) } : {}),
    })

    if (!res.ok) {
      const text = await res.text().catch(() => '')
      throw new Error(`[external-api] ${config.key} ${method} ${path} → ${res.status}: ${text}`)
    }

    return res.json() as T
  }

  return {
    get: (path, opts) => request('GET', path, undefined, opts?.params),
    post: (path, body) => request('POST', path, body),
    put: (path, body) => request('PUT', path, body),
    delete: (path) => request('DELETE', path),
  }
}
