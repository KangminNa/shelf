import { join } from 'node:path'
import { mkdirSync } from 'node:fs'
import type { Hono } from 'hono'
import { createModuleDB } from '../../services/db.js'
import { Logger } from '../../services/log.js'
import type { EventBus } from '../../services/events.js'
import { DockerService } from '../docker.js'
import { ProjectRepository, DeploymentRepository } from './repositories.js'
import { ContainerManager } from './container-manager.js'
import { DeployPipeline } from './pipeline.js'
import { WebhookServer } from './webhook-server.js'
import { DeployController } from './controller.js'

export type { Project, Deployment } from './repositories.js'

/**
 * 코어 내장 앱 배포 시스템 — 앱 = Docker 컨테이너.
 *
 *   DeploySystem
 *   ├── ProjectRepository / DeploymentRepository  (데이터)
 *   ├── DockerService     — docker CLI 래퍼
 *   ├── ContainerManager  — 컨테이너 생명주기 (shelf-{app})
 *   ├── DeployPipeline    — git pull → docker build → 컨테이너 재생성 (또는 image pull)
 *   ├── WebhookServer     — :9100, GitHub/GitLab push → 자동 배포
 *   └── DeployController  — /api/deploy + /admin/deploy 라우트
 */
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
    const db = createModuleDB('deploy', join(process.cwd(), 'core', 'migrations', 'deploy'))
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

  /** 사이드바 등에서 쓰는 앱 요약 */
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
    // 컨테이너는 docker --restart 정책이 관리하므로 종료 시 건드리지 않는다
  }
}
