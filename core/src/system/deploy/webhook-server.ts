import * as http from 'node:http'
import * as crypto from 'node:crypto'
import type { Logger } from '../../services/log.js'
import type { ProjectRepository, Project } from './repositories.js'
import type { DeployPipeline } from './pipeline.js'

/**
 * CI/CD용 webhook 전용 HTTP 서버 (별도 포트, 기본 9100).
 * POST /hooks/{projectId} — GitHub(X-Hub-Signature-256 HMAC) / GitLab(X-Gitlab-Token) 검증 후
 * 해당 브랜치 push면 자동 배포를 트리거한다.
 */
export class WebhookServer {
  readonly port: number
  private server?: http.Server

  constructor(
    private readonly projects: ProjectRepository,
    private readonly pipeline: DeployPipeline,
    private readonly log: Logger
  ) {
    this.port = Number(process.env.WEBHOOK_PORT || 9100)
  }

  start(): void {
    this.server = http.createServer((req, res) => this.handle(req, res))
    this.server
      .listen(this.port, '0.0.0.0', () => this.log.info(`webhook server listening on :${this.port}`))
      .on('error', (err: NodeJS.ErrnoException) => {
        if (err.code === 'EADDRINUSE') this.log.warn(`port ${this.port} already in use`)
        else this.log.error(`webhook server error: ${err.message}`)
      })
  }

  stop(): void {
    this.server?.close()
  }

  private handle(req: http.IncomingMessage, res: http.ServerResponse): void {
    const match = (req.url || '').match(/^\/hooks\/(\d+)/)
    if (req.method !== 'POST' || !match) {
      this.respond(res, 404, { ok: false, error: 'Not found. Use POST /hooks/{projectId}' })
      return
    }

    const chunks: Buffer[] = []
    req.on('data', (c) => chunks.push(c))
    req.on('end', () => {
      const body = Buffer.concat(chunks)
      const project = this.projects.find(Number(match[1]))

      if (!project) {
        this.respond(res, 404, { ok: false, error: 'Project not found' })
        return
      }
      if (!this.verifySignature(project, body, req)) {
        this.log.warn(`webhook signature mismatch for "${project.name}"`)
        this.respond(res, 401, { ok: false, error: 'Invalid signature' })
        return
      }
      if (!project.auto_deploy) {
        this.respond(res, 200, { ok: true, message: 'Auto-deploy disabled, ignoring' })
        return
      }

      // 브랜치 필터 (GitHub push payload: "ref": "refs/heads/main")
      let payload: { ref?: string } = {}
      try { payload = JSON.parse(body.toString()) } catch {}
      if (payload.ref && payload.ref !== `refs/heads/${project.branch}`) {
        this.respond(res, 200, { ok: true, message: `Ignoring push to ${payload.ref}` })
        return
      }

      this.respond(res, 202, { ok: true, message: `Deploying ${project.name}...` })
      this.pipeline.deploy(project, 'webhook')
    })
  }

  private verifySignature(project: Project, body: Buffer, req: http.IncomingMessage): boolean {
    const secret = project.webhook_secret

    // GitHub: X-Hub-Signature-256: sha256=<hmac>
    const githubSig = req.headers['x-hub-signature-256'] as string | undefined
    if (githubSig) {
      const expected = 'sha256=' + crypto.createHmac('sha256', secret).update(body).digest('hex')
      try {
        return crypto.timingSafeEqual(Buffer.from(githubSig), Buffer.from(expected))
      } catch {
        return false
      }
    }

    // GitLab: X-Gitlab-Token
    const gitlabToken = req.headers['x-gitlab-token'] as string | undefined
    if (gitlabToken) return gitlabToken === secret

    // 그 외: ?secret= 쿼리 파라미터
    const url = new URL(req.url || '/', 'http://localhost')
    return url.searchParams.get('secret') === secret
  }

  private respond(res: http.ServerResponse, status: number, body: unknown): void {
    res.writeHead(status, { 'content-type': 'application/json' })
    res.end(JSON.stringify(body))
  }
}
