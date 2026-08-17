import { Page, raw } from '../../ui/page.js'

const AUTH_STYLES = `
  * { margin:0; padding:0; box-sizing:border-box; }
  :root {
    --bg:#f8f9fb; --card:#ffffff; --text:#1a1d23; --muted:#8b919c;
    --border:#e4e7ec; --accent:#4361ee; --danger:#e5484d; --radius:10px;
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
  button:hover { opacity:0.92; }
  .error { color:var(--danger); font-size:13px; margin-bottom:14px; display:none; }`

const SHELF_LOGO = `<svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="4" width="20" height="7" rx="2"/><rect x="2" y="13" width="20" height="7" rx="2"/><line x1="6" y1="7.5" x2="6.01" y2="7.5"/><line x1="6" y1="16.5" x2="6.01" y2="16.5"/></svg>`

abstract class AuthPage extends Page {
  protected abstract readonly title: string
  protected abstract readonly subtitle: string
  protected abstract form(): string
  protected abstract submitHandler(): string

  render(): string {
    return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>${this.title} - Shelf</title>
  <style>${AUTH_STYLES}</style>
</head>
<body>
  <div class="card">
    <div class="logo">${SHELF_LOGO} Shelf</div>
    <div class="sub">${this.subtitle}</div>
    ${this.form()}
    ${this.script(this.submitHandler()).toString()}
  </div>
</body>
</html>`
  }

  protected showError(): string {
    return `
      const el = document.getElementById('error');
      el.textContent = json.error?.message || '${this.title} failed';
      el.style.display = 'block';`
  }
}

export class LoginPage extends AuthPage {
  protected readonly title = 'Sign in'
  protected readonly subtitle = 'Sign in to your server'

  protected form(): string {
    return `
      <form id="form">
        <label>Username</label>
        <input type="text" name="username" autocomplete="username" required autofocus>
        <label>Password</label>
        <input type="password" name="password" autocomplete="current-password" required>
        <div class="error" id="error"></div>
        <button type="submit">Sign in</button>
      </form>`
  }

  protected submitHandler(): string {
    return `
      document.getElementById('form').addEventListener('submit', async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        const res = await fetch('/api/auth/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username: fd.get('username'), password: fd.get('password') }),
        });
        const json = await res.json();
        if (json.ok) location.href = '/admin';
        else {${this.showError()}}
      });`
  }
}

export class SetupPage extends AuthPage {
  protected readonly title = 'Setup'
  protected readonly subtitle = 'Welcome! Create your admin account to get started.'

  protected form(): string {
    return `
      <form id="form">
        <label>Username</label>
        <input type="text" name="username" autocomplete="username" required autofocus>
        <label>Password (min 8 chars)</label>
        <input type="password" name="password" autocomplete="new-password" minlength="8" required>
        <label>Confirm password</label>
        <input type="password" name="confirm" autocomplete="new-password" required>
        <div class="error" id="error"></div>
        <button type="submit">Create account</button>
      </form>`
  }

  protected submitHandler(): string {
    return `
      document.getElementById('form').addEventListener('submit', async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        const el = document.getElementById('error');
        if (fd.get('password') !== fd.get('confirm')) {
          el.textContent = 'Passwords do not match';
          el.style.display = 'block';
          return;
        }
        const res = await fetch('/api/auth/setup', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username: fd.get('username'), password: fd.get('password') }),
        });
        const json = await res.json();
        if (json.ok) location.href = '/admin';
        else {${this.showError()}}
      });`
  }
}
