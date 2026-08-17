import { Page, FORM, DIALOG, type Html } from '../../ui/page.js'
import type { Project, Deployment } from './repositories.js'

export type DisplayStatus = 'running' | 'stopped' | 'crashed' | 'deploying'

const STATUS_BADGES: Record<DisplayStatus, string> = {
  running: '<span class="shelf-badge shelf-badge-success">running</span>',
  deploying: '<span class="shelf-badge shelf-badge-info">deploying...</span>',
  crashed: '<span class="shelf-badge shelf-badge-danger">crashed</span>',
  stopped: '<span class="shelf-badge shelf-badge-warning">stopped</span>',
}

const DEPLOY_STATUS_COLORS: Record<string, string> = {
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

abstract class DeployPage extends Page {
  protected statusBadge(status: DisplayStatus): string {
    return STATUS_BADGES[status] || ''
  }

  protected deployBadge(status: string): string {
    return `<span class="shelf-badge shelf-badge-${DEPLOY_STATUS_COLORS[status] || 'info'}">${status}</span>`
  }

  protected shortCommit(hash: string): string {
    return (hash || '').slice(0, 7)
  }

  protected duration(ms: number): string {
    return ms ? `${Math.round(ms / 1000)}s` : '-'
  }

  protected lifecycleButtons(project: Project, status: DisplayStatus): string {
    const toggle = status === 'running'
      ? `<button onclick="stopProject(${project.id})" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Stop</button>`
      : `<button onclick="startProject(${project.id})" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Start</button>`
    const open = project.port
      ? `<a href="http://localhost:${project.port}" target="_blank" class="shelf-btn shelf-btn-ghost shelf-btn-sm">Open</a>`
      : ''
    return `
      <button onclick="deployNow(${project.id})" class="shelf-btn shelf-btn-primary shelf-btn-sm" ${status === 'deploying' ? 'disabled' : ''}>Deploy</button>
      ${toggle}
      ${open}`
  }

  protected lifecycleScripts(): string {
    return `
      async function startProject(id) { await fetch('/api/deploy/projects/' + id + '/start', { method: 'POST' }); location.reload(); }
      async function stopProject(id) { await fetch('/api/deploy/projects/' + id + '/stop', { method: 'POST' }); location.reload(); }`
  }
}

export class ProjectsPage extends DeployPage {
  constructor(private readonly props: { items: ProjectListItem[]; webhookPort: number }) {
    super()
  }

  render(): string {
    const { items, webhookPort } = this.props
    const newAppButton = `<button onclick="document.getElementById('add-dialog').style.display='flex'" class="shelf-btn shelf-btn-primary shelf-btn-sm">+ New app</button>`
    const body = items.length
      ? `<div style="display:grid; grid-template-columns:repeat(auto-fill, minmax(340px, 1fr)); gap:16px;">${items.map((item) => this.card_(item)).join('')}</div>`
      : this.emptyState(
          'No apps',
          'Deploy a Git repository (with a Dockerfile) or run any Docker image.',
          `<button onclick="document.getElementById('add-dialog').style.display='flex'" class="shelf-btn shelf-btn-primary">+ New app</button>`
        )

    return `
      ${this.sectionHeader(`Apps (${items.length})`, newAppButton)}
      ${body}
      <div style="margin-top:16px; font-size:12px; color:var(--text-muted);">Webhook server on :${webhookPort} · Apps run as Docker containers (shelf-{name})</div>
      ${this.addDialog()}
      ${this.script(this.scripts())}`
  }

  private card_(item: ProjectListItem): string {
    const { project: p, status, lastDeploy } = item
    const source = p.source_type === 'image' ? p.image : `${p.repo_url.replace(/^https?:\/\//, '')} · ${p.branch}`
    const lastDeployText = lastDeploy
      ? `<span>last deploy: ${lastDeploy.status} · ${this.formatDateTime(lastDeploy.created_at)}</span>`
      : '<span>never deployed</span>'

    return `
      <div class="shelf-card" style="display:flex; flex-direction:column; gap:12px;">
        <div style="display:flex; justify-content:space-between; align-items:flex-start;">
          <div>
            <a href="/admin/deploy/projects/${p.id}" style="font-size:15px; font-weight:600; color:var(--text); text-decoration:none;">${this.escape(p.name)}</a>
            <div style="font-size:12px; color:var(--text-muted); margin-top:2px;">
              <span class="shelf-badge shelf-badge-${p.source_type === 'image' ? 'info' : 'success'}" style="margin-right:6px;">${p.source_type}</span>${this.escape(source)}
            </div>
          </div>
          ${this.statusBadge(status)}
        </div>
        <div style="display:flex; gap:12px; font-size:12px; color:var(--text-secondary);">
          ${p.port ? `<span>:${p.port} &rarr; ${p.container_port || '?'}</span>` : ''}
          ${p.domain ? `<span>${this.escape(p.domain)}</span>` : ''}
          ${lastDeployText}
        </div>
        <div style="display:flex; gap:6px;">
          ${this.lifecycleButtons(p, status)}
          <a href="/admin/deploy/projects/${p.id}" class="shelf-btn shelf-btn-ghost shelf-btn-sm">Detail</a>
        </div>
      </div>`
  }

  private addDialog(): Html {
    const form = `
      <div style="display:flex; gap:8px; margin-bottom:16px;">
        <button type="button" onclick="setSource('git')" id="tab-git" class="shelf-btn shelf-btn-primary shelf-btn-sm">Git repository</button>
        <button type="button" onclick="setSource('image')" id="tab-image" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Docker image</button>
      </div>
      <form id="add-form" style="${DIALOG.body}">
        <input type="hidden" name="source_type" value="git">
        ${this.field('Name', `<input type="text" name="name" placeholder="my-app" required pattern="[a-zA-Z0-9-_]+" style="${FORM.input}">`)}
        <div id="git-fields" style="${DIALOG.body}">
          ${this.field('Git repository URL (Dockerfile 필수)', `<input type="text" name="repo_url" placeholder="https://github.com/user/repo.git" style="${FORM.input}">`)}
          <div style="width:160px;">
            ${this.field('Branch', `<input type="text" name="branch" value="main" style="${FORM.input}">`)}
          </div>
          ${this.field('Access token (private 저장소, 선택)', `<input type="password" name="git_token" placeholder="ghp_... / github_pat_..." autocomplete="off" style="${FORM.input}">`, 'GitHub fine-grained PAT (Contents: read-only) 권장')}
        </div>
        <div id="image-fields" style="display:none;">
          ${this.field('Docker image', `<input type="text" name="image" placeholder="nginx:alpine 또는 ghcr.io/user/app:latest" style="${FORM.input}">`)}
        </div>
        ${this.portFields()}
        ${this.field('Domain (optional — auto-registered to proxy)', `<input type="text" name="domain" placeholder="app.example.com" style="${FORM.input}">`)}
        ${this.field('Environment variables (KEY=VALUE per line)', `<textarea name="env" rows="2" placeholder="DATABASE_URL=..." style="${FORM.input} ${FORM.mono}"></textarea>`)}
        ${this.field('Volumes (host:container per line, optional)', `<textarea name="volumes" rows="2" placeholder="/srv/data:/data" style="${FORM.input} ${FORM.mono}"></textarea>`)}
        ${this.checkbox('auto_deploy', 'Auto deploy on webhook (push)', true)}
        ${this.dialogActions('add-dialog', 'Create app')}
      </form>`
    return this.dialog('add-dialog', 'New app', form, 520)
  }

  private portFields(): string {
    return `
      <div style="display:flex; gap:12px;">
        <div style="flex:1;">${this.field('Host port', `<input type="number" name="port" placeholder="3000" style="${FORM.input}">`)}</div>
        <div style="flex:1;">${this.field('Container port', `<input type="number" name="container_port" placeholder="80" style="${FORM.input}">`)}</div>
      </div>`
  }

  private scripts(): string {
    return `
      function setSource(type) {
        document.querySelector('#add-form [name=source_type]').value = type;
        document.getElementById('git-fields').style.display = type === 'git' ? 'flex' : 'none';
        document.getElementById('image-fields').style.display = type === 'image' ? 'block' : 'none';
        document.getElementById('tab-git').className = 'shelf-btn shelf-btn-sm shelf-btn-' + (type === 'git' ? 'primary' : 'secondary');
        document.getElementById('tab-image').className = 'shelf-btn shelf-btn-sm shelf-btn-' + (type === 'image' ? 'primary' : 'secondary');
      }
      async function deployNow(id) {
        event.target.disabled = true;
        event.target.textContent = 'Deploying...';
        const res = await fetch('/api/deploy/projects/' + id + '/deploy', { method: 'POST' });
        const json = await res.json();
        if (!json.ok) alert('Deploy failed: ' + (json.error?.message || ''));
        location.reload();
      }
      ${this.lifecycleScripts()}
      document.getElementById('add-form')?.addEventListener('submit', async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        const obj = {
          name: fd.get('name'),
          source_type: fd.get('source_type'),
          repo_url: fd.get('repo_url') || '',
          branch: fd.get('branch') || 'main',
          git_token: fd.get('git_token') || '',
          image: fd.get('image') || '',
          port: fd.get('port') ? Number(fd.get('port')) : null,
          container_port: fd.get('container_port') ? Number(fd.get('container_port')) : null,
          domain: fd.get('domain') || '',
          env: fd.get('env') || '',
          volumes: fd.get('volumes') || '',
          auto_deploy: fd.get('auto_deploy') === 'on',
        };
        const res = await fetch('/api/deploy/projects', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(obj),
        });
        const json = await res.json();
        if (json.ok) location.href = '/admin/deploy/projects/' + json.data.id;
        else alert(json.error?.message || 'Failed');
      });`
  }
}

export class ProjectDetailPage extends DeployPage {
  constructor(
    private readonly props: {
      project: Project
      status: DisplayStatus
      deployments: Deployment[]
      webhookPort: number
    }
  ) {
    super()
  }

  render(): string {
    const { project: p, status, deployments } = this.props
    return `
      <div style="margin-bottom:16px;"><a href="/admin/deploy" style="font-size:13px; color:var(--text-muted); text-decoration:none;">&larr; Apps</a></div>
      ${this.header()}
      <div style="display:grid; grid-template-columns:1fr 1fr; gap:16px; margin-bottom:16px;">
        ${this.configCard()}
        ${this.webhookCard()}
      </div>
      ${this.logsCard()}
      ${this.sectionHeader('Deployments')}
      ${deployments.length
        ? this.tableCard(['Time', 'Commit', 'Message', 'Status', 'Trigger', 'Duration', ''], deployments.map((d, i) => this.deployRow(d, i)).join(''))
        : '<div style="text-align:center; padding:32px; color:var(--text-muted);">No deployments yet. Click Deploy to start.</div>'}
      ${this.logDialog()}
      ${this.editDialog()}
      ${this.script(this.scripts())}`
  }

  private header(): string {
    const { project: p, status } = this.props
    const actions = `
      ${this.lifecycleButtons(p, status)}
      <button onclick="document.getElementById('edit-dialog').style.display='flex'" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Edit</button>
      <button onclick="deleteProject(${p.id}, '${this.escape(p.name)}')" class="shelf-btn shelf-btn-ghost shelf-btn-sm" style="color:var(--danger);">Delete</button>`
    return `
      <div class="shelf-section-header">
        <div style="display:flex; align-items:center; gap:12px;">
          <h2 class="shelf-section-title">${this.escape(p.name)}</h2>
          ${this.statusBadge(status)}
        </div>
        <div style="display:flex; gap:6px;">${actions}</div>
      </div>`
  }

  private configCard(): Html {
    const { project: p } = this.props
    const rows: Array<[string, string] | null> = [
      ['Source', `<span class="shelf-badge shelf-badge-${p.source_type === 'image' ? 'info' : 'success'}">${p.source_type}</span>`],
      p.source_type === 'git'
        ? ['Repository', this.escape(p.repo_url)]
        : ['Image', `<code style="font-size:12px;">${this.escape(p.image)}</code>`],
      p.source_type === 'git' ? ['Branch', this.escape(p.branch)] : null,
      ['Container', `<code style="font-size:12px;">shelf-${this.escape(p.name)}</code>`],
      ['Ports', p.port ? `${p.port} &rarr; ${p.container_port || '?'} (host &rarr; container)` : '-'],
      ['Volumes', p.volumes ? `<code style="font-size:12px; white-space:pre-line;">${this.escape(p.volumes)}</code>` : '-'],
      ['Domain', p.domain ? this.escape(p.domain) : '-'],
      ['Auto deploy', p.auto_deploy ? 'on' : 'off'],
    ]
    const body = (rows.filter(Boolean) as Array<[string, string]>)
      .map(([key, value]) => `<tr><td style="color:var(--text-muted); padding:4px 12px 4px 0; white-space:nowrap;">${key}</td><td>${value}</td></tr>`)
      .join('')
    return this.card(`
      <div style="font-size:13px; font-weight:600; margin-bottom:12px;">Configuration</div>
      <table style="font-size:13px; width:100%;">${body}</table>`)
  }

  private webhookCard(): Html {
    const { project: p, webhookPort } = this.props
    if (p.source_type !== 'git') {
      return this.card(`
        <div style="font-size:13px; font-weight:600; margin-bottom:12px;">Webhook (CI/CD)</div>
        <div style="font-size:13px; color:var(--text-muted);">Image 소스 앱은 Deploy 버튼으로 최신 이미지를 pull 합니다.</div>`)
    }
    return this.card(`
      <div style="font-size:13px; font-weight:600; margin-bottom:12px;">Webhook (CI/CD)</div>
      <div style="font-size:13px; color:var(--text-secondary); margin-bottom:8px;">Add this webhook to your GitHub repository (Settings &rarr; Webhooks):</div>
      <div style="font-family:var(--font-mono); font-size:12px; background:var(--bg-tertiary); padding:8px 12px; border-radius:var(--radius); margin-bottom:8px; word-break:break-all;">
        http://&lt;server-ip&gt;:${webhookPort}/hooks/${p.id}
      </div>
      <div style="font-size:13px; color:var(--text-secondary); margin-bottom:4px;">Secret:</div>
      <div style="display:flex; gap:8px; align-items:center;">
        <code style="font-size:12px; background:var(--bg-tertiary); padding:6px 12px; border-radius:var(--radius); flex:1; overflow:hidden; text-overflow:ellipsis;">${p.webhook_secret}</code>
        <button onclick="navigator.clipboard.writeText('${p.webhook_secret}')" class="shelf-btn shelf-btn-ghost shelf-btn-sm">Copy</button>
      </div>
      <div style="font-size:12px; color:var(--text-muted); margin-top:8px;">Content type: application/json · Event: push · Branch: ${this.escape(p.branch)}</div>`)
  }

  private logsCard(): Html {
    return this.card(`
      <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:8px;">
        <div style="font-size:13px; font-weight:600;">Container logs</div>
        <button onclick="refreshLogs()" class="shelf-btn shelf-btn-ghost shelf-btn-sm">Refresh</button>
      </div>
      <pre id="runtime-logs" style="font-family:var(--font-mono); font-size:12px; background:var(--bg-tertiary); padding:12px; border-radius:var(--radius); max-height:280px; overflow:auto; white-space:pre-wrap; margin:0;">loading...</pre>`,
      'margin-bottom:16px;')
  }

  private deployRow(d: Deployment, index: number): string {
    const { project: p } = this.props
    const canRollback = p.source_type === 'git' && d.status === 'success' && !!d.commit_hash && index !== 0
    const rollback = canRollback
      ? `<button onclick="event.stopPropagation(); rollbackTo(${d.id}, '${this.shortCommit(d.commit_hash)}')" class="shelf-btn shelf-btn-ghost shelf-btn-sm">Rollback</button>`
      : ''
    return `
      <tr style="cursor:pointer;" onclick="showDeployLog(${d.id})">
        <td style="font-size:12px; color:var(--text-muted);">${this.formatDateTime(d.created_at)}</td>
        <td><code style="font-size:12px;">${this.shortCommit(d.commit_hash)}</code></td>
        <td style="max-width:240px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">${this.escape(d.commit_message) || '-'}</td>
        <td>${this.deployBadge(d.status)}</td>
        <td><span class="shelf-badge">${d.trigger_type}</span></td>
        <td style="font-size:12px; color:var(--text-muted);">${this.duration(d.duration_ms)}</td>
        <td>${rollback}</td>
      </tr>`
  }

  private logDialog(): Html {
    return this.dialog(
      'log-dialog',
      'Deployment log',
      `<pre id="deploy-log" style="font-family:var(--font-mono); font-size:12px; background:var(--bg-tertiary); padding:12px; border-radius:var(--radius); overflow:auto; white-space:pre-wrap; margin:0; max-height:60vh;"></pre>`,
      720
    )
  }

  private editDialog(): Html {
    const { project: p } = this.props
    const sourceFields = p.source_type === 'git'
      ? `
        ${this.field('Git repository URL', `<input type="text" name="repo_url" value="${this.escape(p.repo_url)}" required style="${FORM.input}">`)}
        <div style="width:160px;">${this.field('Branch', `<input type="text" name="branch" value="${this.escape(p.branch)}" style="${FORM.input}">`)}</div>
        ${this.field(`Access token ${p.git_token ? '(설정됨 — 비우면 유지)' : '(private 저장소, 선택)'}`, `<input type="password" name="git_token" placeholder="${p.git_token ? '••••••••' : 'ghp_...'}" autocomplete="off" style="${FORM.input}">`)}`
      : this.field('Docker image', `<input type="text" name="image" value="${this.escape(p.image)}" required style="${FORM.input}">`)

    const form = `
      <form id="edit-form" style="${DIALOG.body}">
        ${sourceFields}
        <div style="display:flex; gap:12px;">
          <div style="flex:1;">${this.field('Host port', `<input type="number" name="port" value="${p.port ?? ''}" style="${FORM.input}">`)}</div>
          <div style="flex:1;">${this.field('Container port', `<input type="number" name="container_port" value="${p.container_port ?? ''}" style="${FORM.input}">`)}</div>
        </div>
        ${this.field('Domain', `<input type="text" name="domain" value="${this.escape(p.domain)}" style="${FORM.input}">`)}
        ${this.field('Environment variables (KEY=VALUE per line)', `<textarea name="env" rows="3" style="${FORM.input} ${FORM.mono}">${this.escape(p.env)}</textarea>`)}
        ${this.field('Volumes (host:container per line)', `<textarea name="volumes" rows="2" style="${FORM.input} ${FORM.mono}">${this.escape(p.volumes)}</textarea>`)}
        ${this.checkbox('auto_deploy', 'Auto deploy on webhook (push)', !!p.auto_deploy)}
        <div style="font-size:12px; color:var(--text-muted); padding:8px 12px; background:var(--bg-tertiary); border-radius:var(--radius);">
          변경 사항은 다음 Deploy 때 컨테이너에 적용됩니다.
        </div>
        ${this.dialogActions('edit-dialog', 'Save')}
      </form>`
    return this.dialog('edit-dialog', `Edit ${this.escape(p.name)}`, form, 520)
  }

  private scripts(): string {
    const { project: p } = this.props
    const sourcePayload = p.source_type === 'git'
      ? `repo_url: fd.get('repo_url'), branch: fd.get('branch') || 'main', ...(fd.get('git_token') ? { git_token: fd.get('git_token') } : {}),`
      : `image: fd.get('image'),`

    return `
      const PROJECT_ID = ${p.id};
      async function deployNow(id) {
        document.querySelectorAll('button').forEach(b => b.disabled = true);
        const res = await fetch('/api/deploy/projects/' + id + '/deploy', { method: 'POST' });
        const json = await res.json();
        if (!json.ok) alert('Deploy failed: ' + (json.error?.message || ''));
        location.reload();
      }
      ${this.lifecycleScripts()}
      async function deleteProject(id, name) {
        if (!confirm('Delete app "' + name + '"? Container and built image will be removed.')) return;
        await fetch('/api/deploy/projects/' + id, { method: 'DELETE' });
        location.href = '/admin/deploy';
      }
      async function refreshLogs() {
        const res = await fetch('/api/deploy/projects/' + PROJECT_ID + '/logs');
        const json = await res.json();
        const el = document.getElementById('runtime-logs');
        el.textContent = json.data.logs.length ? json.data.logs.join('\\n') : '(no output — container ' + json.data.status + ')';
        el.scrollTop = el.scrollHeight;
      }
      async function showDeployLog(id) {
        const res = await fetch('/api/deploy/deployments/' + id);
        const json = await res.json();
        document.getElementById('deploy-log').textContent = json.data.log || '(empty)';
        document.getElementById('log-dialog').style.display = 'flex';
      }
      async function rollbackTo(id, commit) {
        if (!confirm('Roll back to commit ' + commit + '? The app will be rebuilt from that commit.')) return;
        document.querySelectorAll('button').forEach(b => b.disabled = true);
        const res = await fetch('/api/deploy/deployments/' + id + '/rollback', { method: 'POST' });
        const json = await res.json();
        if (!json.ok) alert('Rollback failed: ' + (json.error?.message || ''));
        location.reload();
      }
      document.getElementById('edit-form')?.addEventListener('submit', async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        const obj = {
          ${sourcePayload}
          port: fd.get('port') ? Number(fd.get('port')) : null,
          container_port: fd.get('container_port') ? Number(fd.get('container_port')) : null,
          domain: fd.get('domain') || '',
          env: fd.get('env') || '',
          volumes: fd.get('volumes') || '',
          auto_deploy: fd.get('auto_deploy') === 'on',
        };
        const res = await fetch('/api/deploy/projects/${p.id}', {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(obj),
        });
        const json = await res.json();
        if (json.ok) location.reload();
        else alert(json.error?.message || 'Failed');
      });
      refreshLogs();
      setInterval(refreshLogs, 5000);`
  }
}

export class DeploymentsPage extends DeployPage {
  constructor(private readonly props: { rows: Array<Deployment & { project_name: string | null }> }) {
    super()
  }

  render(): string {
    const { rows } = this.props
    return `
      ${this.sectionHeader('All deployments')}
      ${rows.length
        ? this.tableCard(['Time', 'App', 'Commit', 'Message', 'Status', 'Trigger', 'Duration'], rows.map((row) => this.row(row)).join(''))
        : '<div style="text-align:center; padding:48px; color:var(--text-muted);">No deployments yet.</div>'}`
  }

  private row(d: Deployment & { project_name: string | null }): string {
    return `
      <tr>
        <td style="font-size:12px; color:var(--text-muted);">${this.formatDateTime(d.created_at)}</td>
        <td><a href="/admin/deploy/projects/${d.project_id}" style="color:var(--accent); text-decoration:none;">${this.escape(d.project_name) || '(deleted)'}</a></td>
        <td><code style="font-size:12px;">${this.shortCommit(d.commit_hash)}</code></td>
        <td style="max-width:280px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">${this.escape(d.commit_message) || '-'}</td>
        <td>${this.deployBadge(d.status)}</td>
        <td><span class="shelf-badge">${d.trigger_type}</span></td>
        <td style="font-size:12px; color:var(--text-muted);">${this.duration(d.duration_ms)}</td>
      </tr>`
  }
}
