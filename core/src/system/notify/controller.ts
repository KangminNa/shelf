import { Controller, fields, notFound, invalid } from '../../kernel/controller.js'
import type { NotifySystem } from './index.js'
import type { Channel } from './repositories.js'
import { NotificationsPage } from './views.js'

const CHANNEL_FIELDS = ['url', 'secret', 'enabled', 'description'] as const

const URL_RE = /^https?:\/\/[^\s;|&`$<>'"\\]+$/

function sanitize(channel: Channel): Omit<Channel, 'secret'> & { has_secret: boolean } {
  const { secret, ...rest } = channel
  return { ...rest, has_secret: !!secret }
}

export class NotifyController extends Controller {
  constructor(private readonly notify: NotifySystem) {
    super(notify.logger)
    this.registerApi()
    this.registerPages()
  }

  private channel(id: string): Channel {
    return this.notify.channels.find(id) ?? notFound('Channel')
  }

  private registerApi(): void {
    this.get('/channels', () => this.notify.channels.allSorted().map(sanitize))

    this.post('/channels', ({ body }) => {
      if (!URL_RE.test(String(body.url || ''))) invalid('A valid http(s) URL is required')
      return sanitize(
        this.notify.channels.create({
          kind: 'webhook',
          url: body.url,
          secret: body.secret || '',
          description: body.description || '',
          enabled: 1,
        })
      )
    }, 201)

    this.patch('/channels/:id', (req) => {
      if (req.body.url !== undefined && !URL_RE.test(String(req.body.url))) invalid('A valid http(s) URL is required')
      const patch = fields<Channel>(req.body, CHANNEL_FIELDS)
      if (!Object.keys(patch).length) invalid('No fields to update')
      return sanitize(this.notify.channels.update(req.id, patch) ?? notFound('Channel'))
    })

    this.delete('/channels/:id', (req) => {
      this.notify.channels.delete(req.id)
      return null
    })

    this.post('/channels/:id/test', async (req) => {
      await this.notify.deliver(this.channel(req.id), {
        event: 'notify:test',
        level: 'recovery',
        title: 'Shelf test notification',
        detail: 'If you can read this, the channel works.',
      })
      return { message: 'Test notification sent' }
    })
  }

  private registerPages(): void {
    this.page('/', () =>
      new NotificationsPage({
        channels: this.notify.channels.allSorted(),
        deliveries: this.notify.deliveries.recent(20),
      }).render()
    )
  }
}
