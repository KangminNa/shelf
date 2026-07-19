import type Database from 'better-sqlite3'
import { Repository } from '../../db/repository.js'

/** 앱 = Docker 컨테이너로 실행되는 배포 단위 */
export interface Project {
  id: number
  name: string
  source_type: 'git' | 'image'
  repo_url: string
  branch: string
  image: string
  port: number | null // 호스트 포트 (프록시가 바라보는 포트)
  container_port: number | null // 컨테이너 내부 포트
  env: string // KEY=VALUE 줄 단위
  volumes: string // host:container 줄 단위
  domain: string
  webhook_secret: string
  auto_deploy: number
  // 레거시 (process 런타임 시절) — UI에서 미사용
  install_cmd: string
  build_cmd: string
  start_cmd: string
  created_at: number
  updated_at: number
}

export interface Deployment {
  id: number
  project_id: number
  commit_hash: string
  commit_message: string
  status: 'pending' | 'running' | 'success' | 'failed'
  trigger_type: 'manual' | 'webhook'
  log: string
  duration_ms: number
  created_at: number
}

export class ProjectRepository extends Repository<Project> {
  constructor(db: Database.Database) {
    super(db, 'projects')
  }

  allSorted(): Project[] {
    return this.query().orderBy('name').all()
  }

  findByName(name: string): Project | undefined {
    return this.findBy({ name } as Partial<Project>)
  }
}

export class DeploymentRepository extends Repository<Deployment> {
  constructor(db: Database.Database) {
    super(db, 'deployments')
  }

  forProject(projectId: number, limit = 20): Deployment[] {
    return this.query().where('project_id', projectId).orderBy('created_at', 'desc').limit(limit).all()
  }

  latestFor(projectId: number): Deployment | undefined {
    return this.forProject(projectId, 1)[0]
  }

  recent(limit = 100): Deployment[] {
    return this.query().orderBy('created_at', 'desc').limit(limit).all()
  }

  deleteForProject(projectId: number): void {
    this.query().where('project_id', projectId).delete()
  }
}
