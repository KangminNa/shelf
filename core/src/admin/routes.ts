import { Hono } from 'hono'
import { join } from 'node:path'
import { renderShell, type AppNavItem } from '../ui/shell.js'
import { HostMetrics } from '../services/host-metrics.js'
import { dataDir } from '../db/database.js'
import { formatBytes, formatPercent, formatDuration } from '../ui/format.js'
import type { Page } from '../ui/page.js'
import { DashboardPage } from './pages/dashboard.js'
import { SystemPage } from './pages/system.js'
import { AppGuidePage } from './pages/app-guide.js'
import { SettingsPage } from './pages/settings.js'

export interface AppUsage {
  name: string
  cpu: number | null
  memory: number | null
}

export interface AdminDeps {
  apps(): Promise<AppNavItem[]>
  proxyHostCount(): number
  dockerAvailable(): Promise<boolean>
  appUsage(): Promise<AppUsage[]>
}

export function createAdminRoutes(deps: AdminDeps) {
  const admin = new Hono()
  const host = new HostMetrics(join(dataDir()))

  const shell = async (title: string, activePath: string, page: Page) =>
    renderShell({ title, activePath, content: page.render(), apps: await deps.apps() })

  admin.get('/', async (c) => {
    const page = new DashboardPage({ apps: await deps.apps(), proxyHostCount: deps.proxyHostCount() })
    return c.html(await shell('Dashboard', '/admin', page))
  })

  admin.get('/system', async (c) => {
    const page = new SystemPage({ apps: await deps.apps(), dockerAvailable: await deps.dockerAvailable() })
    return c.html(await shell('System', '/admin/system', page))
  })

  admin.get('/guide', async (c) => c.html(await shell('App guide', '/admin/guide', new AppGuidePage())))

  admin.get('/settings', async (c) => c.html(await shell('Settings', '/admin/settings', new SettingsPage())))

  admin.get('/metrics', async (c) => {
    const snapshot = host.snapshot()
    const usage = await deps.appUsage()
    return c.json({
      ok: true,
      data: {
        cpu: {
          busy: formatPercent(snapshot.cpu.busy),
          detail: `${snapshot.cpu.cores} cores · load ${snapshot.cpu.load.toFixed(2)}`,
        },
        memory: snapshot.memory
          ? {
              used: formatBytes(snapshot.memory.used),
              detail: `of ${formatBytes(snapshot.memory.total)} · ${formatPercent(snapshot.memory.used / snapshot.memory.total)}`,
            }
          : { used: '—', detail: 'unavailable' },
        disk: snapshot.disk
          ? {
              free: formatBytes(snapshot.disk.total - snapshot.disk.used),
              detail: `free of ${formatBytes(snapshot.disk.total)}`,
            }
          : { free: '—', detail: 'unavailable' },
        uptime: { value: formatDuration(snapshot.uptime), detail: 'host uptime' },
        apps: Object.fromEntries(
          usage.map((app) => [
            app.name,
            { cpu: formatPercent(app.cpu), memory: app.memory === null ? '—' : formatBytes(app.memory) },
          ])
        ),
      },
    })
  })

  admin.post('/api/restart', (c) => {
    setTimeout(() => process.exit(0), 300)
    return c.json({ ok: true, data: { message: 'Restarting...' } })
  })

  return admin
}
