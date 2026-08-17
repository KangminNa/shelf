import { Page, el, join, raw, submits, matches, type Attrs, type Child } from '../../ui/page.js'
import { RUNTIME_SCRIPT } from '../../ui/runtime.js'

const AUTH_STYLES = `
  * { margin:0; padding:0; box-sizing:border-box; }
  :root {
    --bg:#f8f9fb; --card:#ffffff; --text:#1a1d23; --muted:#8b919c;
    --border:#e4e7ec; --accent:#4361ee; --danger:#e5484d; --success:#30a46c;
    --radius:10px; --shadow:0 8px 32px rgba(0,0,0,0.12);
  }
  @media (prefers-color-scheme: dark) {
    :root { --bg:#0f1115; --card:#171a21; --text:#e6e8ec; --muted:#7d8590; --border:#262b35; }
  }
  body {
    font-family:-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    background:var(--bg); color:var(--text);
    min-height:100vh; display:grid; place-items:center; padding:24px;
  }
  .card {
    background:var(--card); border:1px solid var(--border); border-radius:16px;
    padding:36px; width:100%; max-width:380px; box-shadow:0 8px 32px rgba(0,0,0,0.06);
  }
  .logo { display:flex; align-items:center; gap:10px; justify-content:center; margin-bottom:6px; font-size:22px; font-weight:700; }
  .sub { text-align:center; color:var(--muted); font-size:13px; margin-bottom:28px; }
  label { display:block; font-size:13px; font-weight:500; margin-bottom:6px; }
  input {
    width:100%; padding:10px 14px; border:1px solid var(--border); border-radius:var(--radius);
    background:var(--bg); color:var(--text); font-size:14px; margin-bottom:16px; outline:none;
  }
  input:focus { border-color:var(--accent); }
  button {
    width:100%; padding:11px; border:0; border-radius:var(--radius);
    background:var(--accent); color:#fff; font-size:14px; font-weight:600; cursor:pointer;
  }
  button:hover { opacity:0.92; }`

const SHELF_LOGO = raw(
  `<svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="4" width="20" height="7" rx="2"/><rect x="2" y="13" width="20" height="7" rx="2"/><line x1="6" y1="7.5" x2="6.01" y2="7.5"/><line x1="6" y1="16.5" x2="6.01" y2="16.5"/></svg>`
)

abstract class AuthPage extends Page {
  protected abstract readonly title: string
  protected abstract readonly subtitle: string
  protected abstract readonly endpoint: string
  protected abstract readonly submitLabel: string
  protected abstract fields(): Child

  render(): string {
    return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>${this.escape(this.title)} - Shelf</title>
  <style>${AUTH_STYLES}</style>
  <script>${RUNTIME_SCRIPT}</script>
</head>
<body>
  ${this.cardBody()}
</body>
</html>`
  }

  private cardBody(): string {
    return el.div(
      { class: 'card' },
      el.div({ class: 'logo' }, SHELF_LOGO, ' Shelf'),
      el.div({ class: 'sub' }, this.subtitle),
      el.form(
        { ...submits('POST', this.endpoint, { then: 'redirect:/admin' }) },
        this.fields(),
        el.button({ type: 'submit' }, this.submitLabel)
      )
    ).toString()
  }

  protected credential(label: string, name: string, type: string, attrs: Attrs = {}): Child {
    return join([el.label({}, label), el.input({ type, name, required: true, ...attrs })])
  }
}

export class LoginPage extends AuthPage {
  protected readonly title = 'Sign in'
  protected readonly subtitle = 'Sign in to your server'
  protected readonly endpoint = '/api/auth/login'
  protected readonly submitLabel = 'Sign in'

  protected fields(): Child {
    return [
      this.credential('Username', 'username', 'text', { autocomplete: 'username', autofocus: true }),
      this.credential('Password', 'password', 'password', { autocomplete: 'current-password' }),
    ]
  }
}

export class SetupPage extends AuthPage {
  protected readonly title = 'Setup'
  protected readonly subtitle = 'Welcome! Create your admin account to get started.'
  protected readonly endpoint = '/api/auth/setup'
  protected readonly submitLabel = 'Create account'

  protected fields(): Child {
    return [
      this.credential('Username', 'username', 'text', { autocomplete: 'username', autofocus: true }),
      this.credential('Password (min 8 chars)', 'password', 'password', { autocomplete: 'new-password', minlength: 8 }),
      this.credential('Confirm password', 'confirm', 'password', {
        autocomplete: 'new-password',
        'data-omit-empty': '',
        ...matches('[name=password]', 'Passwords do not match'),
      }),
    ]
  }
}
