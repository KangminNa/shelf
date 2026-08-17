import {
  Page,
  FORM,
  DIALOG,
  el,
  join,
  loads,
  live,
  copies,
  panel,
  tabValue,
  submits,
  type Html,
  type Child,
} from '../../ui/page.js'
import type { Project, Deployment } from './repositories.js'

export type DisplayStatus = 'running' | 'stopped' | 'crashed' | 'deploying'

const STATUS_VARIANTS: Record<DisplayStatus, string> = {
  running: 'success',
  deploying: 'info',
  crashed: 'danger',
  stopped: 'warning',
}

const DEPLOY_VARIANTS: Record<string, string> = {
  success: 'success',
  failed: 'danger',
  running: 'info',
  pending: 'warning',
}

export interface ProjectListItem {
  project: Project
  status: DisplayStatus
  lastDeploy?: Deployment
}

const MUTED = 'font-size:12px; color:var(--text-muted);'
const MONO = 'font-family:var(--font-mono); font-size:12px;'
const LOG_BOX = `${MONO} background:var(--bg-tertiary); padding:12px; border-radius:var(--radius); overflow:auto; white-space:pre-wrap; margin:0;`

abstract class DeployPage extends Page {
  protected badge(label: Child, variant?: string): Html {
    return el.span({ class: `shelf-badge${variant ? ` shelf-badge-${variant}` : ''}` }, label)
  }

  protected statusBadge(status: DisplayStatus): Html {
    return this.badge(status === 'deploying' ? 'deploying...' : status, STATUS_VARIANTS[status])
  }

  protected sourceBadge(project: Project): Html {
    return this.badge(project.source_type, project.source_type === 'image' ? 'info' : 'success')
  }

  protected commit(hash: string): Html {
    return el.code({ style: MONO }, (hash || '').slice(0, 7))
  }

  protected duration(ms: number): string {
    return ms ? `${Math.round(ms / 1000)}s` : '-'
  }

  protected lifecycleButtons(project: Project, status: DisplayStatus): Child {
    const api = `/api/deploy/projects/${project.id}`
    return [
      this.actionButton('POST', `${api}/deploy`, 'Deploy', { variant: 'primary', busy: 'Deploying...' }),
      status === 'running'
        ? this.actionButton('POST', `${api}/stop`, 'Stop', { variant: 'secondary' })
        : this.actionButton('POST', `${api}/start`, 'Start', { variant: 'secondary' }),
      project.port
        ? el.a(
            { href: `http://localhost:${project.port}`, target: '_blank', class: 'shelf-btn shelf-btn-ghost shelf-btn-sm' },
            'Open'
          )
        : null,
    ]
  }

  protected portFields(port: number | null, containerPort: number | null): Html {
    return el.div(
      { style: 'display:flex; gap:12px;' },
      el.div(
        { style: 'flex:1;' },
        this.field('Host port (선택 — 직접 노출할 때만)', this.input({ type: 'number', name: 'port', value: port ?? '', placeholder: '비워두세요' }))
      ),
      el.div(
        { style: 'flex:1;' },
        this.field(
          'Container port',
          this.input({ type: 'number', name: 'container_port', value: containerPort ?? '', placeholder: '3000', required: true }),
          '앱이 리슨하는 포트. 프록시가 이 포트로 직접 연결합니다'
        )
      )
    )
  }

  protected settingFields(project?: Project): Child {
    return [
      this.portFields(project?.port ?? null, project?.container_port ?? null),
      this.field(
        'Domain (선택 — 프록시에 자동 등록)',
        this.input({ type: 'text', name: 'domain', value: project?.domain ?? '', placeholder: 'app.example.com' })
      ),
      this.field(
        'Environment variables (KEY=VALUE per line)',
        this.textarea({ name: 'env', rows: 3, placeholder: 'DATABASE_URL=...' }, project?.env)
      ),
      this.field(
        'Volumes (host:container per line)',
        this.textarea({ name: 'volumes', rows: 2, placeholder: '/srv/data:/data' }, project?.volumes)
      ),
      this.checkbox('auto_deploy', 'Auto deploy on webhook (push)', project ? !!project.auto_deploy : true),
    ]
  }

  protected gitFields(project?: Project): Child {
    return [
      this.field(
        'Git repository URL (Dockerfile 필수)',
        this.input({ type: 'text', name: 'repo_url', value: project?.repo_url ?? '', placeholder: 'https://github.com/user/repo.git' })
      ),
      el.div(
        { style: 'width:160px;' },
        this.field('Branch', this.input({ type: 'text', name: 'branch', value: project?.branch || 'main' }))
      ),
      this.field(
        `Access token ${project?.git_token ? '(설정됨 — 비우면 유지)' : '(private 저장소, 선택)'}`,
        this.input({
          type: 'password',
          name: 'git_token',
          placeholder: project?.git_token ? '••••••••' : 'ghp_... / github_pat_...',
          autocomplete: 'off',
          'data-omit-empty': '',
        }),
        'GitHub fine-grained PAT (Contents: read-only) 권장'
      ),
    ]
  }

  protected imageField(project?: Project): Html {
    return this.field(
      'Docker image',
      this.input({ type: 'text', name: 'image', value: project?.image ?? '', placeholder: 'nginx:alpine 또는 ghcr.io/user/app:latest' })
    )
  }
}

export class ProjectsPage extends DeployPage {
  constructor(private readonly props: { items: ProjectListItem[]; webhookPort: number }) {
    super()
  }

  render(): string {
    const { items, webhookPort } = this.props
    return join([
      this.sectionHeader(`Apps (${items.length})`, this.openButton('add-dialog', '+ New app')),
      items.length
        ? el.div(
            { style: 'display:grid; grid-template-columns:repeat(auto-fill, minmax(340px, 1fr)); gap:16px;' },
            items.map((item) => this.projectCard(item))
          )
        : this.emptyState(
            'No apps',
            'Deploy a Git repository (with a Dockerfile) or run any Docker image.',
            this.openButton('add-dialog', '+ New app', 'primary', '')
          ),
      el.div(
        { style: `margin-top:16px; ${MUTED}` },
        `Webhook server on :${webhookPort} · Apps run as Docker containers`
      ),
      this.addDialog(),
    ]).toString()
  }

  private projectCard(item: ProjectListItem): Html {
    const { project, status, lastDeploy } = item
    const source =
      project.source_type === 'image' ? project.image : `${project.repo_url.replace(/^https?:\/\//, '')} · ${project.branch}`
    const detailUrl = `/admin/deploy/projects/${project.id}`

    return this.card(
      [
        el.div(
          { style: 'display:flex; justify-content:space-between; align-items:flex-start;' },
          el.div(
            {},
            el.a({ href: detailUrl, style: 'font-size:15px; font-weight:600; color:var(--text); text-decoration:none;' }, project.name),
            el.div({ style: `${MUTED} margin-top:2px; display:flex; align-items:center; gap:6px;` }, this.sourceBadge(project), source)
          ),
          this.statusBadge(status)
        ),
        el.div(
          { style: 'display:flex; gap:12px; font-size:12px; color:var(--text-secondary);' },
          project.port ? el.span({}, `:${project.port} → ${project.container_port || '?'}`) : null,
          project.domain ? el.span({}, project.domain) : null,
          el.span(
            {},
            lastDeploy ? `last deploy: ${lastDeploy.status} · ${this.formatDateTime(lastDeploy.created_at)}` : 'never deployed'
          )
        ),
        el.div(
          { style: 'display:flex; gap:6px;' },
          this.lifecycleButtons(project, status),
          el.a({ href: detailUrl, class: 'shelf-btn shelf-btn-ghost shelf-btn-sm' }, 'Detail')
        ),
      ],
      'display:flex; flex-direction:column; gap:12px;'
    )
  }

  private addDialog(): Html {
    const body = [
      this.tabBar('source', [
        ['git', 'Git repository'],
        ['image', 'Docker image'],
      ]),
      el.form(
        { style: DIALOG.body, ...submits('POST', '/api/deploy/projects', { then: 'redirect:/admin/deploy/projects/{id}' }) },
        el.input({ type: 'hidden', name: 'source_type', value: 'git', ...tabValue('source') }),
        this.field('Name', this.input({ type: 'text', name: 'name', placeholder: 'my-app', required: true, pattern: '[a-zA-Z0-9-_]+' })),
        el.div({ style: DIALOG.body, ...panel('source', 'git', true) }, this.gitFields()),
        el.div({ ...panel('source', 'image') }, this.imageField()),
        this.settingFields(),
        this.dialogActions('add-dialog', 'Create app')
      ),
    ]
    return this.dialog('add-dialog', 'New app', body, 520)
  }
}

export class ProjectDetailPage extends DeployPage {
  constructor(
    private readonly props: {
      project: Project
      status: DisplayStatus
      deployments: Deployment[]
      webhookPort: number
      container: string
      proxyTarget: string | null
    }
  ) {
    super()
  }

  render(): string {
    const { deployments } = this.props
    return join([
      el.div(
        { style: 'margin-bottom:16px;' },
        el.a({ href: '/admin/deploy', style: `${MUTED} text-decoration:none;` }, '← Apps')
      ),
      this.header(),
      el.div(
        { style: 'display:grid; grid-template-columns:1fr 1fr; gap:16px; margin-bottom:16px;' },
        this.configCard(),
        this.webhookCard()
      ),
      this.logsCard(),
      this.sectionHeader('Deployments'),
      deployments.length
        ? this.tableCard(
            ['Time', 'Commit', 'Message', 'Status', 'Trigger', 'Duration', ''],
            deployments.map((deployment, index) => this.deployRow(deployment, index))
          )
        : el.div({ style: 'text-align:center; padding:32px; color:var(--text-muted);' }, 'No deployments yet. Click Deploy to start.'),
      this.dialog('log-dialog', 'Deployment log', el.pre({ id: 'deploy-log', style: `${LOG_BOX} max-height:60vh;` }), 720),
      this.editDialog(),
    ]).toString()
  }

  private header(): Html {
    const { project, status } = this.props
    return el.div(
      { class: 'shelf-section-header' },
      el.div(
        { style: 'display:flex; align-items:center; gap:12px;' },
        el.h2({ class: 'shelf-section-title' }, project.name),
        this.statusBadge(status)
      ),
      el.div(
        { style: 'display:flex; gap:6px;' },
        this.lifecycleButtons(project, status),
        this.openButton('edit-dialog', 'Edit', 'secondary'),
        this.actionButton('DELETE', `/api/deploy/projects/${project.id}`, 'Delete', {
          danger: true,
          confirm: `Delete app "${project.name}"? Container and built image will be removed.`,
          then: 'redirect:/admin/deploy',
        })
      )
    )
  }

  private configCard(): Html {
    const { project, container, proxyTarget } = this.props
    const rows: Array<[string, Child] | null> = [
      ['Source', this.sourceBadge(project)],
      project.source_type === 'git' ? ['Repository', project.repo_url] : ['Image', el.code({ style: MONO }, project.image)],
      project.source_type === 'git' ? ['Branch', project.branch] : null,
      ['Container', el.code({ style: MONO }, container)],
      ['Proxy target', proxyTarget ? el.code({ style: MONO }, proxyTarget) : '-'],
      ['Host port', project.port ? `${project.port} (직접 노출)` : '없음 (프록시 전용)'],
      ['Volumes', project.volumes ? el.code({ style: `${MONO} white-space:pre-line;` }, project.volumes) : '-'],
      ['Domain', project.domain || '-'],
      ['Auto deploy', project.auto_deploy ? 'on' : 'off'],
    ]

    return this.card([
      el.div({ style: 'font-size:13px; font-weight:600; margin-bottom:12px;' }, 'Configuration'),
      el.table(
        { style: 'font-size:13px; width:100%;' },
        (rows.filter(Boolean) as Array<[string, Child]>).map(([key, value]) =>
          el.tr({}, el.td({ style: 'color:var(--text-muted); padding:4px 12px 4px 0; white-space:nowrap;' }, key), el.td({}, value))
        )
      ),
    ])
  }

  private webhookCard(): Html {
    const { project, webhookPort } = this.props
    const title = el.div({ style: 'font-size:13px; font-weight:600; margin-bottom:12px;' }, 'Webhook (CI/CD)')

    if (project.source_type !== 'git') {
      return this.card([
        title,
        el.div({ style: 'font-size:13px; color:var(--text-muted);' }, 'Image 소스 앱은 Deploy 버튼으로 최신 이미지를 pull 합니다.'),
      ])
    }

    return this.card([
      title,
      el.div(
        { style: 'font-size:13px; color:var(--text-secondary); margin-bottom:8px;' },
        'Add this webhook to your GitHub repository (Settings → Webhooks):'
      ),
      el.div(
        { style: `${MONO} background:var(--bg-tertiary); padding:8px 12px; border-radius:var(--radius); margin-bottom:8px; word-break:break-all;` },
        `http://<server-ip>:${webhookPort}/hooks/${project.id}`
      ),
      el.div({ style: 'font-size:13px; color:var(--text-secondary); margin-bottom:4px;' }, 'Secret:'),
      el.div(
        { style: 'display:flex; gap:8px; align-items:center;' },
        el.code(
          { style: `${MONO} background:var(--bg-tertiary); padding:6px 12px; border-radius:var(--radius); flex:1; overflow:hidden; text-overflow:ellipsis;` },
          project.webhook_secret
        ),
        el.button({ ...copies(project.webhook_secret), class: 'shelf-btn shelf-btn-ghost shelf-btn-sm' }, 'Copy')
      ),
      el.div({ style: `${MUTED} margin-top:8px;` }, `Content type: application/json · Event: push · Branch: ${project.branch}`),
    ])
  }

  private logsCard(): Html {
    const logsUrl = `/api/deploy/projects/${this.props.project.id}/logs`
    return this.card(
      [
        el.div(
          { style: 'display:flex; justify-content:space-between; align-items:center; margin-bottom:8px;' },
          el.div({ style: 'font-size:13px; font-weight:600;' }, 'Container logs'),
          el.button(
            { ...loads(logsUrl, { into: '#runtime-logs', pick: 'logs', empty: '(no output)' }), class: 'shelf-btn shelf-btn-ghost shelf-btn-sm' },
            'Refresh'
          )
        ),
        el.pre(
          {
            id: 'runtime-logs',
            style: `${LOG_BOX} max-height:280px;`,
            ...live(logsUrl, { pick: 'logs', every: 5000, empty: '(no output)' }),
          },
          'loading...'
        ),
      ],
      'margin-bottom:16px;'
    )
  }

  private deployRow(deployment: Deployment, index: number): Html {
    const { project } = this.props
    const canRollback = project.source_type === 'git' && deployment.status === 'success' && !!deployment.commit_hash && index !== 0

    return el.tr(
      {
        style: 'cursor:pointer;',
        ...loads(`/api/deploy/deployments/${deployment.id}`, {
          into: '#deploy-log',
          pick: 'log',
          open: 'log-dialog',
          empty: '(empty)',
        }),
      },
      el.td({ style: MUTED }, this.formatDateTime(deployment.created_at)),
      el.td({}, this.commit(deployment.commit_hash)),
      el.td({ style: 'max-width:240px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;' }, deployment.commit_message || '-'),
      el.td({}, this.badge(deployment.status, DEPLOY_VARIANTS[deployment.status] || 'info')),
      el.td({}, this.badge(deployment.trigger_type)),
      el.td({ style: MUTED }, this.duration(deployment.duration_ms)),
      el.td(
        {},
        canRollback
          ? this.actionButton('POST', `/api/deploy/deployments/${deployment.id}/rollback`, 'Rollback', {
              busy: 'Rolling back...',
              confirm: `Roll back to commit ${(deployment.commit_hash || '').slice(0, 7)}? The app will be rebuilt from that commit.`,
            })
          : null
      )
    )
  }

  private editDialog(): Html {
    const { project } = this.props
    const form = el.form(
      { style: DIALOG.body, ...submits('PATCH', `/api/deploy/projects/${project.id}`) },
      project.source_type === 'git' ? this.gitFields(project) : this.imageField(project),
      this.settingFields(project),
      this.notice('변경 사항은 다음 Deploy 때 컨테이너에 적용됩니다.'),
      this.dialogActions('edit-dialog', 'Save')
    )
    return this.dialog('edit-dialog', `Edit ${project.name}`, form, 520)
  }
}

export class DeploymentsPage extends DeployPage {
  constructor(private readonly props: { rows: Array<Deployment & { project_name: string | null }> }) {
    super()
  }

  render(): string {
    const { rows } = this.props
    return join([
      this.sectionHeader('All deployments'),
      rows.length
        ? this.tableCard(
            ['Time', 'App', 'Commit', 'Message', 'Status', 'Trigger', 'Duration'],
            rows.map((row) => this.row(row))
          )
        : el.div({ style: 'text-align:center; padding:48px; color:var(--text-muted);' }, 'No deployments yet.'),
    ]).toString()
  }

  private row(deployment: Deployment & { project_name: string | null }): Html {
    return el.tr(
      {},
      el.td({ style: MUTED }, this.formatDateTime(deployment.created_at)),
      el.td(
        {},
        el.a(
          { href: `/admin/deploy/projects/${deployment.project_id}`, style: 'color:var(--accent); text-decoration:none;' },
          deployment.project_name || '(deleted)'
        )
      ),
      el.td({}, this.commit(deployment.commit_hash)),
      el.td({ style: 'max-width:280px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;' }, deployment.commit_message || '-'),
      el.td({}, this.badge(deployment.status, DEPLOY_VARIANTS[deployment.status] || 'info')),
      el.td({}, this.badge(deployment.trigger_type)),
      el.td({ style: MUTED }, this.duration(deployment.duration_ms))
    )
  }
}
