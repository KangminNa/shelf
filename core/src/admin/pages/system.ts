import { statCard, badge, table } from '../../ui/index.js'
import { Page, el, raw, join, type Html } from '../../ui/page.js'
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
    return join([this.memoryStats(), this.environment(), this.appStatus()]).toString()
  }

  private memoryStats(): Html {
    const mem = process.memoryUsage()
    const toMB = (bytes: number) => `${Math.round(bytes / 1024 / 1024)} MB`
    return el.div(
      { class: 'shelf-stats' },
      raw(statCard({ label: 'RSS Memory', value: toMB(mem.rss), icon: 'cpu', color: 'var(--accent)' })),
      raw(statCard({ label: 'Heap used', value: toMB(mem.heapUsed), sub: `of ${toMB(mem.heapTotal)}`, icon: 'database', color: 'var(--warning)' })),
      raw(statCard({ label: 'External', value: toMB(mem.external), icon: 'hardDrive', color: 'var(--text-muted)' })),
      raw(statCard({ label: 'Uptime', value: this.formatDuration(process.uptime()), icon: 'clock', color: 'var(--success)' }))
    )
  }

  private environment(): Html {
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
    return el.div({ class: 'shelf-mt-lg' }, this.sectionHeader('Environment'), raw(table(['Property', 'Value', ''], rows)))
  }

  private appStatus(): Html | null {
    const { apps } = this.props
    if (!apps.length) return null
    const rows = apps.map((app) => [
      this.escape(app.name),
      app.port ? `:${app.port}` : '-',
      app.running ? badge('running', 'success') : badge('stopped', 'warning'),
    ])
    return el.div({ class: 'shelf-mt-lg' }, this.sectionHeader('App status'), raw(table(['App', 'Port', 'Status'], rows)))
  }
}
