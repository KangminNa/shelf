import { Page, FORM } from '../../ui/page.js'

export class SettingsPage extends Page {
  render(): string {
    return this.card(`
      <div class="shelf-card-header">
        <div class="shelf-card-title">Server settings</div>
      </div>
      <div style="display:flex; flex-direction:column; gap:16px;">
        ${this.readOnlyField('Port', process.env.PORT || '9666', '120px')}
        ${this.readOnlyField('Environment', process.env.NODE_ENV || 'development', '200px')}
        <div style="font-size:12px; color:var(--text-muted); padding-top:8px; border-top:1px solid var(--border);">
          Server settings are configured via environment variables or
          <code style="background:var(--bg-tertiary); padding:2px 6px; border-radius:4px; font-family:var(--font-mono); font-size:12px;">.env</code> file.
        </div>
      </div>`, 'max-width:560px;')
  }

  private readOnlyField(label: string, value: string, width: string): string {
    return `
      <div>
        <label style="${FORM.label}">${label}</label>
        <input type="text" value="${this.escape(value)}" disabled
          style="${FORM.input} width:${width}; background:var(--bg-tertiary); color:var(--text-secondary); font-family:var(--font-mono);">
      </div>`
  }
}
