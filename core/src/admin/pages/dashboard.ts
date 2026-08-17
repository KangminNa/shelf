import { statCard, sectionHeader, button } from '../../ui/index.js'
import { Page } from '../../ui/page.js'
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
    return this.stats() + this.appSection() + this.quickActions()
  }

  private stats(): string {
    const { apps, proxyHostCount } = this.props
    const running = apps.filter((app) => app.running).length
    const memoryMb = Math.round(process.memoryUsage().rss / 1024 / 1024)
    return `
      <div class="shelf-stats">
        ${statCard({ label: 'Apps', value: String(apps.length), sub: `${running} running`, icon: 'package', color: 'blue' })}
        ${statCard({ label: 'Proxy hosts', value: String(proxyHostCount), sub: 'domains routed', icon: 'globe', color: 'green' })}
        ${statCard({ label: 'Uptime', value: this.formatDuration(process.uptime()), sub: 'since start', icon: 'clock', color: 'green' })}
        ${statCard({ label: 'Memory', value: `${memoryMb} MB`, sub: 'RSS usage', icon: 'cpu', color: 'amber' })}
      </div>`
  }

  private appSection(): string {
    const { apps } = this.props
    const body = apps.length
      ? `<div class="shelf-modules-grid">${apps.map((app) => this.appCard(app)).join('')}</div>`
      : `<div style="text-align:center; padding:32px; color:var(--text-muted); font-size:13px;">No apps yet — deploy a Git repository or a Docker image.</div>`
    return `
      <div class="shelf-mt-lg">
        ${sectionHeader('Apps', button('New app', { href: '/admin/deploy', variant: 'secondary', icon: 'plus', size: 'sm' }))}
        ${body}
      </div>`
  }

  private appCard(app: AppNavItem): string {
    const dotColor = app.running ? 'var(--success)' : 'var(--text-muted)'
    return `
      <a href="/admin/deploy/projects/${app.id}" class="shelf-module-card">
        <div class="shelf-module-header">
          <div class="shelf-module-icon" style="background:var(--accent-light);">
            <span style="display:inline-flex; width:10px; height:10px; border-radius:50%; background:${dotColor};"></span>
          </div>
          <div>
            <div class="shelf-module-name">${this.escape(app.name)}</div>
            <div class="shelf-module-version">${app.running ? 'running' : 'stopped'}${app.port ? ` · :${app.port}` : ''}</div>
          </div>
        </div>
      </a>`
  }

  private quickActions(): string {
    return `
      <div class="shelf-mt-lg">
        ${sectionHeader('Quick actions')}
        <div class="shelf-flex shelf-gap-md">
          ${button('Deploy an app', { href: '/admin/deploy', variant: 'primary', icon: 'zap' })}
          ${button('Add proxy host', { href: '/admin/proxy', variant: 'secondary', icon: 'globe' })}
          ${button('App guide', { href: '/admin/guide', variant: 'secondary', icon: 'file' })}
        </div>
      </div>`
  }
}
