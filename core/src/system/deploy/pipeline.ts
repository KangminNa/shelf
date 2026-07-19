import { spawn } from 'node:child_process'
import { existsSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import type { Logger } from '../../services/log.js'
import type { EventBus } from '../../services/events.js'
import type { DockerService } from '../docker.js'
import { ContainerManager } from './container-manager.js'
import type { ProjectRepository, DeploymentRepository, Project } from './repositories.js'

export interface DeployResult {
  ok: boolean
  deploymentId: number
  error?: string
}

/**
 * 배포 파이프라인.
 *  - git 소스:  clone/pull → docker build → 컨테이너 재생성
 *  - image 소스: docker pull → 컨테이너 재생성
 * 성공 시 도메인이 있으면 프록시에 자동 등록. 전 과정 로그는 deployments에 저장.
 */
export class DeployPipeline {
  private static readonly GIT_TIMEOUT_MS = 5 * 60 * 1000
  private static readonly MAX_LOG_CHARS = 200_000

  private readonly inFlight = new Set<number>()

  constructor(
    private readonly reposDir: string,
    private readonly projects: ProjectRepository,
    private readonly deployments: DeploymentRepository,
    private readonly containers: ContainerManager,
    private readonly docker: DockerService,
    private readonly events: EventBus,
    private readonly log: Logger
  ) {}

  isDeploying(projectId: number): boolean {
    return this.inFlight.has(projectId)
  }

  async deploy(project: Project, trigger: 'manual' | 'webhook'): Promise<DeployResult> {
    if (this.inFlight.has(project.id)) {
      return { ok: false, deploymentId: 0, error: 'Deployment already in progress' }
    }
    this.inFlight.add(project.id)

    const start = Date.now()
    const deployment = this.deployments.create({ project_id: project.id, status: 'running', trigger_type: trigger })
    let fullLog = ''
    const append = (text: string) => {
      fullLog += text
      if (fullLog.length > DeployPipeline.MAX_LOG_CHARS) fullLog = fullLog.slice(-DeployPipeline.MAX_LOG_CHARS)
    }

    try {
      this.events.emit('deploy:started', { projectId: project.id, name: project.name, deploymentId: deployment.id })
      this.log.info(`deploying "${project.name}" (${trigger}, ${project.source_type})...`)

      if (project.source_type === 'image') {
        // 1a. 이미지 pull
        append(`\n$ docker pull ${project.image}\n`)
        append(await this.docker.pull(project.image))
      } else {
        // 1b. git clone/pull → 커밋 기록 → docker build
        const repoDir = join(this.reposDir, project.name)
        if (!existsSync(join(repoDir, '.git'))) {
          rmSync(repoDir, { recursive: true, force: true })
          await this.gitStep(append, `git clone --branch ${project.branch} --single-branch ${project.repo_url} ${JSON.stringify(repoDir)}`, this.reposDir)
        } else {
          await this.gitStep(append, `git fetch origin ${project.branch}`, repoDir)
          await this.gitStep(append, `git reset --hard origin/${project.branch}`, repoDir)
        }

        const commitInfo = await this.runShell('git log -1 --format=%H%n%s', repoDir)
        const [commitHash = '', commitMessage = ''] = commitInfo.output.trim().split('\n')
        this.deployments.update(deployment.id, { commit_hash: commitHash, commit_message: commitMessage })

        if (!existsSync(join(repoDir, 'Dockerfile'))) {
          throw new Error('Dockerfile not found in repository root. Every Shelf app needs a Dockerfile.')
        }

        const tag = ContainerManager.imageTag(project)
        append(`\n$ docker build -t ${tag} .\n`)
        append(await this.docker.build(tag, repoDir))
      }

      // 2. 컨테이너 재생성
      append(`\n$ docker run (recreate container shelf-${project.name})\n`)
      await this.containers.recreate(project)

      // 3. 도메인이 있으면 프록시 등록
      if (project.domain && project.port) {
        // APP_HOST: Shelf가 컨테이너로 돌 때는 host.docker.internal (compose에서 설정)
        this.events.emit('proxy:register-host', {
          domain: project.domain,
          target_host: process.env.APP_HOST || '127.0.0.1',
          target_port: project.port,
          description: `app: ${project.name}`,
        })
      }

      const duration = Date.now() - start
      this.deployments.update(deployment.id, { status: 'success', log: fullLog, duration_ms: duration })
      this.events.emit('deploy:succeeded', { projectId: project.id, name: project.name, deploymentId: deployment.id })
      this.log.info(`deployed "${project.name}" in ${Math.round(duration / 1000)}s`)
      return { ok: true, deploymentId: deployment.id }
    } catch (err: any) {
      append(`\n[error] ${err.message}`)
      this.deployments.update(deployment.id, { status: 'failed', log: fullLog, duration_ms: Date.now() - start })
      this.events.emit('deploy:failed', { projectId: project.id, name: project.name, deploymentId: deployment.id, error: err.message })
      this.log.error(`deploy failed for "${project.name}": ${err.message}`)
      return { ok: false, deploymentId: deployment.id, error: err.message }
    } finally {
      this.inFlight.delete(project.id)
    }
  }

  removeRepo(projectName: string): void {
    const dir = join(this.reposDir, projectName)
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true })
  }

  // --- 내부 ---

  private async gitStep(append: (s: string) => void, cmd: string, cwd: string): Promise<void> {
    append(`\n$ ${cmd}\n`)
    const result = await this.runShell(cmd, cwd)
    append(result.output)
    if (result.code !== 0) throw new Error(`git step failed with exit code ${result.code}`)
  }

  private runShell(cmd: string, cwd: string): Promise<{ code: number; output: string }> {
    return new Promise((resolve) => {
      const proc = spawn('sh', ['-c', cmd], { cwd })
      let output = ''
      const collect = (d: Buffer) => { output += d.toString() }
      proc.stdout?.on('data', collect)
      proc.stderr?.on('data', collect)
      const timer = setTimeout(() => {
        proc.kill('SIGKILL')
        output += `\n[timeout] command exceeded ${DeployPipeline.GIT_TIMEOUT_MS / 1000}s`
      }, DeployPipeline.GIT_TIMEOUT_MS)
      proc.on('close', (code) => {
        clearTimeout(timer)
        resolve({ code: code ?? 1, output })
      })
    })
  }
}
