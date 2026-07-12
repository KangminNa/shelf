import type { NotifyService, EventBus } from '../types.js'

export function createNotifyService(slug: string, events: EventBus): NotifyService {
  return {
    async send(message, opts) {
      const notification = {
        module: slug,
        message,
        to: opts?.to || 'admin',
        channel: opts?.channel || 'default',
        timestamp: Date.now(),
      }

      events.emit('core:notification', notification)
      console.log(`[${slug}] notify: ${message}`)
    },
  }
}
