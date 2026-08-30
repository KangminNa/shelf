import * as crypto from 'node:crypto'
import type { Alert, NotificationChannel } from './types.js'

const TIMEOUT_MS = 10_000

export class WebhookChannel implements NotificationChannel {
  readonly kind = 'webhook'

  async send(alert: Alert, target: { url: string; secret: string }): Promise<void> {
    const body = JSON.stringify({
      event: alert.event,
      level: alert.level,
      title: alert.title,
      detail: alert.detail,
      app: alert.app ?? null,
      sent_at: Math.floor(Date.now() / 1000),
    })

    const response = await fetch(target.url, {
      method: 'POST',
      headers: {
        'content-type': 'application/json',
        'user-agent': 'shelf-notify/1',
        ...(target.secret ? { 'x-shelf-signature-256': WebhookChannel.sign(target.secret, body) } : {}),
      },
      body,
      signal: AbortSignal.timeout(TIMEOUT_MS),
    })

    if (!response.ok) throw new Error(`${response.status} ${response.statusText}`)
  }

  static sign(secret: string, body: string): string {
    return 'sha256=' + crypto.createHmac('sha256', secret).update(body).digest('hex')
  }
}
