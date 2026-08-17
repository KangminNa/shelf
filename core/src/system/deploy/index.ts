import { join } from 'node:path'
import { mkdirSync } from 'node:fs'
import { Hono } from 'hono'
import { AppDatabase } from '../../db/database.js'
import { Logger } from '../../services/log.js'
import type { EventBus } from '../../services/events.js'
import { DockerService } from '../docker.js'
import { ProjectRepository, DeploymentRepository } from './repositories.js'
import { ContainerManager } from './container-manager.js'
import { DeployPipeline } from './pipeline.js'
import { WebhookServer } from './webhook-server.js'
import { SelfDeployer } from './self-deployer.js'
import { DeployController } from './controller.js'

export type { Project, Deployment } from './repositories.js'

export class DeploySystem {
  readonly projects: ProjectRepository
  readonly deployments: DeploymentRepository
  readonly docker: DockerService
  readonly containers: ContainerManager
  readonly pipeline: DeployPipeline
  readonly selfDeployer: SelfDeployer
  private readonly webhook: WebhookServer
  private readonly controller: DeployController
  private readonly log = new Logger('deploy')

  constructor(events: EventBus) {
    const db = new AppDatabase('deploy', join(process.cwd(), 'core', 'migrations', 'deploy'))
    const reposDir = join(process.cwd(), 'data', 'deploy', 'repos')
    mkdirSync(reposDir, { recursive: true })

    this.projects = new ProjectRepository(db.raw)
    this.deployments = new DeploymentRepository(db.raw)

    this.docker = new DockerService(this.log.scope('docker'))
    this.containers = new ContainerManager(this.docker, events, this.log)
    this.pipeline = new DeployPipeline(reposDir, this.projects, this.deployments, this.containers, this.docker, events, this.log)
    this.selfDeployer = new SelfDeployer(this.docker, events, this.log.scope('self-deploy'))
    this.webhook = new WebhookServer(this.projects, this.pipeline, this.selfDeployer, this.log.scope('webhook'))
    this.controller = new DeployController(this.projects, this.deployments, this.containers, this.pipeline, this.webhook.port, events)

    this.webhook.start()
    if (this.selfDeployer.configured) this.log.info('self-deploy enabled (POST /hooks/self)')
    this.docker.available().then((ok) => {
      if (!ok) this.log.warn('Docker daemon not reachable — deploys will fail until Docker is running')
    })
    this.log.info('deploy system ready (docker runtime)')
  }

  get api(): Hono {
    return this.controller.api
  }

  get pages(): Hono {
    return this.controller.pages
  }

  get webhookRoutes(): Hono {
    const routes = new Hono()
    routes.post('/self', async (c) => {
      if (!this.selfDeployer.configured) {
        return c.json({ ok: false, error: 'Self-deploy not configured' }, 404)
      }
      const body = Buffer.from(await c.req.arrayBuffer())
      if (!this.selfDeployer.verify(body, c.req.header('x-hub-signature-256'))) {
        return c.json({ ok: false, error: 'Invalid signature' }, 401)
      }
      let payload: { ref?: string } = {}
      try { payload = JSON.parse(body.toString()) } catch {}
      if (!this.selfDeployer.matchesBranch(payload)) {
        return c.json({ ok: true, message: 'Ignoring push to other branch' })
      }
      this.selfDeployer.trigger()
      return c.json({ ok: true, message: 'Rebuilding Shelf...' }, 202)
    })
    return routes
  }

  async appSummaries(): Promise<Array<{ id: number; name: string; running: boolean; port: number | null }>> {
    return Promise.all(
      this.projects.allSorted().map(async (p) => ({
        id: p.id,
        name: p.name,
        running: (await this.containers.status(p)) === 'running',
        port: p.port,
      }))
    )
  }

  shutdown(): void {
    this.webhook.stop()
  }
}
