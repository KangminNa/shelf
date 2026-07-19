import { css } from './styles.js'
import { icon } from './icons.js'

export interface AppNavItem {
  id: number
  name: string
  running: boolean
  port: number | null
}

interface ShellOpts {
  title: string
  activePath: string
  content: string
  apps?: AppNavItem[]
  previewUrl?: string
}

/**
 * 관리 화면 공통 셸 (사이드바 + 탑바).
 * previewUrl이 있으면 오른쪽에 라이브 프리뷰 패널을 붙인다.
 */
export function renderShell(opts: ShellOpts): string {
  const { title, activePath, content, apps = [], previewUrl } = opts

  const coreNav = [
    { label: 'Dashboard', path: '/admin', icon: 'home' as const },
    { label: 'Apps', path: '/admin/deploy', icon: 'package' as const },
    { label: 'Proxy', path: '/admin/proxy', icon: 'globe' as const },
    { label: 'System', path: '/admin/system', icon: 'cpu' as const },
    { label: 'Settings', path: '/admin/settings', icon: 'settings' as const },
  ]

  const isActive = (path: string) => {
    if (path === '/admin') return activePath === '/admin'
    return activePath.startsWith(path)
  }

  const sidebarNav = coreNav
    .map(
      (item) => `
      <a href="${item.path}" class="shelf-nav-item${isActive(item.path) ? ' active' : ''}">
        ${icon(item.icon)}
        ${item.label}
      </a>`
    )
    .join('')

  const appNav = apps
    .map(
      (app) => `
      <div style="display:flex; align-items:center;">
        <a href="/admin/deploy/projects/${app.id}" class="shelf-nav-item${activePath === `/admin/deploy/projects/${app.id}` ? ' active' : ''}" style="flex:1;">
          <span style="display:inline-flex; width:8px; height:8px; border-radius:50%; background:${app.running ? 'var(--success)' : 'var(--text-muted)'};"></span>
          ${app.name}
        </a>
        ${app.port ? `<a href="http://localhost:${app.port}" target="_blank" title="Open app" style="display:inline-flex; padding:6px; opacity:0.45; color:inherit;">${icon('link', 14)}</a>` : ''}
      </div>`
    )
    .join('')

  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>${title} - Shelf</title>
  <style>${css}</style>
</head>
<body>
  <div class="shelf-layout">
    <aside class="shelf-sidebar">
      <div class="shelf-sidebar-header">
        <a href="/admin" class="shelf-sidebar-logo">
          ${icon('server')}
          Shelf
        </a>
        <span class="shelf-sidebar-version">v0.1.0</span>
      </div>

      <nav class="shelf-sidebar-nav">
        <div class="shelf-nav-section">
          <div class="shelf-nav-label">Core</div>
          ${sidebarNav}
        </div>

        ${
          apps.length
            ? `<div class="shelf-nav-section">
            <div class="shelf-nav-label">Apps</div>
            ${appNav}
          </div>`
            : ''
        }
      </nav>

      <div class="shelf-sidebar-footer">
        <div class="shelf-sidebar-status">
          <span class="shelf-status-dot"></span>
          Running on :${process.env.PORT || 9666}
        </div>
      </div>
    </aside>

    <div class="shelf-main">
      <header class="shelf-topbar">
        <h1 class="shelf-topbar-title">${title}</h1>
        <div class="shelf-topbar-actions">
          <a href="/admin/settings" class="shelf-btn shelf-btn-ghost shelf-btn-icon" title="Settings">
            ${icon('settings')}
          </a>
          <button onclick="fetch('/api/auth/logout',{method:'POST'}).then(()=>location.href='/login')" class="shelf-btn shelf-btn-ghost shelf-btn-icon" title="Sign out">
            ${icon('power')}
          </button>
        </div>
      </header>

      ${previewUrl ? previewLayout(content, previewUrl) : `
      <main class="shelf-content">
        ${content}
      </main>
      `}
    </div>
  </div>
</body>
</html>`
}

function previewLayout(content: string, previewUrl: string): string {
  return `
      <div style="display:flex; height:calc(100vh - var(--topbar-h)); overflow:hidden;">
        <main class="shelf-content" style="flex:1; min-width:0; overflow:auto;">
          ${content}
        </main>
        <aside id="shelf-preview" style="width:42%; min-width:320px; flex-shrink:0; border-left:1px solid var(--border); display:flex; flex-direction:column; background:var(--bg-secondary);">
          <div style="display:flex; align-items:center; gap:8px; padding:10px 16px; border-bottom:1px solid var(--border); font-size:13px;">
            <span style="display:inline-flex; width:8px; height:8px; border-radius:50%; background:var(--success);"></span>
            <span style="font-weight:600;">Live preview</span>
            <code style="font-family:var(--font-mono); font-size:12px; color:var(--text-muted);">${previewUrl}</code>
            <div style="margin-left:auto; display:flex; gap:4px;">
              <button onclick="document.getElementById('shelf-preview-frame').contentWindow.location.reload()" class="shelf-btn shelf-btn-ghost shelf-btn-sm shelf-btn-icon" title="Refresh">${icon('refresh', 15)}</button>
              <a href="${previewUrl}" target="_blank" class="shelf-btn shelf-btn-ghost shelf-btn-sm shelf-btn-icon" title="Open in new tab">${icon('link', 15)}</a>
              <button onclick="togglePreview()" class="shelf-btn shelf-btn-ghost shelf-btn-sm shelf-btn-icon" title="Hide preview">${icon('eye', 15)}</button>
            </div>
          </div>
          <iframe id="shelf-preview-frame" src="${previewUrl}" style="flex:1; border:0; width:100%; background:#fff;"></iframe>
        </aside>
        <button id="shelf-preview-show" onclick="togglePreview()" title="Show preview"
          style="display:none; position:fixed; right:16px; bottom:16px; z-index:50; padding:10px 14px; border:1px solid var(--border); border-radius:999px; background:var(--bg); color:var(--text); cursor:pointer; box-shadow:var(--shadow); font-size:13px; align-items:center; gap:6px;">
          ${icon('eye', 15)} Preview
        </button>
      </div>
      <script>
        function togglePreview() {
          const panel = document.getElementById('shelf-preview');
          const show = document.getElementById('shelf-preview-show');
          const hidden = panel.style.display === 'none';
          panel.style.display = hidden ? 'flex' : 'none';
          show.style.display = hidden ? 'none' : 'inline-flex';
          localStorage.setItem('shelf-preview-hidden', hidden ? '0' : '1');
        }
        if (localStorage.getItem('shelf-preview-hidden') === '1') togglePreview();
      </script>
  `
}
