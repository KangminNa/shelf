import { statCard, sectionHeader, button } from '../../ui/index.js'
import type { AppNavItem } from '../../ui/shell.js'

function formatUptime(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m`
  return `${Math.floor(seconds)}s`
}

export function dashboardPage(apps: AppNavItem[], proxyHostCount: number): string {
  const running = apps.filter((a) => a.running).length
  const memory = process.memoryUsage()

  const appCards = apps.map((app) => `
    <a href="/admin/deploy/projects/${app.id}" class="shelf-module-card">
      <div class="shelf-module-header">
        <div class="shelf-module-icon" style="background:var(--accent-light);">
          <span style="display:inline-flex; width:10px; height:10px; border-radius:50%; background:${app.running ? 'var(--success)' : 'var(--text-muted)'};"></span>
        </div>
        <div>
          <div class="shelf-module-name">${app.name}</div>
          <div class="shelf-module-version">${app.running ? 'running' : 'stopped'}${app.port ? ` · :${app.port}` : ''}</div>
        </div>
      </div>
    </a>`).join('')

  return `
    <div class="shelf-stats">
      ${statCard({ label: 'Apps', value: String(apps.length), sub: `${running} running`, icon: 'package', color: 'blue' })}
      ${statCard({ label: 'Proxy hosts', value: String(proxyHostCount), sub: 'domains routed', icon: 'globe', color: 'green' })}
      ${statCard({ label: 'Uptime', value: formatUptime(process.uptime()), sub: 'since start', icon: 'clock', color: 'green' })}
      ${statCard({ label: 'Memory', value: `${Math.round(memory.rss / 1024 / 1024)} MB`, sub: 'RSS usage', icon: 'cpu', color: 'amber' })}
    </div>

    <div class="shelf-mt-lg">
      ${sectionHeader('Apps', button('New app', { href: '/admin/deploy', variant: 'secondary', icon: 'plus', size: 'sm' }))}
      ${apps.length
        ? `<div class="shelf-modules-grid">${appCards}</div>`
        : `<div style="text-align:center; padding:32px; color:var(--text-muted); font-size:13px;">No apps yet — deploy a Git repository or a Docker image.</div>`}
    </div>

    <div class="shelf-mt-lg">
      ${sectionHeader('Quick actions')}
      <div class="shelf-flex shelf-gap-md">
        ${button('Deploy an app', { href: '/admin/deploy', variant: 'primary', icon: 'zap' })}
        ${button('Add proxy host', { href: '/admin/proxy', variant: 'secondary', icon: 'globe' })}
        ${button('App guide', { href: '/admin/guide', variant: 'secondary', icon: 'file' })}
      </div>
    </div>
  `
}
