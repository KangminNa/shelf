import { statCard, sectionHeader, badge, table } from '../../ui/index.js'
import type { AppNavItem } from '../../ui/shell.js'

export function systemPage(apps: AppNavItem[], dockerAvailable: boolean) {
  const mem = process.memoryUsage()
  const toMB = (bytes: number) => `${Math.round(bytes / 1024 / 1024)} MB`

  const stats = `
    <div class="shelf-stats">
      ${statCard({ label: 'RSS Memory', value: toMB(mem.rss), icon: 'cpu', color: 'var(--accent)' })}
      ${statCard({ label: 'Heap used', value: toMB(mem.heapUsed), sub: `of ${toMB(mem.heapTotal)}`, icon: 'database', color: 'var(--warning)' })}
      ${statCard({ label: 'External', value: toMB(mem.external), icon: 'hardDrive', color: 'var(--text-muted)' })}
      ${statCard({ label: 'Uptime', value: formatUptime(process.uptime()), icon: 'clock', color: 'var(--success)' })}
    </div>`

  const envRows = [
    ['Node.js', process.version, badge('runtime', 'info')],
    ['Platform', `${process.platform} (${process.arch})`, badge('os', 'info')],
    ['PID', `${process.pid}`, ''],
    ['Port', process.env.PORT || '9666', badge('server', 'success')],
    ['Environment', process.env.NODE_ENV || 'development', badge(process.env.NODE_ENV === 'production' ? 'prod' : 'dev', process.env.NODE_ENV === 'production' ? 'warning' : 'info')],
    ['Docker', dockerAvailable ? 'connected' : 'not reachable', badge(dockerAvailable ? 'ok' : 'down', dockerAvailable ? 'success' : 'danger')],
    ['Apps', `${apps.length} (${apps.filter((a) => a.running).length} running)`, ''],
  ]

  const envTable = `
    <div class="shelf-mt-lg">
      ${sectionHeader('Environment')}
      ${table(['Property', 'Value', ''], envRows)}
    </div>`

  const appRows = apps.map((app) => [
    app.name,
    app.port ? `:${app.port}` : '-',
    app.running ? badge('running', 'success') : badge('stopped', 'warning'),
  ])

  const appTable = apps.length
    ? `
    <div class="shelf-mt-lg">
      ${sectionHeader('App status')}
      ${table(['App', 'Port', 'Status'], appRows)}
    </div>`
    : ''

  return stats + envTable + appTable
}

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.round(seconds % 60)
  if (d > 0) return `${d}d ${h}h ${m}m`
  if (h > 0) return `${h}h ${m}m ${s}s`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}
