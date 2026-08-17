import { Page, FORM, DIALOG, el, raw, join, openDialog, submits, fills, panel, revealsWhen, revealed, type Html, type Child } from '../../ui/page.js'
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

const PROVIDER_VARIANTS: Record<string, string> = {
  letsencrypt: 'info',
  selfsigned: 'warning',
  manual: 'info',
}

const TRASH = raw(
  `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>`
)
const POWER = raw(
  `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>`
)

function ownerApp(host: ProxyHost): string | null {
  const match = /^app:\s*(.+)$/.exec(host.description || '')
  return match ? match[1].trim() : null
}

abstract class ProxyPage extends Page {
  protected badge(text: Child, variant = 'info'): Html {
    return el.span({ class: `shelf-badge shelf-badge-${variant}` }, text)
  }

  protected hostFields(host?: ProxyHost): Child[] {
    const owner = host ? ownerApp(host) : null
    const locked = !!owner
    return [
      this.field('Domain', this.input({ type: 'text', name: 'domain', value: host?.domain, placeholder: 'app.example.com', required: true })),
      el.div(
        { style: 'display:flex; gap:12px;' },
        el.div({ style: 'flex:1;' }, this.field('Scheme', this.schemeSelect(host, locked))),
        el.div({ style: 'flex:2;' }, this.field('Target host', this.input({ type: 'text', name: 'target_host', value: host?.target_host ?? '127.0.0.1', required: true, disabled: locked }))),
        el.div({ style: 'flex:1;' }, this.field('Port', this.input({ type: 'number', name: 'target_port', value: host?.target_port ?? '', placeholder: '3000', required: true, disabled: locked })))
      ),
      locked ? this.notice(`타깃은 앱 "${owner}" 배포가 관리합니다. 변경은 앱 설정에서 하세요.`) : null,
      this.field('Description', this.input({ type: 'text', name: 'description', value: host?.description, placeholder: 'My app server' })),
    ]
  }

  private schemeSelect(host?: ProxyHost, locked = false): Html {
    return el.select(
      { name: 'target_scheme', style: FORM.input, disabled: locked },
      ...['http', 'https'].map((scheme) => el.option({ value: scheme, selected: host?.target_scheme === scheme }, scheme))
    )
  }

  protected sslOptions(host?: ProxyHost): Html {
    return el.div(
      { style: 'border-top:1px solid var(--border); padding-top:12px; display:flex; flex-direction:column; gap:8px;' },
      el.div({ style: 'font-size:13px; font-weight:600;' }, 'SSL'),
      this.checkbox('ssl_enabled', '인증서 사용', !!host?.ssl_enabled),
      this.checkbox('force_ssl', raw('Force SSL (HTTP &rarr; HTTPS 리다이렉트)'), !!host?.force_ssl),
      this.checkbox('hsts_enabled', 'HSTS', !!host?.hsts_enabled),
      this.checkbox('hsts_subdomains', raw('HSTS &mdash; 서브도메인 포함'), !!host?.hsts_subdomains, true)
    )
  }
}

export class HostsPage extends ProxyPage {
  constructor(private readonly props: { hosts: ProxyHost[]; certDomains: Set<string>; status: ProxyServerStatus }) {
    super()
  }

  render(): string {
    const { hosts, status } = this.props
    const body = hosts.length
      ? [
          this.tableCard(['Domain', 'Target', 'Status', ''], hosts.map((host) => this.row(host))),
          el.div(
            { style: 'margin-top:16px; font-size:12px; color:var(--text-muted);' },
            `HTTP proxy on :${status.httpPort}`,
            status.httpsActive ? ` | HTTPS on :${status.httpsPort} (${status.certificateCount} certs)` : ' | HTTPS not active'
          ),
        ]
      : this.emptyState('No proxy hosts', 'Add your first proxy host to start routing traffic.', this.openButton('add-dialog', '+ Add proxy host', 'primary', ''))

    return join([
      this.sectionHeader(
        `Proxy hosts (${hosts.length})`,
        hosts.length
          ? [this.actionButton('POST', '/api/proxy/reload', 'Reload', { variant: 'secondary' }), this.openButton('add-dialog', '+ Add host')]
          : null
      ),
      body,
      this.addDialog(),
      hosts.map((host) => this.editDialog(host)),
    ]).toString()
  }

  private row(host: ProxyHost): Html {
    const covered = this.props.certDomains.has(host.domain)
    const owner = ownerApp(host)
    return el.tr(
      {},
      el.td(
        {},
        el.div({ style: 'font-weight:500;' }, host.domain),
        owner
          ? el.div({ style: 'font-size:12px; color:var(--text-muted);' }, this.badge('app'), ` ${owner} — 타깃은 배포가 관리`)
          : host.description
            ? el.div({ style: 'font-size:12px; color:var(--text-muted);' }, host.description)
            : null
      ),
      el.td(
        {},
        el.code(
          { style: 'font-size:12px; background:var(--bg-tertiary); padding:2px 8px; border-radius:4px; font-family:var(--font-mono);' },
          `${host.target_scheme}://${host.target_host}:${host.target_port}`
        )
      ),
      el.td(
        {},
        this.badge(host.enabled ? 'online' : 'disabled', host.enabled ? 'success' : 'warning'),
        ' ',
        covered ? this.badge(`SSL${host.force_ssl ? ' · forced' : ''}`, 'info') : host.ssl_enabled ? this.badge('SSL pending', 'warning') : null,
        ' ',
        host.hsts_enabled ? this.badge('HSTS', 'success') : null
      ),
      el.td(
        {},
        el.div(
          { style: 'display:flex; gap:4px;' },
          covered ? null : el.a({ href: '/admin/proxy/ssl', class: 'shelf-btn shelf-btn-ghost shelf-btn-sm', title: 'Request SSL', style: 'font-size:11px;' }, 'SSL'),
          this.openButton(`host-edit-${host.id}`, 'Edit', 'ghost'),
          this.actionButton('POST', `/api/proxy/hosts/${host.id}/toggle`, POWER, {
            size: 'sm shelf-btn-icon',
            title: host.enabled ? 'Disable' : 'Enable',
          }),
          this.actionButton('DELETE', `/api/proxy/hosts/${host.id}`, TRASH, {
            size: 'sm shelf-btn-icon',
            title: 'Delete',
            danger: true,
            confirm: `Delete proxy host "${host.domain}"?`,
          })
        )
      )
    )
  }

  private addDialog(): Html {
    return this.dialog(
      'add-dialog',
      'Add proxy host',
      el.form(
        { style: DIALOG.body, ...submits('POST', '/api/proxy/hosts') },
        this.hostFields(),
        el.div({ style: 'display:flex; gap:16px;' }, this.checkbox('ssl_enabled', '인증서 사용'), this.checkbox('force_ssl', 'Force HTTPS')),
        this.dialogActions('add-dialog', 'Add host')
      )
    )
  }

  private editDialog(host: ProxyHost): Html {
    const id = `host-edit-${host.id}`
    return this.dialog(
      id,
      `Edit ${host.domain}`,
      el.form(
        { style: DIALOG.body, ...submits('PATCH', `/api/proxy/hosts/${host.id}`) },
        this.hostFields(host),
        this.sslOptions(host),
        this.dialogActions(id, 'Save')
      )
    )
  }
}

export class SslPage extends ProxyPage {
  constructor(private readonly props: { certs: CertView[]; domainsWithoutCert: string[]; defaultEmail: string }) {
    super()
  }

  render(): string {
    const { certs } = this.props
    return join([
      this.sectionHeader(`SSL certificates (${certs.length})`, [
        this.actionButton('POST', '/api/proxy/certs/check-renewals', 'Check renewals', { variant: 'secondary', busy: 'Checking...' }),
        this.openButton('issue-dialog', '+ New certificate'),
      ]),
      certs.length
        ? this.tableCard(['Domains', 'Provider', 'Expires', 'Status', 'Renewal', ''], certs.map((cert) => this.row(cert)))
        : el.div({ style: 'text-align:center; padding:32px; color:var(--text-muted);' }, 'No certificates yet.'),
      this.uncovered(),
      this.dialog('issue-dialog', 'New SSL certificate', [this.tabs(), this.letsEncryptForm(), this.selfSignedForm(), this.manualForm()]),
    ]).toString()
  }

  private row(cert: CertView): Html {
    const names = (cert.domains || cert.domain).split('\n').filter(Boolean)
    const daysLeft = cert.expires_at ? Math.floor((cert.expires_at - Math.floor(Date.now() / 1000)) / 86400) : -1
    return el.tr(
      {},
      el.td(
        {},
        el.div({ style: 'font-weight:500;' }, cert.domain),
        names.length > 1 ? el.div({ style: 'font-size:11px; color:var(--text-muted);' }, names.slice(1).join(', ')) : null
      ),
      el.td(
        {},
        this.badge(cert.provider, PROVIDER_VARIANTS[cert.provider] || 'info'),
        cert.provider === 'letsencrypt'
          ? el.div({ style: 'font-size:11px; color:var(--text-muted);' }, cert.dns_provider ? 'DNS-01 · cloudflare' : 'HTTP-01')
          : null
      ),
      el.td({}, cert.expires_at ? this.formatDate(cert.expires_at) : 'unknown'),
      el.td({}, daysLeft < 0 ? this.badge('expired', 'danger') : this.badge(`${daysLeft}d left`, daysLeft < 30 ? 'warning' : 'success')),
      el.td({}, cert.auto_renew ? this.badge('auto', 'success') : el.span({ style: 'color:var(--text-muted);' }, 'manual')),
      el.td(
        {},
        el.div(
          { style: 'display:flex; gap:4px;' },
          cert.provider === 'letsencrypt'
            ? this.actionButton('POST', `/api/proxy/certs/${cert.id}/renew`, 'Renew', {
                confirm: `Renew certificate for ${cert.domain}?`,
                busy: 'Renewing...',
              })
            : null,
          this.actionButton('DELETE', `/api/proxy/certs/${cert.id}`, TRASH, {
            size: 'sm shelf-btn-icon',
            danger: true,
            confirm: `Delete certificate for ${cert.domain}?`,
          })
        )
      )
    )
  }

  private uncovered(): Html | null {
    const { domainsWithoutCert } = this.props
    if (!domainsWithoutCert.length) return null
    return this.card(
      [
        el.div({ style: 'font-size:13px; font-weight:600; margin-bottom:8px;' }, 'Domains without SSL'),
        el.div(
          { style: 'display:flex; flex-wrap:wrap; gap:8px;' },
          domainsWithoutCert.map((domain) =>
            el.div(
              { style: 'display:flex; align-items:center; gap:8px; padding:6px 12px; background:var(--bg-tertiary); border-radius:var(--radius); font-size:13px;' },
              domain,
              el.button(
                { ...openDialog('issue-dialog'), ...fills('#le-form [name=domains]', domain), class: 'shelf-btn shelf-btn-primary shelf-btn-sm', style: 'padding:2px 8px; font-size:11px;' },
                'Issue SSL'
              )
            )
          )
        ),
      ],
      'margin-top:24px;'
    )
  }

  private tabs(): Html {
    return this.tabBar('issuer', [
      ['le', "Let's Encrypt"],
      ['self', 'Self-signed'],
      ['manual', 'Manual upload'],
    ])
  }

  private letsEncryptForm(): Html {
    return el.form(
      { id: 'le-form', style: DIALOG.body, ...panel('issuer', 'le', true), ...submits('POST', '/api/proxy/certs/issue', { busy: 'Issuing...' }) },
      this.field(
        'Domains (줄 단위 — SAN 인증서, *.example.com 가능)',
        this.textarea({ name: 'domains', rows: 3, placeholder: 'example.com\nwww.example.com\n*.example.com', required: true })
      ),
      this.field('Email', this.input({ type: 'email', name: 'email', value: this.props.defaultEmail, placeholder: 'admin@example.com' })),
      this.field(
        'Challenge',
        el.select(
          { name: 'challenge', style: FORM.input, ...revealsWhen('#dns-token-field', 'dns') },
          el.option({ value: 'http' }, 'HTTP-01 — 포트 80으로 검증 (기본)'),
          el.option({ value: 'dns' }, 'DNS-01 — Cloudflare (와일드카드 필수)')
        )
      ),
      el.div(
        { ...revealed('dns-token-field') },
        this.field(
          'Cloudflare API Token (Zone.DNS Edit)',
          this.input({ type: 'password', name: 'dns_token', autocomplete: 'off', placeholder: 'cf_...', 'data-omit-empty': '' }),
          '갱신 때 재사용하도록 저장됩니다 (API 응답에는 노출 안 됨)'
        )
      ),
      this.notice(
        raw('HTTP-01: 도메인이 이 서버 IP를 가리키고 포트 80이 열려 있어야 합니다.<br>DNS-01: 서버 노출 없이 발급 가능, *.도메인 와일드카드 지원.')
      ),
      this.dialogActions('issue-dialog', 'Issue certificate')
    )
  }

  private selfSignedForm(): Html {
    return el.form(
      { style: DIALOG.body, ...panel('issuer', 'self'), ...submits('POST', '/api/proxy/certs/selfsigned') },
      this.field('Domain', this.input({ type: 'text', name: 'domain', placeholder: 'shelf.local', required: true })),
      this.notice("로컬/LAN 전용. 도메인 + *.도메인을 커버하는 10년짜리 인증서를 만듭니다. 브라우저 경고가 뜨므로 공개 서비스에는 Let's Encrypt를 쓰세요."),
      this.dialogActions('issue-dialog', 'Generate')
    )
  }

  private manualForm(): Html {
    return el.form(
      { style: DIALOG.body, ...panel('issuer', 'manual'), ...submits('POST', '/api/proxy/certs/upload') },
      this.field('Domain', this.input({ type: 'text', name: 'domain', placeholder: 'example.com', required: true })),
      this.field('Certificate (PEM)', this.textarea({ name: 'cert', rows: 4, placeholder: '-----BEGIN CERTIFICATE-----', required: true })),
      this.field('Private key (PEM)', this.textarea({ name: 'key', rows: 4, placeholder: '-----BEGIN PRIVATE KEY-----', required: true })),
      this.dialogActions('issue-dialog', 'Upload certificate')
    )
  }
}

export class AccessLogsPage extends ProxyPage {
  constructor(private readonly props: { logs: AccessLog[]; selectedDomain: string }) {
    super()
  }

  render(): string {
    const { logs, selectedDomain } = this.props
    const domains = [...new Set(logs.map((log) => log.domain))]
    const filter = el.select(
      {
        onchange: "location.href='/admin/proxy/logs' + (this.value ? '?domain=' + this.value : '')",
        style: 'padding:4px 8px; border:1px solid var(--border); border-radius:var(--radius-sm); background:var(--bg); color:var(--text); font-size:13px;',
      },
      el.option({ value: '' }, 'All domains'),
      ...domains.map((domain) => el.option({ value: domain, selected: domain === selectedDomain }, domain))
    )

    return join([
      this.sectionHeader('Access logs', filter),
      logs.length
        ? this.tableCard(['Time', 'Domain', 'Method', 'Path', 'Status', 'Duration'], logs.map((log) => this.row(log)))
        : el.div({ style: 'text-align:center; padding:48px; color:var(--text-muted);' }, 'No access logs yet.'),
    ]).toString()
  }

  private row(log: AccessLog): Html {
    const variant = log.status < 300 ? 'success' : log.status < 400 ? 'info' : log.status < 500 ? 'warning' : 'danger'
    return el.tr(
      {},
      el.td({ style: 'font-size:12px; color:var(--text-muted);' }, this.formatDateTime(log.created_at)),
      el.td({}, log.domain),
      el.td({}, el.code({ style: 'font-size:12px;' }, log.method)),
      el.td({ style: 'max-width:200px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;' }, log.path),
      el.td({}, this.badge(log.status, variant)),
      el.td({ style: 'font-size:12px; color:var(--text-muted);' }, `${log.duration_ms}ms`)
    )
  }
}
