/** 로그인/최초 설정 페이지 — 셸 밖의 독립 화면 */

function authLayout(title: string, body: string): string {
  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>${title} - Shelf</title>
  <style>
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
    .error { color:var(--danger); font-size:13px; margin-bottom:14px; display:none; }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo">
      <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="4" width="20" height="7" rx="2"/><rect x="2" y="13" width="20" height="7" rx="2"/><line x1="6" y1="7.5" x2="6.01" y2="7.5"/><line x1="6" y1="16.5" x2="6.01" y2="16.5"/></svg>
      Shelf
    </div>
    ${body}
  </div>
</body>
</html>`
}

export function loginPage(): string {
  return authLayout('Sign in', `
    <div class="sub">Sign in to your server</div>
    <form id="form">
      <label>Username</label>
      <input type="text" name="username" autocomplete="username" required autofocus>
      <label>Password</label>
      <input type="password" name="password" autocomplete="current-password" required>
      <div class="error" id="error"></div>
      <button type="submit">Sign in</button>
    </form>
    <script>
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
        else {
          const el = document.getElementById('error');
          el.textContent = json.error?.message || 'Sign in failed';
          el.style.display = 'block';
        }
      });
    </script>
  `)
}

export function setupPage(): string {
  return authLayout('Setup', `
    <div class="sub">Welcome! Create your admin account to get started.</div>
    <form id="form">
      <label>Username</label>
      <input type="text" name="username" autocomplete="username" required autofocus>
      <label>Password (min 8 chars)</label>
      <input type="password" name="password" autocomplete="new-password" minlength="8" required>
      <label>Confirm password</label>
      <input type="password" name="confirm" autocomplete="new-password" required>
      <div class="error" id="error"></div>
      <button type="submit">Create account</button>
    </form>
    <script>
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
        else {
          el.textContent = json.error?.message || 'Setup failed';
          el.style.display = 'block';
        }
      });
    </script>
  `)
}
