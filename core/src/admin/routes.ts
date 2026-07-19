import { Hono } from 'hono'
import { renderShell, type AppNavItem } from '../ui/shell.js'
import { dashboardPage } from './pages/dashboard.js'
import { systemPage } from './pages/system.js'
import { appGuidePage } from './pages/app-guide.js'

export interface AdminDeps {
  apps(): Promise<AppNavItem[]>
  proxyHostCount(): number
  dockerAvailable(): Promise<boolean>
}

/** 코어 관리 대시보드 (/admin) — Apps/Proxy 관리 페이지는 각 시스템 컨트롤러가 담당 */
export function createAdminRoutes(deps: AdminDeps) {
  const admin = new Hono()

  const render = async (title: string, activePath: string, content: string) =>
    renderShell({ title, activePath, content, apps: await deps.apps() })

  admin.get('/', async (c) => {
    const apps = await deps.apps()
    return c.html(await render('Dashboard', '/admin', dashboardPage(apps, deps.proxyHostCount())))
  })

  admin.get('/system', async (c) => {
    const apps = await deps.apps()
    return c.html(await render('System', '/admin/system', systemPage(apps, await deps.dockerAvailable())))
  })

  admin.get('/guide', async (c) => {
    return c.html(await render('App guide', '/admin/guide', appGuidePage()))
  })

  admin.get('/settings', async (c) => {
    return c.html(await render('Settings', '/admin/settings', settingsPage()))
  })

  // 서버 재시작 (supervisor 필요: docker restart 정책, systemd, pm2 등)
  admin.post('/api/restart', (c) => {
    setTimeout(() => process.exit(0), 300)
    return c.json({ ok: true, data: { message: 'Restarting...' } })
  })

  return admin
}

function settingsPage() {
  return `
    <div class="shelf-card" style="max-width:560px;">
      <div class="shelf-card-header">
        <div class="shelf-card-title">Server settings</div>
      </div>
      <div style="display:flex; flex-direction:column; gap:16px;">
        <div>
          <label style="display:block; font-size:13px; font-weight:500; margin-bottom:4px;">Port</label>
          <input type="text" value="${process.env.PORT || 9666}" disabled
            style="width:120px; padding:8px 12px; border:1px solid var(--border); border-radius:var(--radius); background:var(--bg-tertiary); color:var(--text-secondary); font-size:14px; font-family:var(--font-mono);">
        </div>
        <div>
          <label style="display:block; font-size:13px; font-weight:500; margin-bottom:4px;">Environment</label>
          <input type="text" value="${process.env.NODE_ENV || 'development'}" disabled
            style="width:200px; padding:8px 12px; border:1px solid var(--border); border-radius:var(--radius); background:var(--bg-tertiary); color:var(--text-secondary); font-size:14px; font-family:var(--font-mono);">
        </div>
        <div style="font-size:12px; color:var(--text-muted); padding-top:8px; border-top:1px solid var(--border);">
          Server settings are configured via environment variables or <code style="background:var(--bg-tertiary); padding:2px 6px; border-radius:4px; font-family:var(--font-mono); font-size:12px;">.env</code> file.
        </div>
      </div>
    </div>`
}
