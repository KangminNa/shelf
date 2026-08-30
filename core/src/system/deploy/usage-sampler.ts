import type { DockerService } from '../docker.js'

export interface AppUsage {
  name: string
  cpu: number | null
  memory: number | null
}

export class UsageSampler {
  private sample = new Map<string, { cpu: number | null; memory: number | null }>()

  constructor(private readonly docker: DockerService) {}

  async refresh(): Promise<void> {
    this.sample = await this.docker.stats('shelf-')
  }

  of(containerName: string): { cpu: number | null; memory: number | null } {
    return this.sample.get(containerName) ?? { cpu: null, memory: null }
  }
}
