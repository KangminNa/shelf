import { Hono } from 'hono'
import { renderShell, type AppNavItem } from '../ui/shell.js'
import type { Page } from '../ui/page.js'
import { DashboardPage } from './pages/dashboard.js'
import { SystemPage } from './pages/system.js'
import { AppGuidePage } from './pages/app-guide.js'
import { SettingsPage } from './pages/settings.js'

export interface AdminDeps {
  apps(): Promise<AppNavItem[]>
  proxyHostCount(): number
  dockerAvailable(): Promise<boolean>
}

export function createAdminRoutes(deps: AdminDeps) {
  const admin = new Hono()

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

  admin.post('/api/restart', (c) => {
    setTimeout(() => process.exit(0), 300)
    return c.json({ ok: true, data: { message: 'Restarting...' } })
  })

  return admin
}
