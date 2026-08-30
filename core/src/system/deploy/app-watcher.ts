import type { EventBus } from '../../services/events.js'
import type { Logger } from '../../services/log.js'
import type { ProjectRepository, Project } from './repositories.js'
import type { ContainerManager } from './container-manager.js'

type Liveness = 'up' | 'down'

export class AppWatcher {
  private readonly seen = new Map<number, Liveness>()
  private readonly stoppedOnPurpose = new Set<number>()

  constructor(
    private readonly projects: ProjectRepository,
    private readonly containers: ContainerManager,
    private readonly events: EventBus,
    private readonly log: Logger
  ) {
    events.on('deploy:container-stopped', ({ projectId }: { projectId: number }) => {
      this.stoppedOnPurpose.add(projectId)
      this.seen.delete(projectId)
    })
    events.on('deploy:container-started', ({ projectId }: { projectId: number }) => {
      this.stoppedOnPurpose.delete(projectId)
    })
  }

  async check(): Promise<void> {
    for (const project of this.projects.all()) {
      if (this.stoppedOnPurpose.has(project.id)) continue

      const status = await this.containers.status(project)
      if (status === 'none') continue

      const now: Liveness = status === 'running' ? 'up' : 'down'
      const before = this.seen.get(project.id)
      this.seen.set(project.id, now)

      if (before === now) continue
      if (before === undefined) {
        if (status === 'crashed') this.announce('monitor:app-down', project, 'crashed')
        continue
      }
      if (now === 'down') this.announce('monitor:app-down', project, status)
      else this.announce('monitor:app-recovered', project, 'running')
    }
  }

  private announce(event: string, project: Project, status: string): void {
    this.log.warn(`${project.name} is ${status}`)
    this.events.emit(event, { projectId: project.id, name: project.name, domain: project.domain, status })
  }
}
