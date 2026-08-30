import { Page, el, join, submits, type Html, type Child } from '../../ui/page.js'
import type { Channel, Delivery } from './repositories.js'

const MUTED = 'font-size:12px; color:var(--text-muted);'
const MONO = 'font-family:var(--font-mono); font-size:12px;'

export class NotificationsPage extends Page {
  constructor(private readonly props: { channels: Channel[]; deliveries: Delivery[] }) {
    super()
  }

  render(): string {
    const { channels, deliveries } = this.props
    return join([
      this.sectionHeader(`Notification channels (${channels.length})`, this.openButton('add-dialog', '+ Add webhook')),
      channels.length
        ? this.tableCard(['URL', 'Secret', 'Note', 'Enabled', ''], channels.map((channel) => this.row(channel)))
        : this.emptyState(
            'No channels yet',
            '장애가 생겼을 때 Shelf가 POST할 주소를 등록하세요. Discord·Slack의 incoming webhook URL도 그대로 씁니다.',
            this.openButton('add-dialog', '+ Add webhook', 'primary', '')
          ),
      this.explanation(),
      deliveries.length ? this.history(deliveries) : null,
      this.addDialog(),
    ]).toString()
  }

  private row(channel: Channel): Html {
    return el.tr(
      {},
      el.td({ style: `${MONO} max-width:280px; overflow:hidden; text-overflow:ellipsis;` }, channel.url),
      el.td({}, channel.secret ? this.badge('signed', 'success') : this.badge('unsigned', 'warning')),
      el.td({ style: MUTED }, channel.description || '-'),
      el.td({}, channel.enabled ? this.badge('on', 'success') : this.badge('off')),
      el.td(
        {},
        el.div(
          { style: 'display:flex; gap:4px; justify-content:flex-end;' },
          this.actionButton('POST', `/api/notify/channels/${channel.id}/test`, 'Test', { busy: 'Sending...', then: 'none' }),
          this.actionButton('PATCH', `/api/notify/channels/${channel.id}`, channel.enabled ? 'Disable' : 'Enable', {
            variant: 'secondary',
          }),
          this.actionButton('DELETE', `/api/notify/channels/${channel.id}`, 'Delete', {
            danger: true,
            confirm: `Delete this channel?\n${channel.url}`,
          })
        )
      )
    )
  }

  private badge(text: Child, variant = 'info'): Html {
    return el.span({ class: `shelf-badge shelf-badge-${variant}` }, text)
  }

  private explanation(): Html {
    return this.card(
      [
        el.div({ style: 'font-size:13px; font-weight:600; margin-bottom:8px;' }, '언제 알림이 가나'),
        el.table(
          { style: 'font-size:13px; width:100%;' },
          [
            ['앱이 멈췄을 때', '컨테이너가 죽거나 응답이 끊긴 순간 한 번. 직접 Stop한 앱은 알리지 않습니다'],
            ['앱이 돌아왔을 때', '멈췄던 앱이 다시 실행되면 한 번'],
            ['배포가 실패했을 때', '빌드나 컨테이너 교체가 실패한 경우'],
            ['인증서 갱신이 실패했을 때', '자동 갱신이 실패해 만료가 다가오는 경우'],
          ].map(([when, what]) =>
            el.tr(
              {},
              el.td({ style: 'padding:4px 12px 4px 0; white-space:nowrap; color:var(--text-muted);' }, when),
              el.td({}, what)
            )
          )
        ),
        this.notice(
          'POST 본문은 {event, level, title, detail, app, sent_at} 입니다. Secret을 넣으면 x-shelf-signature-256 헤더에 HMAC-SHA256 서명이 붙습니다.'
        ),
      ],
      'margin-top:16px;'
    )
  }

  private history(deliveries: Delivery[]): Html {
    return el.div(
      { class: 'shelf-mt-lg' },
      this.sectionHeader('Recent deliveries'),
      this.tableCard(
        ['Time', 'Event', 'Title', 'Result'],
        deliveries.map((delivery) =>
          el.tr(
            {},
            el.td({ style: MUTED }, this.formatDateTime(delivery.created_at)),
            el.td({}, el.code({ style: MONO }, delivery.event)),
            el.td({}, delivery.title),
            el.td({}, delivery.ok ? this.badge('sent', 'success') : this.badge(delivery.detail || 'failed', 'danger'))
          )
        )
      )
    )
  }

  private addDialog(): Html {
    return this.dialog(
      'add-dialog',
      'Add webhook channel',
      el.form(
        { style: 'display:flex; flex-direction:column; gap:14px;', ...submits('POST', '/api/notify/channels') },
        this.field(
          'Webhook URL',
          this.input({ type: 'url', name: 'url', placeholder: 'https://discord.com/api/webhooks/...', required: true }),
          'Discord·Slack의 incoming webhook URL을 그대로 넣어도 됩니다'
        ),
        this.field(
          'Secret (선택)',
          this.input({ type: 'password', name: 'secret', autocomplete: 'off', placeholder: 'openssl rand -hex 32', 'data-omit-empty': '' }),
          '넣으면 x-shelf-signature-256 헤더로 본문을 서명합니다'
        ),
        this.field('Note (선택)', this.input({ type: 'text', name: 'description', placeholder: '내 디스코드 #alerts' })),
        this.dialogActions('add-dialog', 'Add channel')
      )
    )
  }
}
