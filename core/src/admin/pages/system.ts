import { statCard, sectionHeader, badge, table } from '../../ui/index.js'
import { Page } from '../../ui/page.js'
import type { AppNavItem } from '../../ui/shell.js'

export interface SystemProps {
  apps: AppNavItem[]
  dockerAvailable: boolean
}

export class SystemPage extends Page {
  constructor(private readonly props: SystemProps) {
    super()
  }

  render(): string {
    return this.memoryStats() + this.environment() + this.appStatus()
  }

  private memoryStats(): string {
    const mem = process.memoryUsage()
    const toMB = (bytes: number) => `${Math.round(bytes / 1024 / 1024)} MB`
    return `
      <div class="shelf-stats">
        ${statCard({ label: 'RSS Memory', value: toMB(mem.rss), icon: 'cpu', color: 'var(--accent)' })}
        ${statCard({ label: 'Heap used', value: toMB(mem.heapUsed), sub: `of ${toMB(mem.heapTotal)}`, icon: 'database', color: 'var(--warning)' })}
        ${statCard({ label: 'External', value: toMB(mem.external), icon: 'hardDrive', color: 'var(--text-muted)' })}
        ${statCard({ label: 'Uptime', value: this.formatDuration(process.uptime()), icon: 'clock', color: 'var(--success)' })}
      </div>`
  }

  private environment(): string {
    const { apps, dockerAvailable } = this.props
    const isProduction = process.env.NODE_ENV === 'production'
    const rows = [
      ['Node.js', process.version, badge('runtime', 'info')],
      ['Platform', `${process.platform} (${process.arch})`, badge('os', 'info')],
      ['PID', `${process.pid}`, ''],
      ['Port', process.env.PORT || '9666', badge('server', 'success')],
      ['Environment', process.env.NODE_ENV || 'development', badge(isProduction ? 'prod' : 'dev', isProduction ? 'warning' : 'info')],
      ['Docker', dockerAvailable ? 'connected' : 'not reachable', badge(dockerAvailable ? 'ok' : 'down', dockerAvailable ? 'success' : 'danger')],
      ['Apps', `${apps.length} (${apps.filter((app) => app.running).length} running)`, ''],
    ]
    return `
      <div class="shelf-mt-lg">
        ${sectionHeader('Environment')}
        ${table(['Property', 'Value', ''], rows)}
      </div>`
  }

  private appStatus(): string {
    const { apps } = this.props
    if (!apps.length) return ''
    const rows = apps.map((app) => [
      this.escape(app.name),
      app.port ? `:${app.port}` : '-',
      app.running ? badge('running', 'success') : badge('stopped', 'warning'),
    ])
    return `
      <div class="shelf-mt-lg">
        ${sectionHeader('App status')}
        ${table(['App', 'Port', 'Status'], rows)}
      </div>`
  }
}
