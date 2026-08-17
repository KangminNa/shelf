import { statCard, button } from '../../ui/index.js'
import { Page, el, raw, join, type Html } from '../../ui/page.js'
import type { AppNavItem } from '../../ui/shell.js'

export interface DashboardProps {
  apps: AppNavItem[]
  proxyHostCount: number
}

export class DashboardPage extends Page {
  constructor(private readonly props: DashboardProps) {
    super()
  }

  render(): string {
    return join([this.stats(), this.appSection(), this.quickActions()]).toString()
  }

  private stats(): Html {
    const { apps, proxyHostCount } = this.props
    const running = apps.filter((app) => app.running).length
    const memoryMb = Math.round(process.memoryUsage().rss / 1024 / 1024)
    return el.div(
      { class: 'shelf-stats' },
      raw(statCard({ label: 'Apps', value: String(apps.length), sub: `${running} running`, icon: 'package', color: 'blue' })),
      raw(statCard({ label: 'Proxy hosts', value: String(proxyHostCount), sub: 'domains routed', icon: 'globe', color: 'green' })),
      raw(statCard({ label: 'Uptime', value: this.formatDuration(process.uptime()), sub: 'since start', icon: 'clock', color: 'green' })),
      raw(statCard({ label: 'Memory', value: `${memoryMb} MB`, sub: 'RSS usage', icon: 'cpu', color: 'amber' }))
    )
  }

  private appSection(): Html {
    const { apps } = this.props
    const body = apps.length
      ? el.div({ class: 'shelf-modules-grid' }, apps.map((app) => this.appCard(app)))
      : el.div({ style: 'text-align:center; padding:32px; color:var(--text-muted); font-size:13px;' }, 'No apps yet — deploy a Git repository or a Docker image.')

    return el.div(
      { class: 'shelf-mt-lg' },
      this.sectionHeader('Apps', raw(button('New app', { href: '/admin/deploy', variant: 'secondary', icon: 'plus', size: 'sm' }))),
      body
    )
  }

  private appCard(app: AppNavItem): Html {
    const dotColor = app.running ? 'var(--success)' : 'var(--text-muted)'
    return el.a(
      { href: `/admin/deploy/projects/${app.id}`, class: 'shelf-module-card' },
      el.div(
        { class: 'shelf-module-header' },
        el.div(
          { class: 'shelf-module-icon', style: 'background:var(--accent-light);' },
          el.span({ style: `display:inline-flex; width:10px; height:10px; border-radius:50%; background:${dotColor};` })
        ),
        el.div(
          {},
          el.div({ class: 'shelf-module-name' }, app.name),
          el.div({ class: 'shelf-module-version' }, `${app.running ? 'running' : 'stopped'}${app.port ? ` · :${app.port}` : ''}`)
        )
      )
    )
  }

  private quickActions(): Html {
    return el.div(
      { class: 'shelf-mt-lg' },
      this.sectionHeader('Quick actions'),
      el.div(
        { class: 'shelf-flex shelf-gap-md' },
        raw(button('Deploy an app', { href: '/admin/deploy', variant: 'primary', icon: 'zap' })),
        raw(button('Add proxy host', { href: '/admin/proxy', variant: 'secondary', icon: 'globe' })),
        raw(button('App guide', { href: '/admin/guide', variant: 'secondary', icon: 'file' }))
      )
    )
  }
}
