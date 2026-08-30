import { join } from 'node:path'
import { mkdirSync } from 'node:fs'
import { Hono } from 'hono'
import { AppDatabase } from '../../db/database.js'
import { Logger } from '../../services/log.js'
import type { EventBus } from '../../services/events.js'
import type { PublicAddress } from '../../kernel/public-address.js'
import { DockerService } from '../docker.js'
import { ProjectRepository, DeploymentRepository } from './repositories.js'
import { ContainerManager } from './container-manager.js'
import { DeployPipeline } from './pipeline.js'
import { WebhookServer } from './webhook-server.js'
import { WebhookHandler } from './webhook-handler.js'
import { SelfDeployer } from './self-deployer.js'
import { AppWatcher } from './app-watcher.js'
import { UsageSampler } from './usage-sampler.js'
import { Scheduler } from '../../services/scheduler.js'
import { DeployController } from './controller.js'

export type { Project, Deployment } from './repositories.js'

export class DeploySystem {
  readonly projects: ProjectRepository
  readonly deployments: DeploymentRepository
  readonly docker: DockerService
  readonly containers: ContainerManager
  readonly pipeline: DeployPipeline
  readonly selfDeployer: SelfDeployer
  readonly webhook: WebhookServer
  readonly watcher: AppWatcher
  readonly usage: UsageSampler
  private readonly hooks: WebhookHandler
  private readonly scheduler = new Scheduler('deploy')
  private readonly controller: DeployController
  readonly logger = new Logger('deploy')
  private readonly log = this.logger

  constructor(
    events: EventBus,
    readonly publicAddress: PublicAddress
  ) {
    const db = new AppDatabase('deploy', join(process.cwd(), 'core', 'migrations', 'deploy'))
    const reposDir = join(process.cwd(), 'data', 'deploy', 'repos')
    mkdirSync(reposDir, { recursive: true })

    this.projects = new ProjectRepository(db.raw)
    this.deployments = new DeploymentRepository(db.raw)

    this.docker = new DockerService(this.log.scope('docker'))
    this.containers = new ContainerManager(this.docker, events, this.log)
    this.pipeline = new DeployPipeline(reposDir, this.projects, this.deployments, this.containers, this.docker, events, this.log)
    this.selfDeployer = new SelfDeployer(this.docker, events, this.log.scope('self-deploy'))
    this.hooks = new WebhookHandler(this.projects, this.pipeline, this.selfDeployer, this.log.scope('webhook'))
    this.webhook = new WebhookServer(this.hooks, this.log.scope('webhook'))
    this.controller = new DeployController(this, events)

    this.usage = new UsageSampler(this.docker)
    this.scheduler.register('* * * * *', 'usage-sample', () => this.usage.refresh())
    this.usage.refresh().catch(() => {})
    this.watcher = new AppWatcher(this.projects, this.containers, events, this.log.scope('watch'))
    this.scheduler.register('* * * * *', 'app-health', () => this.watcher.check())

    this.webhook.start()
    this.containers.attachExistingContainers().catch((err) => this.log.warn(`docker network setup failed: ${err.message}`))
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
    routes.post('/:hook', async (c) => {
      const body = Buffer.from(await c.req.arrayBuffer())
      const reply = this.hooks.handle({
        path: `/${c.req.param('hook')}`,
        body,
        signature: c.req.header('x-hub-signature-256'),
        gitlabToken: c.req.header('x-gitlab-token'),
        secretParam: c.req.query('secret'),
      })
      return c.json(reply.body, reply.status as never)
    })
    return routes
  }

  appUsage(): Array<{ name: string; cpu: number | null; memory: number | null }> {
    return this.projects.allSorted().map((project) => ({
      name: project.name,
      ...this.usage.of(ContainerManager.containerName(project)),
    }))
  }

  async appSummaries(): Promise<Array<{ id: number; name: string; running: boolean; port: number | null }>> {
    const projects = this.projects.allSorted()
    const statuses = await this.containers.statuses(projects)
    return projects.map((project) => ({
      id: project.id,
      name: project.name,
      running: statuses.get(project.id) === 'running',
      port: project.port,
    }))
  }

  shutdown(): void {
    this.scheduler.stopAll()
    this.webhook.stop()
  }
}
