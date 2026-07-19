import { icon, icons, type IconName } from './icons.js'

function resolveIcon(name: string, size = 20): string {
  if (name in icons) return icon(name as IconName, size)
  return `<span style="font-size:${size}px; line-height:1;">${name}</span>`
}

export function statCard(opts: { label: string; value: string | number; sub?: string; icon?: IconName; color?: string }) {
  const iconColor = opts.color || 'var(--accent)'
  return `
    <div class="shelf-stat">
      <div style="display:flex; align-items:center; justify-content:space-between;">
        <div>
          <div class="shelf-stat-label">${opts.label}</div>
          <div class="shelf-stat-value">${opts.value}</div>
          ${opts.sub ? `<div class="shelf-stat-sub">${opts.sub}</div>` : ''}
        </div>
        ${opts.icon ? `<div style="color:${iconColor}; opacity:0.6;">${icon(opts.icon, 28)}</div>` : ''}
      </div>
    </div>`
}

export function moduleCard(mod: { name: string; displayName: string; version: string; icon: string; description?: string; menu?: any[] }) {
  const iconBg = 'var(--accent-light)'
  const menuCount = mod.menu?.length || 0
  return `
    <a href="/admin/${mod.name}" class="shelf-module-card">
      <div class="shelf-module-header">
        <div class="shelf-module-icon" style="background:${iconBg};">${resolveIcon(mod.icon, 20)}</div>
        <div>
          <div class="shelf-module-name">${mod.displayName || mod.name}</div>
          <div class="shelf-module-version">v${mod.version}</div>
        </div>
      </div>
      ${mod.description ? `<div class="shelf-module-desc">${mod.description}</div>` : ''}
      <div class="shelf-module-meta">
        <span class="shelf-module-meta-item">${icon('layout', 14)} ${menuCount} pages</span>
        <span class="shelf-badge shelf-badge-success">active</span>
      </div>
    </a>`
}

export function badge(text: string, variant: 'success' | 'warning' | 'danger' | 'info' = 'info') {
  return `<span class="shelf-badge shelf-badge-${variant}">${text}</span>`
}

export function button(text: string, opts: { href?: string; variant?: 'primary' | 'secondary' | 'ghost'; icon?: IconName; size?: 'sm' } = {}) {
  const tag = opts.href ? 'a' : 'button'
  const cls = `shelf-btn shelf-btn-${opts.variant || 'secondary'}${opts.size === 'sm' ? ' shelf-btn-sm' : ''}`
  const hrefAttr = opts.href ? ` href="${opts.href}"` : ''
  const iconHtml = opts.icon ? icon(opts.icon, opts.size === 'sm' ? 14 : 16) : ''
  return `<${tag} class="${cls}"${hrefAttr}>${iconHtml}${text}</${tag}>`
}

export function emptyState(opts: { icon: IconName; title: string; desc: string; action?: string; actionHref?: string }) {
  return `
    <div class="shelf-empty">
      ${icon(opts.icon, 48)}
      <div class="shelf-empty-title">${opts.title}</div>
      <div class="shelf-empty-desc">${opts.desc}</div>
      ${opts.action ? button(opts.action, { href: opts.actionHref, variant: 'primary', icon: 'plus' }) : ''}
    </div>`
}

export function sectionHeader(title: string, actions = '') {
  return `
    <div class="shelf-section-header">
      <h2 class="shelf-section-title">${title}</h2>
      <div class="shelf-flex shelf-items-center shelf-gap-sm">${actions}</div>
    </div>`
}

export function table(headers: string[], rows: string[][]) {
  const ths = headers.map(h => `<th>${h}</th>`).join('')
  const trs = rows.map(row => `<tr>${row.map(cell => `<td>${cell}</td>`).join('')}</tr>`).join('')
  return `
    <div class="shelf-card" style="padding:0; overflow:hidden;">
      <table class="shelf-table">
        <thead><tr>${ths}</tr></thead>
        <tbody>${trs}</tbody>
      </table>
    </div>`
}
