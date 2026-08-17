import { Page, FORM, el, type Html } from '../../ui/page.js'

export class SettingsPage extends Page {
  render(): string {
    return this.card(
      [
        el.div({ class: 'shelf-card-header' }, el.div({ class: 'shelf-card-title' }, 'Server settings')),
        el.div(
          { style: 'display:flex; flex-direction:column; gap:16px;' },
          this.readOnlyField('Port', process.env.PORT || '9666', '120px'),
          this.readOnlyField('Environment', process.env.NODE_ENV || 'development', '200px'),
          el.div(
            { style: 'font-size:12px; color:var(--text-muted); padding-top:8px; border-top:1px solid var(--border);' },
            'Server settings are configured via environment variables or ',
            el.code({ style: 'background:var(--bg-tertiary); padding:2px 6px; border-radius:4px; font-family:var(--font-mono); font-size:12px;' }, '.env'),
            ' file.'
          )
        ),
      ],
      'max-width:560px;'
    ).toString()
  }

  private readOnlyField(label: string, value: string, width: string): Html {
    return this.field(
      label,
      el.input({
        type: 'text',
        value,
        disabled: true,
        style: `${FORM.input} width:${width}; background:var(--bg-tertiary); color:var(--text-secondary); font-family:var(--font-mono);`,
      })
    )
  }
}
