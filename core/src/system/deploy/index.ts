import { join } from 'node:path'
import { mkdirSync } from 'node:fs'
import type { Hono } from 'hono'
import { AppDatabase } from '../../db/database.js'
import { Logger } from '../../services/log.js'
import type { EventBus } from '../../services/events.js'
import { DockerService } from '../docker.js'
import { ProjectRepository, DeploymentRepository } from './repositories.js'
import { ContainerManager } from './container-manager.js'
import { DeployPipeline } from './pipeline.js'
import { WebhookServer } from './webhook-server.js'
import { DeployController } from './controller.js'

export type { Project, Deployment } from './repositories.js'

export class DeploySystem {
  readonly projects: ProjectRepository
  readonly deployments: DeploymentRepository
  readonly docker: DockerService
  readonly containers: ContainerManager
  readonly pipeline: DeployPipeline
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
    this.webhook = new WebhookServer(this.projects, this.pipeline, this.log.scope('webhook'))
    this.controller = new DeployController(this.projects, this.deployments, this.containers, this.pipeline, this.webhook.port, events)

    this.webhook.start()
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
