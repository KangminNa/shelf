import { join } from 'node:path'
import type { Hono } from 'hono'
import { AppDatabase } from '../../db/database.js'
import { Logger } from '../../services/log.js'
import type { EventBus } from '../../services/events.js'
import { ChannelRepository, DeliveryRepository, type Channel } from './repositories.js'
import { WebhookChannel } from './channels/webhook.js'
import { ALERTS, ALERT_EVENTS } from './alerts.js'
import type { Alert } from './channels/types.js'
import { NotifyController } from './controller.js'

export type { Channel, Delivery } from './repositories.js'
export type { Alert } from './channels/types.js'

export class NotifySystem {
  readonly channels: ChannelRepository
  readonly deliveries: DeliveryRepository
  private readonly webhook = new WebhookChannel()
  private readonly controller: NotifyController
  private readonly log = new Logger('notify')

  constructor(events: EventBus) {
    const db = new AppDatabase('notify', join(process.cwd(), 'core', 'migrations', 'notify'))
    this.channels = new ChannelRepository(db.raw)
    this.deliveries = new DeliveryRepository(db.raw)
    this.controller = new NotifyController(this)

    for (const event of ALERT_EVENTS) {
      events.on(event, (payload: unknown) => this.dispatch(ALERTS[event](payload)))
    }
    this.log.info(`notify system ready (${this.channels.active().length} active channel(s))`)
  }

  get logger(): Logger {
    return this.log
  }

  get api(): Hono {
    return this.controller.api
  }

  get pages(): Hono {
    return this.controller.pages
  }

  async dispatch(alert: Alert): Promise<void> {
    for (const channel of this.channels.active()) await this.deliver(channel, alert)
    this.deliveries.prune()
  }

  async deliver(channel: Channel, alert: Alert): Promise<void> {
    try {
      await this.webhook.send(alert, channel)
      this.record(channel, alert, true, '')
    } catch (err: any) {
      this.log.error(`delivery to channel ${channel.id} failed: ${err.message}`)
      this.record(channel, alert, false, err.message)
      throw err
    }
  }

  private record(channel: Channel, alert: Alert, ok: boolean, detail: string): void {
    this.deliveries.create({
      channel_id: channel.id,
      event: alert.event,
      title: alert.title,
      ok: ok ? 1 : 0,
      detail,
    })
  }
}
