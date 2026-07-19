import type { Project, Deployment } from './repositories.js'

export type DisplayStatus = 'running' | 'stopped' | 'crashed' | 'deploying'

const STATUS_BADGES: Record<string, string> = {
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

const INPUT_STYLE = `width:100%; padding:8px 12px; border:1px solid var(--border); border-radius:var(--radius); background:var(--bg); color:var(--text); font-size:14px;`
const LABEL_STYLE = `display:block; font-size:13px; font-weight:500; margin-bottom:4px;`

export interface ProjectListItem {
  project: Project
  status: DisplayStatus
  lastDeploy?: Deployment
}

function sourceLabel(p: Project): string {
  return p.source_type === 'image' ? p.image : `${p.repo_url.replace(/^https?:\/\//, '')} · ${p.branch}`
}

// --- Apps list ---

export function projectsPage(items: ProjectListItem[], webhookPort: number): string {
  const cards = items.map(({ project: p, status, lastDeploy }) => `
    <div class="shelf-card" style="display:flex; flex-direction:column; gap:12px;">
      <div style="display:flex; justify-content:space-between; align-items:flex-start;">
        <div>
          <a href="/admin/deploy/projects/${p.id}" style="font-size:15px; font-weight:600; color:var(--text); text-decoration:none;">${p.name}</a>
          <div style="font-size:12px; color:var(--text-muted); margin-top:2px;">
            <span class="shelf-badge shelf-badge-${p.source_type === 'image' ? 'info' : 'success'}" style="margin-right:6px;">${p.source_type}</span>${sourceLabel(p)}
          </div>
        </div>
        ${STATUS_BADGES[status] || ''}
      </div>
      <div style="display:flex; gap:12px; font-size:12px; color:var(--text-secondary);">
        ${p.port ? `<span>:${p.port} &rarr; ${p.container_port || '?'}</span>` : ''}
        ${p.domain ? `<span>${p.domain}</span>` : ''}
        ${lastDeploy ? `<span>last deploy: ${lastDeploy.status} · ${new Date(lastDeploy.created_at * 1000).toLocaleString()}</span>` : '<span>never deployed</span>'}
      </div>
      <div style="display:flex; gap:6px;">
        <button onclick="deployNow(${p.id})" class="shelf-btn shelf-btn-primary shelf-btn-sm" ${status === 'deploying' ? 'disabled' : ''}>Deploy</button>
        ${status === 'running'
          ? `<button onclick="stopProject(${p.id})" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Stop</button>`
          : `<button onclick="startProject(${p.id})" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Start</button>`}
        ${p.port ? `<a href="http://localhost:${p.port}" target="_blank" class="shelf-btn shelf-btn-ghost shelf-btn-sm">Open</a>` : ''}
        <a href="/admin/deploy/projects/${p.id}" class="shelf-btn shelf-btn-ghost shelf-btn-sm">Detail</a>
      </div>
    </div>`).join('')

  return `
    <div class="shelf-section-header">
      <h2 class="shelf-section-title">Apps (${items.length})</h2>
      <button onclick="document.getElementById('add-dialog').style.display='flex'" class="shelf-btn shelf-btn-primary shelf-btn-sm">+ New app</button>
    </div>

    ${items.length ? `
      <div style="display:grid; grid-template-columns:repeat(auto-fill, minmax(340px, 1fr)); gap:16px;">${cards}</div>
    ` : `
      <div style="text-align:center; padding:48px 24px; color:var(--text-secondary);">
        <h2 style="font-size:18px; font-weight:600; color:var(--text); margin-bottom:8px;">No apps</h2>
        <p style="font-size:13px; margin-bottom:16px;">Deploy a Git repository (with a Dockerfile) or run any Docker image.</p>
        <button onclick="document.getElementById('add-dialog').style.display='flex'" class="shelf-btn shelf-btn-primary">+ New app</button>
      </div>
    `}

    <div style="margin-top:16px; font-size:12px; color:var(--text-muted);">Webhook server on :${webhookPort} · Apps run as Docker containers (shelf-{name})</div>

    ${addProjectDialog()}
    ${listScripts()}
  `
}

function addProjectDialog(): string {
  return `
    <div id="add-dialog" style="display:none; position:fixed; inset:0; background:rgba(0,0,0,0.4); z-index:100; align-items:center; justify-content:center;">
      <div style="background:var(--bg); border:1px solid var(--border); border-radius:var(--radius-lg); padding:24px; width:100%; max-width:520px; max-height:85vh; overflow:auto; box-shadow:var(--shadow);">
        <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:20px;">
          <h3 style="font-size:16px; font-weight:600;">New app</h3>
          <button onclick="document.getElementById('add-dialog').style.display='none'" class="shelf-btn shelf-btn-ghost shelf-btn-icon">&times;</button>
        </div>

        <div style="display:flex; gap:8px; margin-bottom:16px;">
          <button type="button" onclick="setSource('git')" id="tab-git" class="shelf-btn shelf-btn-primary shelf-btn-sm">Git repository</button>
          <button type="button" onclick="setSource('image')" id="tab-image" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Docker image</button>
        </div>

        <form id="add-form" style="display:flex; flex-direction:column; gap:14px;">
          <input type="hidden" name="source_type" value="git">
          <div>
            <label style="${LABEL_STYLE}">Name</label>
            <input type="text" name="name" placeholder="my-app" required pattern="[a-zA-Z0-9-_]+" style="${INPUT_STYLE}">
          </div>

          <div id="git-fields" style="display:flex; flex-direction:column; gap:14px;">
            <div>
              <label style="${LABEL_STYLE}">Git repository URL (Dockerfile 필수)</label>
              <input type="text" name="repo_url" placeholder="https://github.com/user/repo.git" style="${INPUT_STYLE}">
            </div>
            <div style="width:160px;">
              <label style="${LABEL_STYLE}">Branch</label>
              <input type="text" name="branch" value="main" style="${INPUT_STYLE}">
            </div>
          </div>

          <div id="image-fields" style="display:none;">
            <label style="${LABEL_STYLE}">Docker image</label>
            <input type="text" name="image" placeholder="nginx:alpine 또는 ghcr.io/user/app:latest" style="${INPUT_STYLE}">
          </div>

          <div style="display:flex; gap:12px;">
            <div style="flex:1;">
              <label style="${LABEL_STYLE}">Host port</label>
              <input type="number" name="port" placeholder="3000" style="${INPUT_STYLE}">
            </div>
            <div style="flex:1;">
              <label style="${LABEL_STYLE}">Container port</label>
              <input type="number" name="container_port" placeholder="80" style="${INPUT_STYLE}">
            </div>
          </div>

          <div>
            <label style="${LABEL_STYLE}">Domain (optional — auto-registered to proxy)</label>
            <input type="text" name="domain" placeholder="app.example.com" style="${INPUT_STYLE}">
          </div>
          <div>
            <label style="${LABEL_STYLE}">Environment variables (KEY=VALUE per line)</label>
            <textarea name="env" rows="2" placeholder="DATABASE_URL=..." style="${INPUT_STYLE} font-family:var(--font-mono); font-size:12px; resize:vertical;"></textarea>
          </div>
          <div>
            <label style="${LABEL_STYLE}">Volumes (host:container per line, optional)</label>
            <textarea name="volumes" rows="2" placeholder="/srv/data:/data" style="${INPUT_STYLE} font-family:var(--font-mono); font-size:12px; resize:vertical;"></textarea>
          </div>
          <label style="display:flex; align-items:center; gap:6px; font-size:13px; cursor:pointer;">
            <input type="checkbox" name="auto_deploy" checked> Auto deploy on webhook (push)
          </label>
          <div style="display:flex; gap:8px; justify-content:flex-end; padding-top:8px;">
            <button type="button" onclick="document.getElementById('add-dialog').style.display='none'" class="shelf-btn shelf-btn-secondary">Cancel</button>
            <button type="submit" class="shelf-btn shelf-btn-primary">Create app</button>
          </div>
        </form>
      </div>
    </div>`
}

function listScripts(): string {
  return `
    <script>
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
      async function startProject(id) { await fetch('/api/deploy/projects/' + id + '/start', { method: 'POST' }); location.reload(); }
      async function stopProject(id) { await fetch('/api/deploy/projects/' + id + '/stop', { method: 'POST' }); location.reload(); }
      document.getElementById('add-form')?.addEventListener('submit', async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        const obj = {
          name: fd.get('name'),
          source_type: fd.get('source_type'),
          repo_url: fd.get('repo_url') || '',
          branch: fd.get('branch') || 'main',
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
      });
    </script>`
}

// --- App detail ---

export function projectDetailPage(p: Project, status: DisplayStatus, deployments: Deployment[], webhookPort: number): string {
  const deployRows = deployments.map((d) => `
    <tr style="cursor:pointer;" onclick="showDeployLog(${d.id})">
      <td style="font-size:12px; color:var(--text-muted);">${new Date(d.created_at * 1000).toLocaleString()}</td>
      <td><code style="font-size:12px;">${(d.commit_hash || '').slice(0, 7)}</code></td>
      <td style="max-width:240px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">${d.commit_message || '-'}</td>
      <td><span class="shelf-badge shelf-badge-${DEPLOY_STATUS_COLORS[d.status] || 'info'}">${d.status}</span></td>
      <td><span class="shelf-badge">${d.trigger_type}</span></td>
      <td style="font-size:12px; color:var(--text-muted);">${d.duration_ms ? Math.round(d.duration_ms / 1000) + 's' : '-'}</td>
    </tr>`).join('')

  const configRows = [
    ['Source', `<span class="shelf-badge shelf-badge-${p.source_type === 'image' ? 'info' : 'success'}">${p.source_type}</span>`],
    p.source_type === 'git' ? ['Repository', p.repo_url] : ['Image', `<code style="font-size:12px;">${p.image}</code>`],
    p.source_type === 'git' ? ['Branch', p.branch] : null,
    ['Container', `<code style="font-size:12px;">shelf-${p.name}</code>`],
    ['Ports', p.port ? `${p.port} &rarr; ${p.container_port || '?'} (host &rarr; container)` : '-'],
    ['Volumes', p.volumes ? `<code style="font-size:12px; white-space:pre-line;">${p.volumes}</code>` : '-'],
    ['Domain', p.domain || '-'],
    ['Auto deploy', p.auto_deploy ? 'on' : 'off'],
  ].filter(Boolean) as [string, string][]

  return `
    <div style="margin-bottom:16px;"><a href="/admin/deploy" style="font-size:13px; color:var(--text-muted); text-decoration:none;">&larr; Apps</a></div>

    <div class="shelf-section-header">
      <div style="display:flex; align-items:center; gap:12px;">
        <h2 class="shelf-section-title">${p.name}</h2>
        ${STATUS_BADGES[status] || ''}
      </div>
      <div style="display:flex; gap:6px;">
        <button onclick="deployNow(${p.id})" class="shelf-btn shelf-btn-primary shelf-btn-sm">Deploy</button>
        ${status === 'running'
          ? `<button onclick="stopProject(${p.id})" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Stop</button>`
          : `<button onclick="startProject(${p.id})" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Start</button>`}
        ${p.port ? `<a href="http://localhost:${p.port}" target="_blank" class="shelf-btn shelf-btn-ghost shelf-btn-sm">Open</a>` : ''}
        <button onclick="document.getElementById('edit-dialog').style.display='flex'" class="shelf-btn shelf-btn-secondary shelf-btn-sm">Edit</button>
        <button onclick="deleteProject(${p.id}, '${p.name}')" class="shelf-btn shelf-btn-ghost shelf-btn-sm" style="color:var(--danger);">Delete</button>
      </div>
    </div>

    <div style="display:grid; grid-template-columns:1fr 1fr; gap:16px; margin-bottom:16px;">
      <div class="shelf-card">
        <div style="font-size:13px; font-weight:600; margin-bottom:12px;">Configuration</div>
        <table style="font-size:13px; width:100%;">
          ${configRows.map(([k, v]) => `<tr><td style="color:var(--text-muted); padding:4px 12px 4px 0; white-space:nowrap;">${k}</td><td>${v}</td></tr>`).join('')}
        </table>
      </div>
      <div class="shelf-card">
        <div style="font-size:13px; font-weight:600; margin-bottom:12px;">Webhook (CI/CD)</div>
        ${p.source_type === 'git' ? `
          <div style="font-size:13px; color:var(--text-secondary); margin-bottom:8px;">Add this webhook to your GitHub repository (Settings &rarr; Webhooks):</div>
          <div style="font-family:var(--font-mono); font-size:12px; background:var(--bg-tertiary); padding:8px 12px; border-radius:var(--radius); margin-bottom:8px; word-break:break-all;">
            http://&lt;server-ip&gt;:${webhookPort}/hooks/${p.id}
          </div>
          <div style="font-size:13px; color:var(--text-secondary); margin-bottom:4px;">Secret:</div>
          <div style="display:flex; gap:8px; align-items:center;">
            <code style="font-size:12px; background:var(--bg-tertiary); padding:6px 12px; border-radius:var(--radius); flex:1; overflow:hidden; text-overflow:ellipsis;">${p.webhook_secret}</code>
            <button onclick="navigator.clipboard.writeText('${p.webhook_secret}')" class="shelf-btn shelf-btn-ghost shelf-btn-sm">Copy</button>
          </div>
          <div style="font-size:12px; color:var(--text-muted); margin-top:8px;">Content type: application/json · Event: push · Branch: ${p.branch}</div>
        ` : `
          <div style="font-size:13px; color:var(--text-muted);">Image 소스 앱은 Deploy 버튼으로 최신 이미지를 pull 합니다.</div>
        `}
      </div>
    </div>

    <div class="shelf-card" style="margin-bottom:16px;">
      <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:8px;">
        <div style="font-size:13px; font-weight:600;">Container logs</div>
        <button onclick="refreshLogs()" class="shelf-btn shelf-btn-ghost shelf-btn-sm">Refresh</button>
      </div>
      <pre id="runtime-logs" style="font-family:var(--font-mono); font-size:12px; background:var(--bg-tertiary); padding:12px; border-radius:var(--radius); max-height:280px; overflow:auto; white-space:pre-wrap; margin:0;">loading...</pre>
    </div>

    <div class="shelf-section-header"><h2 class="shelf-section-title">Deployments</h2></div>
    ${deployments.length ? `
      <div class="shelf-card" style="padding:0; overflow:hidden;">
        <table class="shelf-table">
          <thead><tr><th>Time</th><th>Commit</th><th>Message</th><th>Status</th><th>Trigger</th><th>Duration</th></tr></thead>
          <tbody>${deployRows}</tbody>
        </table>
      </div>
    ` : '<div style="text-align:center; padding:32px; color:var(--text-muted);">No deployments yet. Click Deploy to start.</div>'}

    <div id="log-dialog" style="display:none; position:fixed; inset:0; background:rgba(0,0,0,0.4); z-index:100; align-items:center; justify-content:center;">
      <div style="background:var(--bg); border:1px solid var(--border); border-radius:var(--radius-lg); padding:24px; width:100%; max-width:720px; max-height:80vh; display:flex; flex-direction:column; box-shadow:var(--shadow);">
        <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:12px;">
          <h3 style="font-size:15px; font-weight:600;">Deployment log</h3>
          <button onclick="document.getElementById('log-dialog').style.display='none'" class="shelf-btn shelf-btn-ghost shelf-btn-icon">&times;</button>
        </div>
        <pre id="deploy-log" style="font-family:var(--font-mono); font-size:12px; background:var(--bg-tertiary); padding:12px; border-radius:var(--radius); overflow:auto; white-space:pre-wrap; margin:0; flex:1;"></pre>
      </div>
    </div>

    ${editDialog(p)}

    <script>
      const PROJECT_ID = ${p.id};
      async function deployNow(id) {
        const btns = document.querySelectorAll('button'); btns.forEach(b => b.disabled = true);
        const res = await fetch('/api/deploy/projects/' + id + '/deploy', { method: 'POST' });
        const json = await res.json();
        if (!json.ok) alert('Deploy failed: ' + (json.error?.message || ''));
        location.reload();
      }
      async function startProject(id) { await fetch('/api/deploy/projects/' + id + '/start', { method: 'POST' }); location.reload(); }
      async function stopProject(id) { await fetch('/api/deploy/projects/' + id + '/stop', { method: 'POST' }); location.reload(); }
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
      refreshLogs();
      setInterval(refreshLogs, 5000);
    </script>
  `
}

function editDialog(p: Project): string {
  const esc = (s: string | null) => (s || '').replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;')
  return `
    <div id="edit-dialog" style="display:none; position:fixed; inset:0; background:rgba(0,0,0,0.4); z-index:100; align-items:center; justify-content:center;">
      <div style="background:var(--bg); border:1px solid var(--border); border-radius:var(--radius-lg); padding:24px; width:100%; max-width:520px; max-height:85vh; overflow:auto; box-shadow:var(--shadow);">
        <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:20px;">
          <h3 style="font-size:16px; font-weight:600;">Edit ${p.name}</h3>
          <button onclick="document.getElementById('edit-dialog').style.display='none'" class="shelf-btn shelf-btn-ghost shelf-btn-icon">&times;</button>
        </div>
        <form id="edit-form" style="display:flex; flex-direction:column; gap:14px;">
          ${p.source_type === 'git' ? `
            <div>
              <label style="${LABEL_STYLE}">Git repository URL</label>
              <input type="text" name="repo_url" value="${esc(p.repo_url)}" required style="${INPUT_STYLE}">
            </div>
            <div style="width:160px;">
              <label style="${LABEL_STYLE}">Branch</label>
              <input type="text" name="branch" value="${esc(p.branch)}" style="${INPUT_STYLE}">
            </div>
          ` : `
            <div>
              <label style="${LABEL_STYLE}">Docker image</label>
              <input type="text" name="image" value="${esc(p.image)}" required style="${INPUT_STYLE}">
            </div>
          `}
          <div style="display:flex; gap:12px;">
            <div style="flex:1;">
              <label style="${LABEL_STYLE}">Host port</label>
              <input type="number" name="port" value="${p.port ?? ''}" style="${INPUT_STYLE}">
            </div>
            <div style="flex:1;">
              <label style="${LABEL_STYLE}">Container port</label>
              <input type="number" name="container_port" value="${p.container_port ?? ''}" style="${INPUT_STYLE}">
            </div>
          </div>
          <div>
            <label style="${LABEL_STYLE}">Domain</label>
            <input type="text" name="domain" value="${esc(p.domain)}" style="${INPUT_STYLE}">
          </div>
          <div>
            <label style="${LABEL_STYLE}">Environment variables (KEY=VALUE per line)</label>
            <textarea name="env" rows="3" style="${INPUT_STYLE} font-family:var(--font-mono); font-size:12px; resize:vertical;">${esc(p.env)}</textarea>
          </div>
          <div>
            <label style="${LABEL_STYLE}">Volumes (host:container per line)</label>
            <textarea name="volumes" rows="2" style="${INPUT_STYLE} font-family:var(--font-mono); font-size:12px; resize:vertical;">${esc(p.volumes)}</textarea>
          </div>
          <label style="display:flex; align-items:center; gap:6px; font-size:13px; cursor:pointer;">
            <input type="checkbox" name="auto_deploy" ${p.auto_deploy ? 'checked' : ''}> Auto deploy on webhook (push)
          </label>
          <div style="font-size:12px; color:var(--text-muted); padding:8px 12px; background:var(--bg-tertiary); border-radius:var(--radius);">
            변경 사항은 다음 Deploy 때 컨테이너에 적용됩니다.
          </div>
          <div style="display:flex; gap:8px; justify-content:flex-end;">
            <button type="button" onclick="document.getElementById('edit-dialog').style.display='none'" class="shelf-btn shelf-btn-secondary">Cancel</button>
            <button type="submit" class="shelf-btn shelf-btn-primary">Save</button>
          </div>
        </form>
      </div>
    </div>
    <script>
      document.getElementById('edit-form')?.addEventListener('submit', async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        const obj = {
          ${p.source_type === 'git' ? `repo_url: fd.get('repo_url'), branch: fd.get('branch') || 'main',` : `image: fd.get('image'),`}
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
    </script>`
}

// --- All deployments ---

export function deploymentsPage(rows: Array<Deployment & { project_name: string | null }>): string {
  const tableRows = rows.map((d) => `
    <tr>
      <td style="font-size:12px; color:var(--text-muted);">${new Date(d.created_at * 1000).toLocaleString()}</td>
      <td><a href="/admin/deploy/projects/${d.project_id}" style="color:var(--accent); text-decoration:none;">${d.project_name || '(deleted)'}</a></td>
      <td><code style="font-size:12px;">${(d.commit_hash || '').slice(0, 7)}</code></td>
      <td style="max-width:280px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">${d.commit_message || '-'}</td>
      <td><span class="shelf-badge shelf-badge-${DEPLOY_STATUS_COLORS[d.status] || 'info'}">${d.status}</span></td>
      <td><span class="shelf-badge">${d.trigger_type}</span></td>
      <td style="font-size:12px; color:var(--text-muted);">${d.duration_ms ? Math.round(d.duration_ms / 1000) + 's' : '-'}</td>
    </tr>`).join('')

  return `
    <div class="shelf-section-header"><h2 class="shelf-section-title">All deployments</h2></div>
    ${rows.length ? `
      <div class="shelf-card" style="padding:0; overflow:hidden;">
        <table class="shelf-table">
          <thead><tr><th>Time</th><th>App</th><th>Commit</th><th>Message</th><th>Status</th><th>Trigger</th><th>Duration</th></tr></thead>
          <tbody>${tableRows}</tbody>
        </table>
      </div>
    ` : '<div style="text-align:center; padding:48px; color:var(--text-muted);">No deployments yet.</div>'}
  `
}
