import { statCard, button, icon } from '../../ui/index.js'
import { Page, el, raw, join, live, field, type Html, type Child } from '../../ui/page.js'
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
    return el.div(
      { ...live('/admin/metrics', { every: 5000 }) },
      this.stats(),
      this.appSection(),
      this.quickActions()
    ).toString()
  }

  private stats(): Html {
    const { apps, proxyHostCount } = this.props
    const running = apps.filter((app) => app.running).length
    return el.div(
      { class: 'shelf-stats' },
      raw(statCard({ label: 'Apps', value: String(apps.length), sub: `${running} running`, icon: 'package', color: 'blue' })),
      raw(statCard({ label: 'Proxy hosts', value: String(proxyHostCount), sub: 'domains routed', icon: 'globe', color: 'green' })),
      this.metricCard('CPU', 'cpu.busy', 'cpu.detail', 'cpu', 'var(--accent)'),
      this.metricCard('Memory', 'memory.used', 'memory.detail', 'database', 'var(--warning)'),
      this.metricCard('Disk free', 'disk.free', 'disk.detail', 'hardDrive', 'var(--success)'),
      this.metricCard('Host uptime', 'uptime.value', 'uptime.detail', 'clock', 'var(--text-muted)')
    )
  }

  private metricCard(label: string, valuePath: string, detailPath: string, iconName: string, color: string): Html {
    return el.div(
      { class: 'shelf-stat' },
      el.div(
        { style: 'display:flex; align-items:center; justify-content:space-between;' },
        el.div(
          {},
          el.div({ class: 'shelf-stat-label' }, label),
          el.div({ class: 'shelf-stat-value', ...field(valuePath) }, '—'),
          el.div({ class: 'shelf-stat-sub', ...field(detailPath) }, 'measuring...')
        ),
        el.div({ style: `color:${color}; opacity:0.6;` }, raw(icon(iconName as never, 28)))
      )
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
          el.div({ class: 'shelf-module-version' }, `${app.running ? 'running' : 'stopped'}${app.port ? ` · :${app.port}` : ''}`),
          el.div(
            { class: 'shelf-module-version', style: 'font-family:var(--font-mono);' },
            el.span({ ...field(`apps.${app.name}.cpu`) }, '—'),
            ' CPU · ',
            el.span({ ...field(`apps.${app.name}.memory`) }, '—')
          )
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
