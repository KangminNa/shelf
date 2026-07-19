import type { DockerService, ContainerStatus } from '../docker.js'
import type { EventBus } from '../../services/events.js'
import type { Logger } from '../../services/log.js'
import type { Project } from './repositories.js'

/**
 * 앱 컨테이너 생명주기 관리.
 * 컨테이너 이름: shelf-{app}, git 소스 빌드 이미지: shelf-app-{app}
 */
export class ContainerManager {
  constructor(
    private readonly docker: DockerService,
    private readonly events: EventBus,
    private readonly log: Logger
  ) {}

  static containerName(project: Project): string {
    return `shelf-${project.name}`
  }

  static imageTag(project: Project): string {
    return project.source_type === 'image' ? project.image : `shelf-app-${project.name}`
  }

  /** 컨테이너 (재)생성 후 실행 */
  async recreate(project: Project): Promise<void> {
    await this.docker.runContainer({
      name: ContainerManager.containerName(project),
      image: ContainerManager.imageTag(project),
      hostPort: project.port,
      containerPort: project.container_port,
      env: ContainerManager.parseLines(project.env, '='),
      volumes: (project.volumes || '').split('\n').map((v) => v.trim()).filter(Boolean),
    })
    this.events.emit('deploy:container-started', { projectId: project.id, name: project.name })
  }

  async start(project: Project): Promise<{ ok: boolean; error?: string }> {
    const name = ContainerManager.containerName(project)
    try {
      if ((await this.docker.status(name)) === 'none') {
        await this.recreate(project)
      } else {
        await this.docker.startContainer(name)
      }
      return { ok: true }
    } catch (err: any) {
      return { ok: false, error: err.message }
    }
  }

  async stop(project: Project): Promise<void> {
    await this.docker.stopContainer(ContainerManager.containerName(project))
  }

  /** 컨테이너 + 빌드 이미지 제거 (앱 삭제 시) */
  async remove(project: Project): Promise<void> {
    const name = ContainerManager.containerName(project)
    await this.docker.removeContainer(name).catch(() => {})
    if (project.source_type === 'git') {
      await this.docker.removeImage(ContainerManager.imageTag(project))
    }
    this.log.info(`removed container ${name}`)
  }

  async status(project: Project): Promise<ContainerStatus> {
    return this.docker.status(ContainerManager.containerName(project))
  }

  async logs(project: Project, tail = 200): Promise<string[]> {
    return this.docker.logs(ContainerManager.containerName(project), tail)
  }

  private static parseLines(text: string, sep: string): Record<string, string> {
    const result: Record<string, string> = {}
    for (const line of (text || '').split('\n')) {
      const idx = line.indexOf(sep)
      if (idx > 0 && !line.trimStart().startsWith('#')) {
        result[line.slice(0, idx).trim()] = line.slice(idx + 1).trim()
      }
    }
    return result
  }
}
