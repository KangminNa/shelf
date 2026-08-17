import { el, raw, join, escapeHtml, type Html, type Child, type Attrs } from './element.js'

export { el, raw, join, type Html, type Child, type Attrs }

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
    return escapeHtml(value)
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

  protected hide(id: string): string {
    return `document.getElementById('${id}').style.display='none'`
  }

  protected show(id: string): string {
    return `document.getElementById('${id}').style.display='flex'`
  }

  protected dialog(id: string, title: string, body: Child, maxWidth = 480): Html {
    return el.div(
      { id, style: DIALOG.backdrop },
      el.div(
        { style: `${DIALOG.panel} max-width:${maxWidth}px;` },
        el.div(
          { style: DIALOG.header },
          el.h3({ style: 'font-size:16px; font-weight:600;' }, title),
          el.button({ onclick: this.hide(id), class: 'shelf-btn shelf-btn-ghost shelf-btn-icon' }, raw('&times;'))
        ),
        body
      )
    )
  }

  protected dialogActions(dialogId: string, submitLabel: string, submitId?: string): Html {
    return el.div(
      { style: DIALOG.footer },
      el.button({ type: 'button', onclick: this.hide(dialogId), class: 'shelf-btn shelf-btn-secondary' }, 'Cancel'),
      el.button({ type: 'submit', id: submitId, class: 'shelf-btn shelf-btn-primary' }, submitLabel)
    )
  }

  protected field(label: Child, control: Child, hint?: Child): Html {
    return el.div(
      {},
      el.label({ style: FORM.label }, label),
      control,
      hint ? el.div({ style: FORM.hint }, hint) : null
    )
  }

  protected input(attrs: Attrs): Html {
    return el.input({ ...attrs, style: `${FORM.input}${attrs.style ? ` ${attrs.style}` : ''}` })
  }

  protected textarea(attrs: Attrs, value?: Child): Html {
    return el.textarea({ ...attrs, style: `${FORM.input} ${FORM.mono}` }, value)
  }

  protected checkbox(name: string, label: Child, checked = false, indent = false): Html {
    return el.label(
      { style: `display:flex; align-items:center; gap:6px; font-size:13px; cursor:pointer;${indent ? ' padding-left:22px;' : ''}` },
      el.input({ type: 'checkbox', name, checked }),
      ' ',
      label
    )
  }

  protected sectionHeader(title: Child, actions?: Child): Html {
    return el.div(
      { class: 'shelf-section-header' },
      el.h2({ class: 'shelf-section-title' }, title),
      actions ? el.div({ style: 'display:flex; gap:8px;' }, actions) : null
    )
  }

  protected card(body: Child, style?: string): Html {
    return el.div({ class: 'shelf-card', style }, body)
  }

  protected tableCard(headers: string[], rows: Child): Html {
    return el.div(
      { class: 'shelf-card', style: 'padding:0; overflow:hidden;' },
      el.table(
        { class: 'shelf-table' },
        el.thead({}, el.tr({}, headers.map((header) => el.th({}, header)))),
        el.tbody({}, rows)
      )
    )
  }

  protected emptyState(title: Child, description: Child, action?: Child): Html {
    return el.div(
      { style: 'text-align:center; padding:48px 24px; color:var(--text-secondary);' },
      el.h2({ style: 'font-size:18px; font-weight:600; color:var(--text); margin-bottom:8px;' }, title),
      el.p({ style: 'font-size:13px; margin-bottom:16px;' }, description),
      action
    )
  }

  protected notice(body: Child): Html {
    return el.div(
      { style: 'font-size:12px; color:var(--text-muted); padding:8px 12px; background:var(--bg-tertiary); border-radius:var(--radius);' },
      body
    )
  }

  protected script(body: string): Html {
    return raw(`<script>${body}</script>`)
  }
}
