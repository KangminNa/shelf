const HTML_ESCAPES: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}

export const FORM = {
  input: `width:100%; padding:8px 12px; border:1px solid var(--border); border-radius:var(--radius); background:var(--bg); color:var(--text); font-size:14px;`,
  label: `display:block; font-size:13px; font-weight:500; margin-bottom:4px;`,
  hint: `font-size:12px; color:var(--text-muted); margin-top:4px;`,
  mono: `font-family:var(--font-mono); font-size:12px; resize:vertical;`,
} as const

export const DIALOG = {
  backdrop: `display:none; position:fixed; inset:0; background:rgba(0,0,0,0.4); z-index:100; align-items:center; justify-content:center;`,
  panel: `background:var(--bg); border:1px solid var(--border); border-radius:var(--radius-lg); padding:24px; width:100%; box-shadow:var(--shadow);`,
  header: `display:flex; justify-content:space-between; align-items:center; margin-bottom:20px;`,
  body: `display:flex; flex-direction:column; gap:14px;`,
  footer: `display:flex; gap:8px; justify-content:flex-end;`,
} as const

export abstract class Page {
  abstract render(): string

  protected escape(value: unknown): string {
    return String(value ?? '').replace(/[&<>"']/g, (ch) => HTML_ESCAPES[ch])
  }

  protected formatDate(unixSeconds: number): string {
    return new Date(unixSeconds * 1000).toLocaleDateString()
  }

  protected formatDateTime(unixSeconds: number): string {
    return new Date(unixSeconds * 1000).toLocaleString()
  }

  protected formatDuration(seconds: number): string {
    const days = Math.floor(seconds / 86400)
    const hours = Math.floor((seconds % 86400) / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    if (days > 0) return `${days}d ${hours}h`
    if (hours > 0) return `${hours}h ${minutes}m`
    if (minutes > 0) return `${minutes}m`
    return `${Math.floor(seconds)}s`
  }

  protected dialog(id: string, title: string, body: string, maxWidth = 480): string {
    return `
      <div id="${id}" style="${DIALOG.backdrop}">
        <div style="${DIALOG.panel} max-width:${maxWidth}px;">
          <div style="${DIALOG.header}">
            <h3 style="font-size:16px; font-weight:600;">${title}</h3>
            <button onclick="document.getElementById('${id}').style.display='none'" class="shelf-btn shelf-btn-ghost shelf-btn-icon">&times;</button>
          </div>
          ${body}
        </div>
      </div>`
  }

  protected dialogActions(dialogId: string, submitLabel: string, submitId = ''): string {
    return `
      <div style="${DIALOG.footer}">
        <button type="button" onclick="document.getElementById('${dialogId}').style.display='none'" class="shelf-btn shelf-btn-secondary">Cancel</button>
        <button type="submit" ${submitId ? `id="${submitId}"` : ''} class="shelf-btn shelf-btn-primary">${submitLabel}</button>
      </div>`
  }

  protected field(label: string, control: string, hint = ''): string {
    return `
      <div>
        <label style="${FORM.label}">${label}</label>
        ${control}
        ${hint ? `<div style="${FORM.hint}">${hint}</div>` : ''}
      </div>`
  }

  protected checkbox(name: string, label: string, checked = false, indent = false): string {
    return `
      <label style="display:flex; align-items:center; gap:6px; font-size:13px; cursor:pointer;${indent ? ' padding-left:22px;' : ''}">
        <input type="checkbox" name="${name}" ${checked ? 'checked' : ''}> ${label}
      </label>`
  }

  protected sectionHeader(title: string, actions = ''): string {
    return `
      <div class="shelf-section-header">
        <h2 class="shelf-section-title">${title}</h2>
        ${actions ? `<div style="display:flex; gap:8px;">${actions}</div>` : ''}
      </div>`
  }

  protected card(body: string, style = ''): string {
    return `<div class="shelf-card"${style ? ` style="${style}"` : ''}>${body}</div>`
  }

  protected tableCard(headers: string[], rows: string): string {
    return `
      <div class="shelf-card" style="padding:0; overflow:hidden;">
        <table class="shelf-table">
          <thead><tr>${headers.map((h) => `<th>${h}</th>`).join('')}</tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`
  }

  protected emptyState(title: string, description: string, action = ''): string {
    return `
      <div style="text-align:center; padding:48px 24px; color:var(--text-secondary);">
        <h2 style="font-size:18px; font-weight:600; color:var(--text); margin-bottom:8px;">${title}</h2>
        <p style="font-size:13px; margin-bottom:16px;">${description}</p>
        ${action}
      </div>`
  }

  protected script(body: string): string {
    return `<script>${body}</script>`
  }
}
