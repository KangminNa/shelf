import { spawn } from 'node:child_process'
import { existsSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import type { Logger } from '../../services/log.js'
import type { EventBus } from '../../services/events.js'
import type { DockerService } from '../docker.js'
import { ContainerManager } from './container-manager.js'
import type { ProjectRepository, DeploymentRepository, Project, Deployment } from './repositories.js'

export interface DeployResult {
  ok: boolean
  deploymentId: number
  error?: string
}

export type DeployTrigger = Deployment['trigger_type']

const GIT_TIMEOUT_MS = 5 * 60 * 1000
const MAX_JOURNAL_CHARS = 200_000

class DeploymentJournal {
  private text = ''

  constructor(private readonly secretToMask: string) {}

  add(chunk: string): void {
    const masked = this.secretToMask ? chunk.split(this.secretToMask).join('***') : chunk
    this.text = (this.text + masked).slice(-MAX_JOURNAL_CHARS)
  }

  command(cmd: string): void {
    this.add(`\n$ ${cmd}\n`)
  }

  toString(): string {
    return this.text
  }
}

export class DeployPipeline {
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

  async deploy(project: Project, trigger: DeployTrigger, rollbackCommit?: string): Promise<DeployResult> {
    if (this.inFlight.has(project.id)) {
      return { ok: false, deploymentId: 0, error: 'Deployment already in progress' }
    }
    this.inFlight.add(project.id)

    const startedAt = Date.now()
    const journal = new DeploymentJournal(project.git_token)
    const deployment = this.deployments.create({ project_id: project.id, status: 'running', trigger_type: trigger })

    try {
      this.events.emit('deploy:started', { projectId: project.id, name: project.name, deploymentId: deployment.id })
      this.log.info(`deploying "${project.name}" (${trigger}, ${project.source_type})...`)

      if (project.source_type === 'image') {
        await this.pullImage(project, journal)
      } else {
        await this.buildFromGit(project, deployment.id, journal, rollbackCommit)
      }
      await this.recreateContainer(project, journal)
      this.publishDomain(project)

      return this.succeed(project, deployment, journal, startedAt)
    } catch (err: any) {
      return this.fail(project, deployment, journal, startedAt, err)
    } finally {
      this.inFlight.delete(project.id)
    }
  }

  removeRepo(projectName: string): void {
    const dir = join(this.reposDir, projectName)
    if (existsSync(dir)) rmSync(dir, { recursive: true, force: true })
  }

  static authenticatedUrl(project: Project): string {
    if (!project.git_token || !/^https?:\/\//.test(project.repo_url)) return project.repo_url
    return project.repo_url.replace(/^(https?:\/\/)/, `$1x-access-token:${project.git_token}@`)
  }

  private async pullImage(project: Project, journal: DeploymentJournal): Promise<void> {
    journal.command(`docker pull ${project.image}`)
    journal.add(await this.docker.pull(project.image))
  }

  private async buildFromGit(
    project: Project,
    deploymentId: number,
    journal: DeploymentJournal,
    rollbackCommit?: string
  ): Promise<void> {
    const repoDir = join(this.reposDir, project.name)
    await this.syncRepository(project, repoDir, journal, rollbackCommit)
    await this.recordCommit(deploymentId, repoDir)
    this.ensureDockerfile(repoDir)
    await this.buildImage(project, repoDir, journal)
  }

  private async syncRepository(
    project: Project,
    repoDir: string,
    journal: DeploymentJournal,
    rollbackCommit?: string
  ): Promise<void> {
    const fetchUrl = DeployPipeline.authenticatedUrl(project)

    if (!existsSync(join(repoDir, '.git'))) {
      rmSync(repoDir, { recursive: true, force: true })
      await this.gitStep(journal, `git clone --branch ${project.branch} --single-branch ${fetchUrl} ${JSON.stringify(repoDir)}`, this.reposDir)
      await this.stripCredentialsFromRemote(project, repoDir)
    } else {
      await this.gitStep(journal, `git fetch ${fetchUrl} ${project.branch}`, repoDir)
    }

    if (rollbackCommit) {
      await this.gitStep(journal, `git reset --hard ${rollbackCommit}`, repoDir)
    } else if (existsSync(join(repoDir, '.git', 'FETCH_HEAD'))) {
      await this.gitStep(journal, `git reset --hard FETCH_HEAD`, repoDir)
    }
  }

  private async stripCredentialsFromRemote(project: Project, repoDir: string): Promise<void> {
    if (project.git_token) {
      await this.runShell(`git remote set-url origin ${project.repo_url}`, repoDir)
    }
  }

  private async recordCommit(deploymentId: number, repoDir: string): Promise<void> {
    const result = await this.runShell('git log -1 --format=%H%n%s', repoDir)
    const [commitHash = '', commitMessage = ''] = result.output.trim().split('\n')
    this.deployments.update(deploymentId, { commit_hash: commitHash, commit_message: commitMessage })
  }

  private ensureDockerfile(repoDir: string): void {
    if (!existsSync(join(repoDir, 'Dockerfile'))) {
      throw new Error('Dockerfile not found in repository root. Every Shelf app needs a Dockerfile.')
    }
  }

  private async buildImage(project: Project, repoDir: string, journal: DeploymentJournal): Promise<void> {
    const tag = ContainerManager.imageTag(project)
    journal.command(`docker build -t ${tag} .`)
    journal.add(await this.docker.build(tag, repoDir))
  }

  private async recreateContainer(project: Project, journal: DeploymentJournal): Promise<void> {
    journal.command(`docker run (recreate container shelf-${project.name})`)
    await this.containers.recreate(project)
  }

  private publishDomain(project: Project): void {
    if (!project.domain || !project.port) return
    this.events.emit('proxy:register-host', {
      domain: project.domain,
      target_host: process.env.APP_HOST || '127.0.0.1',
      target_port: project.port,
      description: `app: ${project.name}`,
    })
  }

  private succeed(project: Project, deployment: Deployment, journal: DeploymentJournal, startedAt: number): DeployResult {
    const duration = Date.now() - startedAt
    this.deployments.update(deployment.id, { status: 'success', log: journal.toString(), duration_ms: duration })
    this.events.emit('deploy:succeeded', { projectId: project.id, name: project.name, deploymentId: deployment.id })
    this.log.info(`deployed "${project.name}" in ${Math.round(duration / 1000)}s`)
    return { ok: true, deploymentId: deployment.id }
  }

  private fail(project: Project, deployment: Deployment, journal: DeploymentJournal, startedAt: number, err: Error): DeployResult {
    journal.add(`\n[error] ${err.message}`)
    this.deployments.update(deployment.id, { status: 'failed', log: journal.toString(), duration_ms: Date.now() - startedAt })
    this.events.emit('deploy:failed', { projectId: project.id, name: project.name, deploymentId: deployment.id, error: err.message })
    this.log.error(`deploy failed for "${project.name}": ${err.message}`)
    return { ok: false, deploymentId: deployment.id, error: err.message }
  }

  private async gitStep(journal: DeploymentJournal, cmd: string, cwd: string): Promise<void> {
    journal.command(cmd)
    const result = await this.runShell(cmd, cwd)
    journal.add(result.output)
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
        output += `\n[timeout] command exceeded ${GIT_TIMEOUT_MS / 1000}s`
      }, GIT_TIMEOUT_MS)
      proc.on('close', (code) => {
        clearTimeout(timer)
        resolve({ code: code ?? 1, output })
      })
    })
  }
}
