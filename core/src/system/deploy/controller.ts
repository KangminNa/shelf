import { Hono } from 'hono'
import type { Context } from 'hono'
import * as crypto from 'node:crypto'
import type { EventBus } from '../../services/events.js'
import type { ProjectRepository, DeploymentRepository, Project } from './repositories.js'
import type { ContainerManager } from './container-manager.js'
import type { DeployPipeline } from './pipeline.js'
import { projectsPage, projectDetailPage, deploymentsPage, type DisplayStatus } from './views.js'

const PROJECT_FIELDS = ['source_type', 'repo_url', 'branch', 'image', 'port', 'container_port', 'env', 'volumes', 'domain', 'auto_deploy'] as const

/**
 * 앱(컨테이너) 관리 HTTP 인터페이스.
 * - api: /api/deploy/* (JSON)
 * - pages: /admin/deploy/* (셸 래핑되는 HTML 조각)
 */
export class DeployController {
  readonly api = new Hono()
  readonly pages = new Hono()

  constructor(
    private readonly projects: ProjectRepository,
    private readonly deployments: DeploymentRepository,
    private readonly containers: ContainerManager,
    private readonly pipeline: DeployPipeline,
    private readonly webhookPort: number,
    private readonly events: EventBus
  ) {
    this.registerProjectApi()
    this.registerDeploymentApi()
    this.registerPages()
  }

  private async displayStatus(project: Project): Promise<DisplayStatus> {
    if (this.pipeline.isDeploying(project.id)) return 'deploying'
    const status = await this.containers.status(project)
    return status === 'none' ? 'stopped' : status
  }

  // --- /api/deploy/projects ---

  private registerProjectApi(): void {
    this.api.get('/projects', async (c) => {
      const data = await Promise.all(
        this.projects.allSorted().map(async (p) => ({
          ...p,
          webhook_secret: undefined,
          status: await this.displayStatus(p),
        }))
      )
      return c.json({ ok: true, data })
    })

    this.api.post('/projects', async (c) => {
      const body = await c.req.json()
      if (!body.name || !/^[a-z0-9-_]+$/i.test(body.name)) {
        return this.badRequest(c, 'name is required (alphanumeric with dashes/underscores)')
      }
      const sourceType = body.source_type === 'image' ? 'image' : 'git'
      if (sourceType === 'git' && !body.repo_url) return this.badRequest(c, 'repo_url is required for git source')
      if (sourceType === 'image' && !body.image) return this.badRequest(c, 'image is required for image source')
      if (this.projects.findByName(body.name)) {
        return c.json({ ok: false, error: { code: 'CONFLICT', message: `App "${body.name}" already exists` } }, 409)
      }

      const project = this.projects.create({
        name: body.name,
        source_type: sourceType,
        repo_url: body.repo_url || '',
        branch: body.branch || 'main',
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
      return c.json({ ok: true, data: { id: project.id, name: project.name, webhook_secret: project.webhook_secret } }, 201)
    })

    this.api.get('/projects/:id', async (c) => {
      const project = this.projects.find(c.req.param('id'))
      if (!project) return this.notFound(c)
      return c.json({ ok: true, data: { ...project, status: await this.displayStatus(project) } })
    })

    this.api.patch('/projects/:id', async (c) => {
      const body = await c.req.json()
      const patch: Partial<Project> = {}
      for (const field of PROJECT_FIELDS) {
        if (body[field] !== undefined) {
          ;(patch as any)[field] = typeof body[field] === 'boolean' ? (body[field] ? 1 : 0) : body[field]
        }
      }
      if (!Object.keys(patch).length) return this.badRequest(c, 'No fields to update')
      const project = this.projects.update(c.req.param('id'), patch)
      if (!project) return this.notFound(c)
      return c.json({ ok: true, data: project })
    })

    this.api.delete('/projects/:id', async (c) => {
      const id = Number(c.req.param('id'))
      const project = this.projects.find(id)
      if (project) {
        await this.containers.remove(project)
        this.pipeline.removeRepo(project.name)
        this.deployments.deleteForProject(id)
        this.projects.delete(id)
        this.events.emit('deploy:project-deleted', { id, name: project.name })
      }
      return c.json({ ok: true, data: null })
    })

    this.api.post('/projects/:id/deploy', async (c) => {
      const project = this.projects.find(c.req.param('id'))
      if (!project) return this.notFound(c)
      const result = await this.pipeline.deploy(project, 'manual')
      if (!result.ok) {
        return c.json({ ok: false, error: { code: 'DEPLOY_FAILED', message: result.error }, data: { deploymentId: result.deploymentId } }, 500)
      }
      return c.json({ ok: true, data: { deploymentId: result.deploymentId } })
    })

    this.api.post('/projects/:id/start', async (c) => {
      const project = this.projects.find(c.req.param('id'))
      if (!project) return this.notFound(c)
      const result = await this.containers.start(project)
      if (!result.ok) return c.json({ ok: false, error: { code: 'START_FAILED', message: result.error } }, 400)
      return c.json({ ok: true, data: { status: 'running' } })
    })

    this.api.post('/projects/:id/stop', async (c) => {
      const project = this.projects.find(c.req.param('id'))
      if (!project) return this.notFound(c)
      await this.containers.stop(project)
      return c.json({ ok: true, data: { status: 'stopped' } })
    })

    this.api.get('/projects/:id/logs', async (c) => {
      const project = this.projects.find(c.req.param('id'))
      if (!project) return this.notFound(c)
      const status = await this.containers.status(project)
      return c.json({ ok: true, data: { logs: await this.containers.logs(project), status } })
    })

    this.api.get('/projects/:id/webhook', (c) => {
      const project = this.projects.find(c.req.param('id'))
      if (!project) return this.notFound(c)
      return c.json({ ok: true, data: { path: `/hooks/${project.id}`, port: this.webhookPort, secret: project.webhook_secret } })
    })
  }

  // --- /api/deploy/deployments ---

  private registerDeploymentApi(): void {
    this.api.get('/deployments', (c) => {
      const limit = Number(c.req.query('limit') || 50)
      return c.json({ ok: true, data: this.withProjectNames(this.deployments.recent(limit)) })
    })

    this.api.get('/deployments/:id', (c) => {
      const deployment = this.deployments.find(c.req.param('id'))
      if (!deployment) return c.json({ ok: false, error: { code: 'NOT_FOUND', message: 'Deployment not found' } }, 404)
      return c.json({ ok: true, data: deployment })
    })
  }

  // --- /admin/deploy pages ---

  private registerPages(): void {
    this.pages.get('/', async (c) => {
      const items = await Promise.all(
        this.projects.allSorted().map(async (project) => ({
          project,
          status: await this.displayStatus(project),
          lastDeploy: this.deployments.latestFor(project.id),
        }))
      )
      return c.html(projectsPage(items, this.webhookPort))
    })

    this.pages.get('/projects/:id', async (c) => {
      const project = this.projects.find(c.req.param('id'))
      if (!project) {
        return c.html('<div style="padding:48px; text-align:center; color:var(--text-muted);">App not found. <a href="/admin/deploy">Back</a></div>')
      }
      return c.html(projectDetailPage(project, await this.displayStatus(project), this.deployments.forProject(project.id), this.webhookPort))
    })

    this.pages.get('/deployments', (c) => {
      return c.html(deploymentsPage(this.withProjectNames(this.deployments.recent(100))))
    })
  }

  // --- 헬퍼 ---

  private withProjectNames<T extends { project_id: number }>(rows: T[]): Array<T & { project_name: string | null }> {
    const names = new Map(this.projects.all().map((p) => [p.id, p.name]))
    return rows.map((r) => ({ ...r, project_name: names.get(r.project_id) ?? null }))
  }

  private notFound(c: Context) {
    return c.json({ ok: false, error: { code: 'NOT_FOUND', message: 'App not found' } }, 404)
  }

  private badRequest(c: Context, message: string) {
    return c.json({ ok: false, error: { code: 'VALIDATION', message } }, 400)
  }
}
