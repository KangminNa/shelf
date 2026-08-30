import * as crypto from 'node:crypto'
import { Controller, fields, notFound, invalid, conflict, failed } from '../../kernel/controller.js'
import type { EventBus } from '../../services/events.js'
import type { Project, Deployment } from './repositories.js'
import type { DeploySystem } from './index.js'
import { ContainerManager } from './container-manager.js'
import { ProjectsPage, ProjectDetailPage, DeploymentsPage, type DisplayStatus } from './views.js'

const PROJECT_FIELDS = ['source_type', 'repo_url', 'branch', 'git_token', 'image', 'port', 'container_port', 'env', 'volumes', 'domain', 'auto_deploy'] as const

const PATTERNS: Array<[string, RegExp, string]> = [
  ['branch', /^[\w./-]{1,120}$/, 'Invalid branch name'],
  ['repo_url', /^(https?:\/\/|git@|ssh:\/\/|\/|\.\/)[^\s;|&`$<>'"\\]+$/, 'Invalid repository URL'],
  ['image', /^[\w][\w.\-/:@]{0,200}$/, 'Invalid image reference'],
]

function validateSource(body: Record<string, any>): void {
  for (const [field, pattern, message] of PATTERNS) {
    const value = body[field]
    if (value !== undefined && value !== '' && !pattern.test(String(value))) invalid(message)
  }
}

function sanitize(p: Project): Omit<Project, 'webhook_secret' | 'git_token'> & { has_token: boolean } {
  const { webhook_secret: _s, git_token, ...rest } = p
  return { ...rest, has_token: !!git_token }
}

export class DeployController extends Controller {
  constructor(
    private readonly deploy: DeploySystem,
    private readonly events: EventBus
  ) {
    super(deploy.logger)
    this.registerProjectApi()
    this.registerDeploymentApi()
    this.registerPages()
  }

  private project(id: string | number): Project {
    return this.deploy.projects.find(id) ?? notFound('App')
  }

  private deployment(id: string): Deployment {
    return this.deploy.deployments.find(id) ?? notFound('Deployment')
  }

  private async displayStatus(project: Project): Promise<DisplayStatus> {
    if (this.deploy.pipeline.isDeploying(project.id)) return 'deploying'
    const status = await this.deploy.containers.status(project)
    return status === 'none' ? 'stopped' : status
  }

  private async runPipeline(project: Project, trigger: 'manual' | 'rollback', commit?: string) {
    const result = await this.deploy.pipeline.deploy(project, trigger, commit)
    if (!result.ok) failed(`${trigger === 'rollback' ? 'ROLLBACK' : 'DEPLOY'}_FAILED`, result.error || 'deploy failed', { deploymentId: result.deploymentId })
    return { deploymentId: result.deploymentId, commit }
  }

  private registerProjectApi(): void {
    this.get('/projects', () =>
      Promise.all(
        this.deploy.projects.allSorted().map(async (p) => ({ ...sanitize(p), status: await this.displayStatus(p) }))
      )
    )

    this.post('/projects', ({ body }) => {
      if (!body.name || !/^[a-z0-9-_]+$/i.test(body.name)) invalid('name is required (alphanumeric with dashes/underscores)')
      const sourceType = body.source_type === 'image' ? 'image' : 'git'
      if (sourceType === 'git' && !body.repo_url) invalid('repo_url is required for git source')
      if (sourceType === 'image' && !body.image) invalid('image is required for image source')
      validateSource(body)
      if (this.deploy.projects.findByName(body.name)) conflict(`App "${body.name}" already exists`)

      const project = this.deploy.projects.create({
        name: body.name,
        source_type: sourceType,
        repo_url: body.repo_url || '',
        branch: body.branch || 'main',
        git_token: body.git_token || '',
        image: body.image || '',
        port: body.port || null,
        container_port: body.container_port || null,
        env: body.env || '',
        volumes: body.volumes || '',
        domain: body.domain || '',
        webhook_secret: crypto.randomBytes(24).toString('hex'),
        auto_deploy: body.auto_deploy === false ? 0 : 1,
      })
      this.events.emit('deploy:project-created', { id: project.id, name: project.name })
      return { id: project.id, name: project.name, webhook_secret: project.webhook_secret }
    }, 201)

    this.get('/projects/:id', async (req) => {
      const project = this.project(req.id)
      return { ...sanitize(project), status: await this.displayStatus(project) }
    })

    this.patch('/projects/:id', (req) => {
      validateSource(req.body)
      const patch = fields<Project>(req.body, PROJECT_FIELDS)
      if (!Object.keys(patch).length) invalid('No fields to update')
      return sanitize(this.deploy.projects.update(req.id, patch) ?? notFound('App'))
    })

    this.delete('/projects/:id', async (req) => {
      const project = this.deploy.projects.find(req.id)
      if (!project) return null
      await this.deploy.containers.remove(project)
      this.deploy.pipeline.removeRepo(project.name)
      this.deploy.deployments.deleteForProject(project.id)
      this.deploy.projects.delete(project.id)
      this.events.emit('proxy:release-target', { target_host: ContainerManager.containerName(project) })
      this.events.emit('deploy:project-deleted', { id: project.id, name: project.name })
      return null
    })

    this.post('/projects/:id/deploy', (req) => this.runPipeline(this.project(req.id), 'manual'))

    this.post('/projects/:id/start', async (req) => {
      const result = await this.deploy.containers.start(this.project(req.id))
      if (!result.ok) failed('START_FAILED', result.error || 'start failed')
      return { status: 'running' }
    })

    this.post('/projects/:id/stop', async (req) => {
      await this.deploy.containers.stop(this.project(req.id))
      return { status: 'stopped' }
    })

    this.get('/projects/:id/logs', async (req) => {
      const project = this.project(req.id)
      return { logs: await this.deploy.containers.logs(project), status: await this.deploy.containers.status(project) }
    })

    this.get('/projects/:id/webhook', (req) => {
      const project = this.project(req.id)
      return { path: `/hooks/${project.id}`, port: this.deploy.webhook.port, secret: project.webhook_secret }
    })
  }

  private registerDeploymentApi(): void {
    this.get('/deployments', (req) => this.withProjectNames(this.deploy.deployments.recent(req.number('limit', 50))))

    this.get('/deployments/:id', (req) => this.deployment(req.id))

    this.post('/deployments/:id/rollback', (req) => {
      const deployment = this.deployment(req.id)
      const project = this.project(deployment.project_id)
      if (project.source_type !== 'git' || !deployment.commit_hash) {
        invalid('Rollback is only available for git-sourced deployments with a recorded commit')
      }
      return this.runPipeline(project, 'rollback', deployment.commit_hash)
    })
  }

  private registerPages(): void {
    this.page('/', async () => {
      const items = await Promise.all(
        this.deploy.projects.allSorted().map(async (project) => ({
          project,
          status: await this.displayStatus(project),
          lastDeploy: this.deploy.deployments.latestFor(project.id),
        }))
      )
      return new ProjectsPage({ items, webhookBase: this.deploy.publicAddress.urlFor('/hooks') }).render()
    })

    this.page('/projects/:id', async (req) => {
      const project = this.deploy.projects.find(req.id)
      if (!project) return '<div style="padding:48px; text-align:center; color:var(--text-muted);">App not found. <a href="/admin/deploy">Back</a></div>'
      const target = ContainerManager.proxyTarget(project)
      return new ProjectDetailPage({
        project,
        status: await this.displayStatus(project),
        deployments: this.deploy.deployments.forProject(project.id),
        webhookUrl: this.deploy.publicAddress.urlFor(`/hooks/${project.id}`),
        webhookSecure: this.deploy.publicAddress.secure,
        container: ContainerManager.containerName(project),
        proxyTarget: target ? `${target.host}:${target.port}` : null,
      }).render()
    })

    this.page('/deployments', () =>
      new DeploymentsPage({ rows: this.withProjectNames(this.deploy.deployments.recent(100)) }).render()
    )
  }

  private withProjectNames<T extends { project_id: number }>(rows: T[]): Array<T & { project_name: string | null }> {
    const names = new Map(this.deploy.projects.all().map((p) => [p.id, p.name]))
    return rows.map((r) => ({ ...r, project_name: names.get(r.project_id) ?? null }))
  }
}
