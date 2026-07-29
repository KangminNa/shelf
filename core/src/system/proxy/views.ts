import type { ProxyHost, SslCert, AccessLog } from './repositories.js'

export interface ProxyServerStatus {
  httpPort: number
  httpsPort: number
  httpsActive: boolean
  certificateCount: number
}

export function hostsPage(hosts: ProxyHost[], certDomains: Set<string>, status: ProxyServerStatus): string {
  if (!hosts.length) {
    return `
      <div style="text-align:center; padding:48px 24px; color:var(--text-secondary);">
        <div style="color:var(--text-muted); margin-bottom:12px;">
          <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>
        </div>
        <h2 style="font-size:18px; font-weight:600; color:var(--text); margin-bottom:8px;">No proxy hosts</h2>
        <p style="font-size:13px; margin-bottom:16px;">Add your first proxy host to start routing traffic.</p>
        <button onclick="document.getElementById('add-dialog').style.display='flex'" class="shelf-btn shelf-btn-primary">+ Add proxy host</button>
      </div>
      ${addHostDialog()}
      ${hostScripts()}
    `
  }

  const rows = hosts.map((h) => {
    const statusBadge = h.enabled
      ? '<span class="shelf-badge shelf-badge-success">online</span>'
      : '<span class="shelf-badge shelf-badge-warning">disabled</span>'
    const hasCert = certDomains.has(h.domain)
    const sslBadge = hasCert
      ? `<span class="shelf-badge shelf-badge-info">SSL${h.force_ssl ? ' · forced' : ''}</span>`
      : h.ssl_enabled
        ? '<span class="shelf-badge shelf-badge-warning">SSL pending</span>'
        : ''
    const hstsBadge = h.hsts_enabled ? '<span class="shelf-badge shelf-badge-success">HSTS</span>' : ''
    return `
      <tr>
        <td>
          <div style="font-weight:500;">${h.domain}</div>
          ${h.description ? `<div style="font-size:12px; color:var(--text-muted);">${h.description}</div>` : ''}
        </td>
        <td>
          <code style="font-size:12px; background:var(--bg-tertiary); padding:2px 8px; border-radius:4px; font-family:var(--font-mono);">${h.target_scheme}://${h.target_host}:${h.target_port}</code>
        </td>
        <td>${statusBadge} ${sslBadge} ${hstsBadge}</td>
        <td>
          <div style="display:flex; gap:4px;">
            ${!hasCert ? `<button onclick="requestSsl('${h.domain}')" class="shelf-btn shelf-btn-ghost shelf-btn-sm" title="Request SSL" style="font-size:11px;">SSL</button>` : ''}
            <button onclick="openHostEdit(${h.id})" class="shelf-btn shelf-btn-ghost shelf-btn-sm" title="Edit">Edit</button>
            <button onclick="toggleHost(${h.id})" class="shelf-btn shelf-btn-ghost shelf-btn-sm shelf-btn-icon" title="${h.enabled ? 'Disable' : 'Enable'}">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>
            </button>
            <button onclick="deleteHost(${h.id}, '${h.domain}')" class="shelf-btn shelf-btn-ghost shelf-btn-sm shelf-btn-icon" title="Delete" style="color:var(--danger);">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
            </button>
          </div>
        </td>
      </tr>`
  }).join('')

  return `
    <div class="shelf-section-header">
      <h2 class="shelf-section-title">Proxy hosts (${hosts.length})</h2>
      <div style="display:flex; gap:8px;">
        <button onclick="reloadProxy()" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Reload</button>
        <button onclick="document.getElementById('add-dialog').style.display='flex'" class="shelf-btn shelf-btn-primary shelf-btn-sm">+ Add host</button>
      </div>
    </div>

    <div class="shelf-card" style="padding:0; overflow:hidden;">
      <table class="shelf-table">
        <thead><tr><th>Domain</th><th>Target</th><th>Status</th><th></th></tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>

    <div style="margin-top:16px; font-size:12px; color:var(--text-muted);">
      HTTP proxy on :${status.httpPort} ${status.httpsActive ? `| HTTPS on :${status.httpsPort} (${status.certificateCount} certs)` : '| HTTPS not active'}
    </div>

    ${addHostDialog()}
    ${hostEditDialog()}
    <script>const HOSTS = ${JSON.stringify(hosts)};</script>
    ${hostScripts()}
  `
}

function hostEditDialog(): string {
  const input = `width:100%; padding:8px 12px; border:1px solid var(--border); border-radius:var(--radius); background:var(--bg); color:var(--text); font-size:14px;`
  const label = `display:block; font-size:13px; font-weight:500; margin-bottom:4px;`
  return `
    <div id="host-edit-dialog" style="display:none; position:fixed; inset:0; background:rgba(0,0,0,0.4); z-index:100; align-items:center; justify-content:center;">
      <div style="background:var(--bg); border:1px solid var(--border); border-radius:var(--radius-lg); padding:24px; width:100%; max-width:480px; box-shadow:var(--shadow);">
        <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:20px;">
          <h3 style="font-size:16px; font-weight:600;">Edit proxy host</h3>
          <button onclick="document.getElementById('host-edit-dialog').style.display='none'" class="shelf-btn shelf-btn-ghost shelf-btn-icon">&times;</button>
        </div>
        <form id="host-edit-form" style="display:flex; flex-direction:column; gap:14px;">
          <input type="hidden" name="id">
          <div>
            <label style="${label}">Domain</label>
            <input type="text" name="domain" required style="${input}">
          </div>
          <div style="display:flex; gap:12px;">
            <div style="flex:1;">
              <label style="${label}">Scheme</label>
              <select name="target_scheme" style="${input}"><option value="http">http</option><option value="https">https</option></select>
            </div>
            <div style="flex:2;">
              <label style="${label}">Target host</label>
              <input type="text" name="target_host" required style="${input}">
            </div>
            <div style="flex:1;">
              <label style="${label}">Port</label>
              <input type="number" name="target_port" required style="${input}">
            </div>
          </div>
          <div>
            <label style="${label}">Description</label>
            <input type="text" name="description" style="${input}">
          </div>
          <div style="border-top:1px solid var(--border); padding-top:12px; display:flex; flex-direction:column; gap:8px;">
            <div style="font-size:13px; font-weight:600;">SSL</div>
            <label style="display:flex; align-items:center; gap:6px; font-size:13px; cursor:pointer;">
              <input type="checkbox" name="ssl_enabled"> SSL enabled (인증서 연결)
            </label>
            <label style="display:flex; align-items:center; gap:6px; font-size:13px; cursor:pointer;">
              <input type="checkbox" name="force_ssl"> Force SSL (HTTP &rarr; HTTPS 리다이렉트)
            </label>
            <label style="display:flex; align-items:center; gap:6px; font-size:13px; cursor:pointer;">
              <input type="checkbox" name="hsts_enabled"> HSTS enabled
            </label>
            <label style="display:flex; align-items:center; gap:6px; font-size:13px; cursor:pointer; padding-left:22px;">
              <input type="checkbox" name="hsts_subdomains"> HSTS &mdash; include subdomains
            </label>
          </div>
          <div style="display:flex; gap:8px; justify-content:flex-end; padding-top:4px;">
            <button type="button" onclick="document.getElementById('host-edit-dialog').style.display='none'" class="shelf-btn shelf-btn-secondary">Cancel</button>
            <button type="submit" class="shelf-btn shelf-btn-primary">Save</button>
          </div>
        </form>
      </div>
    </div>`
}

function addHostDialog(): string {
  return `
    <div id="add-dialog" style="display:none; position:fixed; inset:0; background:rgba(0,0,0,0.4); z-index:100; align-items:center; justify-content:center;">
      <div style="background:var(--bg); border:1px solid var(--border); border-radius:var(--radius-lg); padding:24px; width:100%; max-width:480px; box-shadow:var(--shadow);">
        <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:20px;">
          <h3 style="font-size:16px; font-weight:600;">Add proxy host</h3>
          <button onclick="document.getElementById('add-dialog').style.display='none'" class="shelf-btn shelf-btn-ghost shelf-btn-icon">&times;</button>
        </div>
        <form id="add-form" style="display:flex; flex-direction:column; gap:14px;">
          <div>
            <label style="display:block; font-size:13px; font-weight:500; margin-bottom:4px;">Domain</label>
            <input type="text" name="domain" placeholder="app.example.com" required
              style="width:100%; padding:8px 12px; border:1px solid var(--border); border-radius:var(--radius); background:var(--bg); color:var(--text); font-size:14px;">
          </div>
          <div style="display:flex; gap:12px;">
            <div style="flex:1;">
              <label style="display:block; font-size:13px; font-weight:500; margin-bottom:4px;">Scheme</label>
              <select name="target_scheme" style="width:100%; padding:8px 12px; border:1px solid var(--border); border-radius:var(--radius); background:var(--bg); color:var(--text); font-size:14px;">
                <option value="http">http</option>
                <option value="https">https</option>
              </select>
            </div>
            <div style="flex:2;">
              <label style="display:block; font-size:13px; font-weight:500; margin-bottom:4px;">Target host</label>
              <input type="text" name="target_host" placeholder="127.0.0.1" required value="127.0.0.1"
                style="width:100%; padding:8px 12px; border:1px solid var(--border); border-radius:var(--radius); background:var(--bg); color:var(--text); font-size:14px;">
            </div>
            <div style="flex:1;">
              <label style="display:block; font-size:13px; font-weight:500; margin-bottom:4px;">Port</label>
              <input type="number" name="target_port" placeholder="3000" required
                style="width:100%; padding:8px 12px; border:1px solid var(--border); border-radius:var(--radius); background:var(--bg); color:var(--text); font-size:14px;">
            </div>
          </div>
          <div>
            <label style="display:block; font-size:13px; font-weight:500; margin-bottom:4px;">Description</label>
            <input type="text" name="description" placeholder="My app server"
              style="width:100%; padding:8px 12px; border:1px solid var(--border); border-radius:var(--radius); background:var(--bg); color:var(--text); font-size:14px;">
          </div>
          <div style="display:flex; gap:16px;">
            <label style="display:flex; align-items:center; gap:6px; font-size:13px; cursor:pointer;">
              <input type="checkbox" name="ssl_enabled"> SSL enabled
            </label>
            <label style="display:flex; align-items:center; gap:6px; font-size:13px; cursor:pointer;">
              <input type="checkbox" name="force_ssl"> Force HTTPS
            </label>
          </div>
          <div style="display:flex; gap:8px; justify-content:flex-end; padding-top:8px;">
            <button type="button" onclick="document.getElementById('add-dialog').style.display='none'" class="shelf-btn shelf-btn-secondary">Cancel</button>
            <button type="submit" class="shelf-btn shelf-btn-primary">Add host</button>
          </div>
        </form>
      </div>
    </div>`
}

function hostScripts(): string {
  return `
    <script>
      async function toggleHost(id) {
        await fetch('/api/proxy/hosts/' + id + '/toggle', { method: 'POST' });
        location.reload();
      }
      async function deleteHost(id, domain) {
        if (!confirm('Delete proxy host "' + domain + '"?')) return;
        await fetch('/api/proxy/hosts/' + id, { method: 'DELETE' });
        location.reload();
      }
      async function reloadProxy() {
        await fetch('/api/proxy/reload', { method: 'POST' });
        location.reload();
      }
      async function requestSsl(domain) {
        location.href = '/admin/proxy/ssl';
      }
      function openHostEdit(id) {
        const h = HOSTS.find(x => x.id === id);
        const f = document.getElementById('host-edit-form');
        f.querySelector('[name=id]').value = h.id;
        f.querySelector('[name=domain]').value = h.domain;
        f.querySelector('[name=target_scheme]').value = h.target_scheme;
        f.querySelector('[name=target_host]').value = h.target_host;
        f.querySelector('[name=target_port]').value = h.target_port;
        f.querySelector('[name=description]').value = h.description || '';
        f.querySelector('[name=ssl_enabled]').checked = !!h.ssl_enabled;
        f.querySelector('[name=force_ssl]').checked = !!h.force_ssl;
        f.querySelector('[name=hsts_enabled]').checked = !!h.hsts_enabled;
        f.querySelector('[name=hsts_subdomains]').checked = !!h.hsts_subdomains;
        document.getElementById('host-edit-dialog').style.display = 'flex';
      }
      document.getElementById('host-edit-form')?.addEventListener('submit', async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        const res = await fetch('/api/proxy/hosts/' + fd.get('id'), {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            domain: fd.get('domain'),
            target_scheme: fd.get('target_scheme'),
            target_host: fd.get('target_host'),
            target_port: Number(fd.get('target_port')),
            description: fd.get('description') || '',
            ssl_enabled: fd.get('ssl_enabled') === 'on',
            force_ssl: fd.get('force_ssl') === 'on',
            hsts_enabled: fd.get('hsts_enabled') === 'on',
            hsts_subdomains: fd.get('hsts_subdomains') === 'on',
          }),
        });
        const json = await res.json();
        if (json.ok) location.reload();
        else alert(json.error?.message || 'Failed');
      });
      document.getElementById('add-form')?.addEventListener('submit', async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        const obj = {
          domain: fd.get('domain'),
          target_scheme: fd.get('target_scheme') || 'http',
          target_host: fd.get('target_host'),
          target_port: Number(fd.get('target_port')),
          description: fd.get('description') || '',
          ssl_enabled: fd.get('ssl_enabled') === 'on',
          force_ssl: fd.get('force_ssl') === 'on',
        };
        const res = await fetch('/api/proxy/hosts', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(obj),
        });
        const json = await res.json();
        if (json.ok) location.reload();
        else alert(json.error?.message || 'Failed');
      });
    </script>`
}

export interface CertView {
  id: number
  domain: string
  domains: string
  provider: string
  dns_provider: string
  expires_at: number
  auto_renew: number
  has_dns_token: boolean
}

const PROVIDER_BADGES: Record<string, string> = {
  letsencrypt: '<span class="shelf-badge shelf-badge-info">letsencrypt</span>',
  selfsigned: '<span class="shelf-badge shelf-badge-warning">self-signed</span>',
  manual: '<span class="shelf-badge">manual</span>',
}

export function sslPage(certs: CertView[], domainsWithoutCert: string[], defaultEmail: string): string {
  const now = Math.floor(Date.now() / 1000)

  const certRows = certs.map((cert) => {
    const domainList = (cert.domains || cert.domain).split('\n').filter(Boolean)
    const expires = cert.expires_at ? new Date(cert.expires_at * 1000).toLocaleDateString() : 'unknown'
    const daysLeft = cert.expires_at ? Math.floor((cert.expires_at - now) / 86400) : -1
    const expiryBadge = daysLeft < 0
      ? '<span class="shelf-badge shelf-badge-danger">expired</span>'
      : daysLeft < 30
        ? `<span class="shelf-badge shelf-badge-warning">${daysLeft}d left</span>`
        : `<span class="shelf-badge shelf-badge-success">${daysLeft}d left</span>`
    const renewBtn = cert.provider === 'letsencrypt'
      ? `<button onclick="renewCert(${cert.id}, '${cert.domain}')" class="shelf-btn shelf-btn-ghost shelf-btn-sm">Renew</button>`
      : ''
    const challengeNote = cert.provider === 'letsencrypt'
      ? `<div style="font-size:11px; color:var(--text-muted);">${cert.dns_provider ? 'DNS-01 · cloudflare' : 'HTTP-01'}</div>`
      : ''
    return `<tr>
      <td>
        <div style="font-weight:500;">${cert.domain}</div>
        ${domainList.length > 1 ? `<div style="font-size:11px; color:var(--text-muted);">${domainList.slice(1).join(', ')}</div>` : ''}
      </td>
      <td>${PROVIDER_BADGES[cert.provider] || cert.provider}${challengeNote}</td>
      <td>${expires}</td>
      <td>${expiryBadge}</td>
      <td>${cert.auto_renew ? '<span class="shelf-badge shelf-badge-success">auto</span>' : '<span style="color:var(--text-muted);">manual</span>'}</td>
      <td>
        <div style="display:flex; gap:4px;">
          ${renewBtn}
          <button onclick="deleteCert(${cert.id}, '${cert.domain}')" class="shelf-btn shelf-btn-ghost shelf-btn-sm shelf-btn-icon" style="color:var(--danger);">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
          </button>
        </div>
      </td>
    </tr>`
  }).join('')

  return `
    <div class="shelf-section-header">
      <h2 class="shelf-section-title">SSL certificates (${certs.length})</h2>
      <div style="display:flex; gap:8px;">
        <button onclick="checkRenewalsBtn()" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Check renewals</button>
        <button onclick="document.getElementById('issue-dialog').style.display='flex'" class="shelf-btn shelf-btn-primary shelf-btn-sm">+ New certificate</button>
      </div>
    </div>

    ${certs.length ? `
      <div class="shelf-card" style="padding:0; overflow:hidden;">
        <table class="shelf-table">
          <thead><tr><th>Domains</th><th>Provider</th><th>Expires</th><th>Status</th><th>Renewal</th><th></th></tr></thead>
          <tbody>${certRows}</tbody>
        </table>
      </div>
    ` : `
      <div style="text-align:center; padding:32px; color:var(--text-muted);">
        No certificates yet.
      </div>
    `}

    ${domainsWithoutCert.length ? `
      <div class="shelf-card shelf-mt-lg">
        <div style="font-size:13px; font-weight:600; margin-bottom:8px;">Domains without SSL</div>
        <div style="display:flex; flex-wrap:wrap; gap:8px;">
          ${domainsWithoutCert.map((d) => `
            <div style="display:flex; align-items:center; gap:8px; padding:6px 12px; background:var(--bg-tertiary); border-radius:var(--radius); font-size:13px;">
              ${d}
              <button onclick="prefillIssue('${d}')" class="shelf-btn shelf-btn-primary shelf-btn-sm" style="padding:2px 8px; font-size:11px;">Issue SSL</button>
            </div>
          `).join('')}
        </div>
      </div>
    ` : ''}

    ${issueDialog(defaultEmail)}
    ${sslScripts()}
  `
}

const F_INPUT = `width:100%; padding:8px 12px; border:1px solid var(--border); border-radius:var(--radius); background:var(--bg); color:var(--text); font-size:14px;`
const F_LABEL = `display:block; font-size:13px; font-weight:500; margin-bottom:4px;`

function issueDialog(defaultEmail: string): string {
  return `
    <div id="issue-dialog" style="display:none; position:fixed; inset:0; background:rgba(0,0,0,0.4); z-index:100; align-items:center; justify-content:center;">
      <div style="background:var(--bg); border:1px solid var(--border); border-radius:var(--radius-lg); padding:24px; width:100%; max-width:480px; max-height:85vh; overflow:auto; box-shadow:var(--shadow);">
        <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:20px;">
          <h3 style="font-size:16px; font-weight:600;">New SSL certificate</h3>
          <button onclick="document.getElementById('issue-dialog').style.display='none'" class="shelf-btn shelf-btn-ghost shelf-btn-icon">&times;</button>
        </div>

        <div style="display:flex; gap:8px; margin-bottom:16px;">
          <button onclick="showTab('le')" id="tab-le" class="shelf-btn shelf-btn-primary shelf-btn-sm">Let's Encrypt</button>
          <button onclick="showTab('self')" id="tab-self" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Self-signed</button>
          <button onclick="showTab('manual')" id="tab-manual" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Manual upload</button>
        </div>

        <form id="le-form" style="display:flex; flex-direction:column; gap:14px;">
          <div>
            <label style="${F_LABEL}">Domains (줄 단위 — SAN 인증서, *.example.com 가능)</label>
            <textarea name="domains" rows="3" placeholder="example.com&#10;www.example.com&#10;*.example.com" required
              style="${F_INPUT} font-family:var(--font-mono); font-size:12px; resize:vertical;"></textarea>
          </div>
          <div>
            <label style="${F_LABEL}">Email</label>
            <input type="email" name="email" placeholder="admin@example.com" value="${defaultEmail}" style="${F_INPUT}">
          </div>
          <div>
            <label style="${F_LABEL}">Challenge</label>
            <select name="challenge" onchange="document.getElementById('dns-token-field').style.display = this.value === 'dns' ? 'block' : 'none'" style="${F_INPUT}">
              <option value="http">HTTP-01 — 포트 80으로 검증 (기본)</option>
              <option value="dns">DNS-01 — Cloudflare (와일드카드 필수)</option>
            </select>
          </div>
          <div id="dns-token-field" style="display:none;">
            <label style="${F_LABEL}">Cloudflare API Token (Zone.DNS Edit 권한)</label>
            <input type="password" name="dns_token" autocomplete="off" placeholder="cf_..." style="${F_INPUT}">
            <div style="font-size:12px; color:var(--text-muted); margin-top:4px;">갱신 때 재사용하도록 저장됩니다 (API 응답에는 노출 안 됨)</div>
          </div>
          <div style="font-size:12px; color:var(--text-muted); padding:8px; background:var(--bg-tertiary); border-radius:var(--radius);">
            HTTP-01: 도메인이 이 서버 IP를 가리키고 포트 80이 열려 있어야 합니다.<br>
            DNS-01: 서버 노출 없이 발급 가능, *.도메인 와일드카드 지원.
          </div>
          <div style="display:flex; gap:8px; justify-content:flex-end;">
            <button type="button" onclick="document.getElementById('issue-dialog').style.display='none'" class="shelf-btn shelf-btn-secondary">Cancel</button>
            <button type="submit" id="le-submit" class="shelf-btn shelf-btn-primary">Issue certificate</button>
          </div>
        </form>

        <form id="self-form" style="display:none; flex-direction:column; gap:14px;">
          <div>
            <label style="${F_LABEL}">Domain</label>
            <input type="text" name="domain" placeholder="shelf.local" required style="${F_INPUT}">
          </div>
          <div style="font-size:12px; color:var(--text-muted); padding:8px; background:var(--bg-tertiary); border-radius:var(--radius);">
            로컬/LAN 전용. 도메인 + *.도메인을 커버하는 10년짜리 인증서를 만듭니다.
            브라우저 경고가 뜨므로 공개 서비스에는 Let's Encrypt를 쓰세요.
          </div>
          <div style="display:flex; gap:8px; justify-content:flex-end;">
            <button type="button" onclick="document.getElementById('issue-dialog').style.display='none'" class="shelf-btn shelf-btn-secondary">Cancel</button>
            <button type="submit" class="shelf-btn shelf-btn-primary">Generate</button>
          </div>
        </form>

        <form id="manual-form" style="display:none; flex-direction:column; gap:14px;">
          <div>
            <label style="${F_LABEL}">Domain</label>
            <input type="text" name="domain" placeholder="example.com" required style="${F_INPUT}">
          </div>
          <div>
            <label style="${F_LABEL}">Certificate (PEM)</label>
            <textarea name="cert" rows="4" placeholder="-----BEGIN CERTIFICATE-----" required
              style="${F_INPUT} font-size:12px; font-family:var(--font-mono); resize:vertical;"></textarea>
          </div>
          <div>
            <label style="${F_LABEL}">Private key (PEM)</label>
            <textarea name="key" rows="4" placeholder="-----BEGIN PRIVATE KEY-----" required
              style="${F_INPUT} font-size:12px; font-family:var(--font-mono); resize:vertical;"></textarea>
          </div>
          <div style="display:flex; gap:8px; justify-content:flex-end;">
            <button type="button" onclick="document.getElementById('issue-dialog').style.display='none'" class="shelf-btn shelf-btn-secondary">Cancel</button>
            <button type="submit" class="shelf-btn shelf-btn-primary">Upload certificate</button>
          </div>
        </form>
      </div>
    </div>

    <div id="ssl-status" style="display:none; position:fixed; bottom:24px; right:24px; padding:12px 20px; background:var(--bg); border:1px solid var(--border); border-radius:var(--radius); box-shadow:var(--shadow); font-size:13px; z-index:200;"></div>`
}

function sslScripts(): string {
  return `
    <script>
      function showTab(tab) {
        for (const t of ['le', 'self', 'manual']) {
          document.getElementById(t + '-form').style.display = t === tab ? 'flex' : 'none';
          document.getElementById('tab-' + t).className = 'shelf-btn shelf-btn-sm shelf-btn-' + (t === tab ? 'primary' : 'secondary');
        }
      }

      function prefillIssue(domain) {
        document.querySelector('#le-form [name=domains]').value = domain;
        showTab('le');
        document.getElementById('issue-dialog').style.display = 'flex';
      }

      function showStatus(msg, isError) {
        const el = document.getElementById('ssl-status');
        el.textContent = msg;
        el.style.display = 'block';
        el.style.borderColor = isError ? 'var(--danger)' : 'var(--success)';
        setTimeout(() => el.style.display = 'none', 6000);
      }

      async function renewCert(id, domain) {
        if (!confirm('Renew certificate for ' + domain + '?')) return;
        showStatus('Renewing...', false);
        const res = await fetch('/api/proxy/certs/' + id + '/renew', { method: 'POST' });
        const json = await res.json();
        if (json.ok) { showStatus('Renewed', false); location.reload(); }
        else showStatus('Failed: ' + (json.error?.message || ''), true);
      }

      async function deleteCert(id, domain) {
        if (!confirm('Delete certificate for ' + domain + '?')) return;
        await fetch('/api/proxy/certs/' + id, { method: 'DELETE' });
        location.reload();
      }

      async function checkRenewalsBtn() {
        showStatus('Checking renewals...', false);
        await fetch('/api/proxy/certs/check-renewals', { method: 'POST' });
        showStatus('Renewal check complete', false);
        location.reload();
      }

      document.getElementById('le-form')?.addEventListener('submit', async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        const btn = document.getElementById('le-submit');
        btn.disabled = true; btn.textContent = 'Issuing...';
        showStatus('Issuing certificate...', false);
        const res = await fetch('/api/proxy/certs/issue', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            domains: fd.get('domains'),
            email: fd.get('email') || undefined,
            challenge: fd.get('challenge'),
            dns_token: fd.get('dns_token') || undefined,
          }),
        });
        const json = await res.json();
        btn.disabled = false; btn.textContent = 'Issue certificate';
        if (json.ok) { showStatus('Certificate issued!', false); location.reload(); }
        else showStatus('Failed: ' + (json.error?.message || ''), true);
      });

      document.getElementById('self-form')?.addEventListener('submit', async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        const res = await fetch('/api/proxy/certs/selfsigned', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ domain: fd.get('domain') }),
        });
        const json = await res.json();
        if (json.ok) location.reload();
        else showStatus('Failed: ' + (json.error?.message || ''), true);
      });

      document.getElementById('manual-form')?.addEventListener('submit', async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        const res = await fetch('/api/proxy/certs/upload', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ domain: fd.get('domain'), cert: fd.get('cert'), key: fd.get('key') }),
        });
        const json = await res.json();
        if (json.ok) { location.reload(); }
        else showStatus('Failed: ' + (json.error?.message || ''), true);
      });
    </script>`
}

export function logsPage(logs: AccessLog[], selectedDomain: string): string {
  const rows = logs.map((l) => {
    const time = new Date(l.created_at * 1000).toLocaleString()
    const statusColor = l.status < 300 ? 'success' : l.status < 400 ? 'info' : l.status < 500 ? 'warning' : 'danger'
    return `<tr>
      <td style="font-size:12px; color:var(--text-muted);">${time}</td>
      <td>${l.domain}</td>
      <td><code style="font-size:12px;">${l.method}</code></td>
      <td style="max-width:200px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">${l.path}</td>
      <td><span class="shelf-badge shelf-badge-${statusColor}">${l.status}</span></td>
      <td style="font-size:12px; color:var(--text-muted);">${l.duration_ms}ms</td>
    </tr>`
  }).join('')

  return `
    <div class="shelf-section-header">
      <h2 class="shelf-section-title">Access logs</h2>
      <select onchange="location.href='/admin/proxy/logs' + (this.value ? '?domain=' + this.value : '')"
        style="padding:4px 8px; border:1px solid var(--border); border-radius:var(--radius-sm); background:var(--bg); color:var(--text); font-size:13px;">
        <option value="">All domains</option>
        ${[...new Set(logs.map((l) => l.domain))].map((d) => `<option value="${d}" ${d === selectedDomain ? 'selected' : ''}>${d}</option>`).join('')}
      </select>
    </div>
    ${logs.length ? `
      <div class="shelf-card" style="padding:0; overflow:hidden;">
        <table class="shelf-table">
          <thead><tr><th>Time</th><th>Domain</th><th>Method</th><th>Path</th><th>Status</th><th>Duration</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>
    ` : '<div style="text-align:center; padding:48px; color:var(--text-muted);">No access logs yet.</div>'}
  `
}
