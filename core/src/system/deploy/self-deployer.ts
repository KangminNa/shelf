import * as crypto from 'node:crypto'
import type { DockerService } from '../docker.js'
import type { EventBus } from '../../services/events.js'
import type { Logger } from '../../services/log.js'

export interface SelfDeployConfig {
  secret: string
  repoDir: string
  branch: string
  composeProject: string
}

export class SelfDeployer {
  static readonly HELPER_IMAGE = 'docker:cli'
  private static readonly RESPONSE_GRACE_SECONDS = 3

  constructor(
    private readonly docker: DockerService,
    private readonly events: EventBus,
    private readonly log: Logger
  ) {}

  get configured(): boolean {
    return !!this.config
  }

  private get config(): SelfDeployConfig | null {
    const secret = process.env.SELF_DEPLOY_SECRET
    if (!secret) return null
    return {
      secret,
      repoDir: process.env.SELF_DEPLOY_DIR || '/shelf',
      branch: process.env.SELF_DEPLOY_BRANCH || 'main',
      composeProject: process.env.SELF_DEPLOY_COMPOSE_PROJECT || 'shelf',
    }
  }

  verify(body: Buffer, signature: string | undefined): boolean {
    const config = this.config
    if (!config || !signature) return false
    const expected = 'sha256=' + crypto.createHmac('sha256', config.secret).update(body).digest('hex')
    try {
      return crypto.timingSafeEqual(Buffer.from(signature), Buffer.from(expected))
    } catch {
      return false
    }
  }

  matchesBranch(payload: { ref?: string }): boolean {
    const config = this.config
    if (!config) return false
    return !payload.ref || payload.ref === `refs/heads/${config.branch}`
  }

  trigger(): void {
    const config = this.config
    if (!config) return
    this.log.info('self-deploy triggered — spawning helper to rebuild Shelf')
    this.events.emit('deploy:self-started', {})
    this.docker.spawnDetached(
      SelfDeployer.HELPER_IMAGE,
      this.rebuildScript(config),
      ['/var/run/docker.sock:/var/run/docker.sock', `${config.repoDir}:/repo`]
    )
  }

  private rebuildScript(config: SelfDeployConfig): string {
    return [
      `sleep ${SelfDeployer.RESPONSE_GRACE_SECONDS}`,
      `cd /repo`,
      `git fetch origin ${config.branch}`,
      `git reset --hard origin/${config.branch}`,
      `docker compose -p ${config.composeProject} up -d --build`,
    ].join(' && ')
  }
}
