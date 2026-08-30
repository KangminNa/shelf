import * as crypto from 'node:crypto'
import type { Logger } from '../../services/log.js'
import type { ProjectRepository, Project } from './repositories.js'
import type { DeployPipeline } from './pipeline.js'
import type { SelfDeployer } from './self-deployer.js'

export interface WebhookRequest {
  path: string
  body: Buffer
  signature?: string
  gitlabToken?: string
  secretParam?: string
}

export interface WebhookReply {
  status: number
  body: { ok: boolean; message?: string; error?: string }
}

const accepted = (message: string): WebhookReply => ({ status: 202, body: { ok: true, message } })
const ignored = (message: string): WebhookReply => ({ status: 200, body: { ok: true, message } })
const rejected = (status: number, error: string): WebhookReply => ({ status, body: { ok: false, error } })

export class WebhookHandler {
  constructor(
    private readonly projects: ProjectRepository,
    private readonly pipeline: DeployPipeline,
    private readonly selfDeployer: SelfDeployer,
    private readonly log: Logger
  ) {}

  handle(request: WebhookRequest): WebhookReply {
    if (request.path.startsWith('/self')) return this.rebuildShelf(request)

    const id = /^\/(\d+)/.exec(request.path)?.[1]
    if (!id) return rejected(404, 'Not found. Use POST /hooks/{projectId} or /hooks/self')

    return this.deployProject(Number(id), request)
  }

  private rebuildShelf(request: WebhookRequest): WebhookReply {
    if (!this.selfDeployer.configured) return rejected(404, 'Self-deploy not configured (set SELF_DEPLOY_SECRET)')
    if (!this.selfDeployer.verify(request.body, request.signature)) {
      this.log.warn('self-deploy signature mismatch')
      return rejected(401, 'Invalid signature')
    }
    if (!this.selfDeployer.matchesBranch(WebhookHandler.payload(request.body))) {
      return ignored('Ignoring push to other branch')
    }
    this.selfDeployer.trigger()
    return accepted('Rebuilding Shelf...')
  }

  private deployProject(id: number, request: WebhookRequest): WebhookReply {
    const project = this.projects.find(id)
    if (!project) return rejected(404, 'Project not found')

    if (!this.authentic(project, request)) {
      this.log.warn(`webhook signature mismatch for "${project.name}"`)
      return rejected(401, 'Invalid signature')
    }
    if (!project.auto_deploy) return ignored('Auto-deploy disabled, ignoring')

    const ref = WebhookHandler.payload(request.body).ref
    if (ref && ref !== `refs/heads/${project.branch}`) return ignored(`Ignoring push to ${ref}`)

    this.pipeline.deploy(project, 'webhook')
    return accepted(`Deploying ${project.name}...`)
  }

  private authentic(project: Project, request: WebhookRequest): boolean {
    if (request.signature) return WebhookHandler.signatureMatches(project.webhook_secret, request.body, request.signature)
    if (request.gitlabToken) return WebhookHandler.constantEquals(request.gitlabToken, project.webhook_secret)
    if (request.secretParam) return WebhookHandler.constantEquals(request.secretParam, project.webhook_secret)
    return false
  }

  static signatureMatches(secret: string, body: Buffer, signature: string): boolean {
    const expected = 'sha256=' + crypto.createHmac('sha256', secret).update(body).digest('hex')
    return WebhookHandler.constantEquals(signature, expected)
  }

  static constantEquals(a: string, b: string): boolean {
    const left = Buffer.from(a)
    const right = Buffer.from(b)
    if (left.length !== right.length) return false
    return crypto.timingSafeEqual(left, right)
  }

  private static payload(body: Buffer): { ref?: string } {
    try {
      return JSON.parse(body.toString())
    } catch {
      return {}
    }
  }
}
