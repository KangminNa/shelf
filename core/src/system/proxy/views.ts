import { StringTemplatePage, FORM, DIALOG, raw, type Html } from '../../ui/page.js'
import type { ProxyHost, AccessLog } from './repositories.js'

export interface ProxyServerStatus {
  httpPort: number
  httpsPort: number
  httpsActive: boolean
  certificateCount: number
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

const TRASH_ICON = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>`
const POWER_ICON = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>`

abstract class ProxyPage extends StringTemplatePage {
  protected hostFormFields(host?: ProxyHost): string {
    const scheme = (value: string) => (host?.target_scheme === value ? ' selected' : '')
    return `
      ${this.field('Domain', `<input type="text" name="domain" value="${this.escape(host?.domain)}" placeholder="app.example.com" required style="${FORM.input}">`)}
      <div style="display:flex; gap:12px;">
        <div style="flex:1;">
          ${this.field('Scheme', `<select name="target_scheme" style="${FORM.input}"><option value="http"${scheme('http')}>http</option><option value="https"${scheme('https')}>https</option></select>`)}
        </div>
        <div style="flex:2;">
          ${this.field('Target host', `<input type="text" name="target_host" value="${this.escape(host?.target_host ?? '127.0.0.1')}" required style="${FORM.input}">`)}
        </div>
        <div style="flex:1;">
          ${this.field('Port', `<input type="number" name="target_port" value="${host?.target_port ?? ''}" placeholder="3000" required style="${FORM.input}">`)}
        </div>
      </div>
      ${this.field('Description', `<input type="text" name="description" value="${this.escape(host?.description)}" placeholder="My app server" style="${FORM.input}">`)}`
  }

  protected sslOptions(host?: ProxyHost): string {
    return `
      <div style="border-top:1px solid var(--border); padding-top:12px; display:flex; flex-direction:column; gap:8px;">
        <div style="font-size:13px; font-weight:600;">SSL</div>
        ${this.checkbox('ssl_enabled', 'SSL enabled (인증서 연결)', !!host?.ssl_enabled)}
        ${this.checkbox('force_ssl', 'Force SSL (HTTP &rarr; HTTPS 리다이렉트)', !!host?.force_ssl)}
        ${this.checkbox('hsts_enabled', 'HSTS enabled', !!host?.hsts_enabled)}
        ${this.checkbox('hsts_subdomains', 'HSTS &mdash; include subdomains', !!host?.hsts_subdomains, true)}
      </div>`
  }

  protected hostPayloadScript(): string {
    return `
      const hostPayload = (fd) => ({
        domain: fd.get('domain'),
        target_scheme: fd.get('target_scheme') || 'http',
        target_host: fd.get('target_host'),
        target_port: Number(fd.get('target_port')),
        description: fd.get('description') || '',
        ssl_enabled: fd.get('ssl_enabled') === 'on',
        force_ssl: fd.get('force_ssl') === 'on',
        hsts_enabled: fd.get('hsts_enabled') === 'on',
        hsts_subdomains: fd.get('hsts_subdomains') === 'on',
      });`
  }
}

export class HostsPage extends ProxyPage {
  constructor(
    private readonly props: {
      hosts: ProxyHost[]
      certDomains: Set<string>
      status: ProxyServerStatus
    }
  ) {
    super()
  }

  render(): string {
    const { hosts, status } = this.props
    const addButton = `<button onclick="document.getElementById('add-dialog').style.display='flex'" class="shelf-btn shelf-btn-primary shelf-btn-sm">+ Add host</button>`

    if (!hosts.length) {
      return `
        ${this.emptyState('No proxy hosts', 'Add your first proxy host to start routing traffic.', `<button onclick="document.getElementById('add-dialog').style.display='flex'" class="shelf-btn shelf-btn-primary">+ Add proxy host</button>`)}
        ${this.addDialog()}
        ${this.editDialog()}
        ${this.script(`const HOSTS = [];` + this.scripts())}`
    }

    const reloadButton = `<button onclick="reloadProxy()" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Reload</button>`
    return `
      ${this.sectionHeader(`Proxy hosts (${hosts.length})`, reloadButton + addButton)}
      ${this.tableCard(['Domain', 'Target', 'Status', ''], hosts.map((h) => this.row(h)).join(''))}
      <div style="margin-top:16px; font-size:12px; color:var(--text-muted);">
        HTTP proxy on :${status.httpPort} ${status.httpsActive ? `| HTTPS on :${status.httpsPort} (${status.certificateCount} certs)` : '| HTTPS not active'}
      </div>
      ${this.addDialog()}
      ${this.editDialog()}
      ${this.script(`const HOSTS = ${JSON.stringify(hosts)};` + this.scripts())}`
  }

  private row(h: ProxyHost): string {
    const hasCert = this.props.certDomains.has(h.domain)
    const statusBadge = h.enabled
      ? '<span class="shelf-badge shelf-badge-success">online</span>'
      : '<span class="shelf-badge shelf-badge-warning">disabled</span>'
    const sslBadge = hasCert
      ? `<span class="shelf-badge shelf-badge-info">SSL${h.force_ssl ? ' · forced' : ''}</span>`
      : h.ssl_enabled
        ? '<span class="shelf-badge shelf-badge-warning">SSL pending</span>'
        : ''
    const hstsBadge = h.hsts_enabled ? '<span class="shelf-badge shelf-badge-success">HSTS</span>' : ''

    return `
      <tr>
        <td>
          <div style="font-weight:500;">${this.escape(h.domain)}</div>
          ${h.description ? `<div style="font-size:12px; color:var(--text-muted);">${this.escape(h.description)}</div>` : ''}
        </td>
        <td>
          <code style="font-size:12px; background:var(--bg-tertiary); padding:2px 8px; border-radius:4px; font-family:var(--font-mono);">${h.target_scheme}://${this.escape(h.target_host)}:${h.target_port}</code>
        </td>
        <td>${statusBadge} ${sslBadge} ${hstsBadge}</td>
        <td>
          <div style="display:flex; gap:4px;">
            ${!hasCert ? `<a href="/admin/proxy/ssl" class="shelf-btn shelf-btn-ghost shelf-btn-sm" title="Request SSL" style="font-size:11px;">SSL</a>` : ''}
            <button onclick="openHostEdit(${h.id})" class="shelf-btn shelf-btn-ghost shelf-btn-sm" title="Edit">Edit</button>
            <button onclick="toggleHost(${h.id})" class="shelf-btn shelf-btn-ghost shelf-btn-sm shelf-btn-icon" title="${h.enabled ? 'Disable' : 'Enable'}">${POWER_ICON}</button>
            <button onclick="deleteHost(${h.id}, '${this.escape(h.domain)}')" class="shelf-btn shelf-btn-ghost shelf-btn-sm shelf-btn-icon" title="Delete" style="color:var(--danger);">${TRASH_ICON}</button>
          </div>
        </td>
      </tr>`
  }

  private addDialog(): Html {
    const form = `
      <form id="add-form" style="${DIALOG.body}">
        ${this.hostFormFields()}
        <div style="display:flex; gap:16px;">
          ${this.checkbox('ssl_enabled', 'SSL enabled')}
          ${this.checkbox('force_ssl', 'Force HTTPS')}
        </div>
        ${this.dialogActions('add-dialog', 'Add host')}
      </form>`
    return this.dialog('add-dialog', 'Add proxy host', form, 480)
  }

  private editDialog(): Html {
    const form = `
      <form id="host-edit-form" style="${DIALOG.body}">
        <input type="hidden" name="id">
        ${this.hostFormFields()}
        ${this.sslOptions()}
        ${this.dialogActions('host-edit-dialog', 'Save')}
      </form>`
    return this.dialog('host-edit-dialog', 'Edit proxy host', form, 480)
  }

  private scripts(): string {
    return `
      ${this.hostPayloadScript()}
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
      document.getElementById('add-form')?.addEventListener('submit', async (e) => {
        e.preventDefault();
        const res = await fetch('/api/proxy/hosts', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(hostPayload(new FormData(e.target))),
        });
        const json = await res.json();
        if (json.ok) location.reload();
        else alert(json.error?.message || 'Failed');
      });
      document.getElementById('host-edit-form')?.addEventListener('submit', async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        const res = await fetch('/api/proxy/hosts/' + fd.get('id'), {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(hostPayload(fd)),
        });
        const json = await res.json();
        if (json.ok) location.reload();
        else alert(json.error?.message || 'Failed');
      });`
  }
}

export class SslPage extends ProxyPage {
  constructor(
    private readonly props: {
      certs: CertView[]
      domainsWithoutCert: string[]
      defaultEmail: string
    }
  ) {
    super()
  }

  render(): string {
    const { certs } = this.props
    const actions = `
      <button onclick="checkRenewalsBtn()" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Check renewals</button>
      <button onclick="document.getElementById('issue-dialog').style.display='flex'" class="shelf-btn shelf-btn-primary shelf-btn-sm">+ New certificate</button>`

    return `
      ${this.sectionHeader(`SSL certificates (${certs.length})`, actions)}
      ${certs.length
        ? this.tableCard(['Domains', 'Provider', 'Expires', 'Status', 'Renewal', ''], certs.map((cert) => this.row(cert)).join(''))
        : '<div style="text-align:center; padding:32px; color:var(--text-muted);">No certificates yet.</div>'}
      ${this.uncoveredDomains()}
      ${this.issueDialog()}
      ${this.statusToast()}
      ${this.script(this.scripts())}`
  }

  private row(cert: CertView): string {
    const domainList = (cert.domains || cert.domain).split('\n').filter(Boolean)
    const daysLeft = cert.expires_at ? Math.floor((cert.expires_at - Math.floor(Date.now() / 1000)) / 86400) : -1
    const expiryBadge = daysLeft < 0
      ? '<span class="shelf-badge shelf-badge-danger">expired</span>'
      : daysLeft < 30
        ? `<span class="shelf-badge shelf-badge-warning">${daysLeft}d left</span>`
        : `<span class="shelf-badge shelf-badge-success">${daysLeft}d left</span>`
    const renewButton = cert.provider === 'letsencrypt'
      ? `<button onclick="renewCert(${cert.id}, '${this.escape(cert.domain)}')" class="shelf-btn shelf-btn-ghost shelf-btn-sm">Renew</button>`
      : ''
    const challenge = cert.provider === 'letsencrypt'
      ? `<div style="font-size:11px; color:var(--text-muted);">${cert.dns_provider ? 'DNS-01 · cloudflare' : 'HTTP-01'}</div>`
      : ''

    return `
      <tr>
        <td>
          <div style="font-weight:500;">${this.escape(cert.domain)}</div>
          ${domainList.length > 1 ? `<div style="font-size:11px; color:var(--text-muted);">${this.escape(domainList.slice(1).join(', '))}</div>` : ''}
        </td>
        <td>${PROVIDER_BADGES[cert.provider] || cert.provider}${challenge}</td>
        <td>${cert.expires_at ? this.formatDate(cert.expires_at) : 'unknown'}</td>
        <td>${expiryBadge}</td>
        <td>${cert.auto_renew ? '<span class="shelf-badge shelf-badge-success">auto</span>' : '<span style="color:var(--text-muted);">manual</span>'}</td>
        <td>
          <div style="display:flex; gap:4px;">
            ${renewButton}
            <button onclick="deleteCert(${cert.id}, '${this.escape(cert.domain)}')" class="shelf-btn shelf-btn-ghost shelf-btn-sm shelf-btn-icon" style="color:var(--danger);">${TRASH_ICON}</button>
          </div>
        </td>
      </tr>`
  }

  private uncoveredDomains(): Html {
    const { domainsWithoutCert } = this.props
    if (!domainsWithoutCert.length) return raw('')
    const chips = domainsWithoutCert.map((domain) => `
      <div style="display:flex; align-items:center; gap:8px; padding:6px 12px; background:var(--bg-tertiary); border-radius:var(--radius); font-size:13px;">
        ${this.escape(domain)}
        <button onclick="prefillIssue('${this.escape(domain)}')" class="shelf-btn shelf-btn-primary shelf-btn-sm" style="padding:2px 8px; font-size:11px;">Issue SSL</button>
      </div>`).join('')
    return raw(`
      <div class="shelf-card shelf-mt-lg">
        <div style="font-size:13px; font-weight:600; margin-bottom:8px;">Domains without SSL</div>
        <div style="display:flex; flex-wrap:wrap; gap:8px;">${chips}</div>
      </div>`)
  }

  private issueDialog(): Html {
    const tabs = `
      <div style="display:flex; gap:8px; margin-bottom:16px;">
        <button onclick="showTab('le')" id="tab-le" class="shelf-btn shelf-btn-primary shelf-btn-sm">Let's Encrypt</button>
        <button onclick="showTab('self')" id="tab-self" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Self-signed</button>
        <button onclick="showTab('manual')" id="tab-manual" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Manual upload</button>
      </div>`
    return this.dialog('issue-dialog', 'New SSL certificate', tabs + this.letsEncryptForm() + this.selfSignedForm() + this.manualForm(), 480)
  }

  private letsEncryptForm(): string {
    return `
      <form id="le-form" style="${DIALOG.body}">
        ${this.field('Domains (줄 단위 — SAN 인증서, *.example.com 가능)', `<textarea name="domains" rows="3" placeholder="example.com&#10;www.example.com&#10;*.example.com" required style="${FORM.input} ${FORM.mono}"></textarea>`)}
        ${this.field('Email', `<input type="email" name="email" placeholder="admin@example.com" value="${this.escape(this.props.defaultEmail)}" style="${FORM.input}">`)}
        ${this.field('Challenge', `
          <select name="challenge" onchange="document.getElementById('dns-token-field').style.display = this.value === 'dns' ? 'block' : 'none'" style="${FORM.input}">
            <option value="http">HTTP-01 — 포트 80으로 검증 (기본)</option>
            <option value="dns">DNS-01 — Cloudflare (와일드카드 필수)</option>
          </select>`)}
        <div id="dns-token-field" style="display:none;">
          ${this.field('Cloudflare API Token (Zone.DNS Edit 권한)', `<input type="password" name="dns_token" autocomplete="off" placeholder="cf_..." style="${FORM.input}">`, '갱신 때 재사용하도록 저장됩니다 (API 응답에는 노출 안 됨)')}
        </div>
        <div style="font-size:12px; color:var(--text-muted); padding:8px; background:var(--bg-tertiary); border-radius:var(--radius);">
          HTTP-01: 도메인이 이 서버 IP를 가리키고 포트 80이 열려 있어야 합니다.<br>
          DNS-01: 서버 노출 없이 발급 가능, *.도메인 와일드카드 지원.
        </div>
        ${this.dialogActions('issue-dialog', 'Issue certificate', 'le-submit')}
      </form>`
  }

  private selfSignedForm(): string {
    return `
      <form id="self-form" style="display:none; flex-direction:column; gap:14px;">
        ${this.field('Domain', `<input type="text" name="domain" placeholder="shelf.local" required style="${FORM.input}">`)}
        <div style="font-size:12px; color:var(--text-muted); padding:8px; background:var(--bg-tertiary); border-radius:var(--radius);">
          로컬/LAN 전용. 도메인 + *.도메인을 커버하는 10년짜리 인증서를 만듭니다.
          브라우저 경고가 뜨므로 공개 서비스에는 Let's Encrypt를 쓰세요.
        </div>
        ${this.dialogActions('issue-dialog', 'Generate')}
      </form>`
  }

  private manualForm(): string {
    return `
      <form id="manual-form" style="display:none; flex-direction:column; gap:14px;">
        ${this.field('Domain', `<input type="text" name="domain" placeholder="example.com" required style="${FORM.input}">`)}
        ${this.field('Certificate (PEM)', `<textarea name="cert" rows="4" placeholder="-----BEGIN CERTIFICATE-----" required style="${FORM.input} ${FORM.mono}"></textarea>`)}
        ${this.field('Private key (PEM)', `<textarea name="key" rows="4" placeholder="-----BEGIN PRIVATE KEY-----" required style="${FORM.input} ${FORM.mono}"></textarea>`)}
        ${this.dialogActions('issue-dialog', 'Upload certificate')}
      </form>`
  }

  private statusToast(): Html {
    return raw(`<div id="ssl-status" style="display:none; position:fixed; bottom:24px; right:24px; padding:12px 20px; background:var(--bg); border:1px solid var(--border); border-radius:var(--radius); box-shadow:var(--shadow); font-size:13px; z-index:200;"></div>`)
  }

  private scripts(): string {
    return `
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
        if (json.ok) location.reload();
        else showStatus('Failed: ' + (json.error?.message || ''), true);
      });`
  }
}

export class AccessLogsPage extends ProxyPage {
  constructor(private readonly props: { logs: AccessLog[]; selectedDomain: string }) {
    super()
  }

  render(): string {
    const { logs, selectedDomain } = this.props
    const domains = [...new Set(logs.map((log) => log.domain))]
    const filter = `
      <select onchange="location.href='/admin/proxy/logs' + (this.value ? '?domain=' + this.value : '')"
        style="padding:4px 8px; border:1px solid var(--border); border-radius:var(--radius-sm); background:var(--bg); color:var(--text); font-size:13px;">
        <option value="">All domains</option>
        ${domains.map((d) => `<option value="${this.escape(d)}" ${d === selectedDomain ? 'selected' : ''}>${this.escape(d)}</option>`).join('')}
      </select>`

    return `
      ${this.sectionHeader('Access logs', filter)}
      ${logs.length
        ? this.tableCard(['Time', 'Domain', 'Method', 'Path', 'Status', 'Duration'], logs.map((log) => this.row(log)).join(''))
        : '<div style="text-align:center; padding:48px; color:var(--text-muted);">No access logs yet.</div>'}`
  }

  private row(log: AccessLog): string {
    const color = log.status < 300 ? 'success' : log.status < 400 ? 'info' : log.status < 500 ? 'warning' : 'danger'
    return `
      <tr>
        <td style="font-size:12px; color:var(--text-muted);">${this.formatDateTime(log.created_at)}</td>
        <td>${this.escape(log.domain)}</td>
        <td><code style="font-size:12px;">${this.escape(log.method)}</code></td>
        <td style="max-width:200px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">${this.escape(log.path)}</td>
        <td><span class="shelf-badge shelf-badge-${color}">${log.status}</span></td>
        <td style="font-size:12px; color:var(--text-muted);">${log.duration_ms}ms</td>
      </tr>`
  }
}
