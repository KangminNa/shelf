import * as http from 'node:http'
import type { Logger } from '../../services/log.js'
import type { WebhookHandler } from './webhook-handler.js'

const MAX_BODY = 1024 * 1024

export class WebhookServer {
  readonly port: number
  private server?: http.Server

  constructor(
    private readonly handler: WebhookHandler,
    private readonly log: Logger
  ) {
    this.port = Number(process.env.WEBHOOK_PORT || 9100)
  }

  start(): void {
    this.server = http.createServer((req, res) => this.accept(req, res))
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

  private accept(req: http.IncomingMessage, res: http.ServerResponse): void {
    const url = new URL(req.url || '/', 'http://localhost')
    if (req.method !== 'POST' || !url.pathname.startsWith('/hooks/')) {
      this.reply(res, 404, { ok: false, error: 'Not found. Use POST /hooks/{projectId} or /hooks/self' })
      return
    }

    this.readBody(req, res, (body) => {
      const reply = this.handler.handle({
        path: url.pathname.slice('/hooks'.length),
        body,
        signature: req.headers['x-hub-signature-256'] as string | undefined,
        gitlabToken: req.headers['x-gitlab-token'] as string | undefined,
        secretParam: url.searchParams.get('secret') || undefined,
      })
      this.reply(res, reply.status, reply.body)
    })
  }

  private readBody(req: http.IncomingMessage, res: http.ServerResponse, done: (body: Buffer) => void): void {
    let received = 0
    const chunks: Buffer[] = []
    req.on('data', (chunk) => {
      received += chunk.length
      if (received > MAX_BODY) {
        this.reply(res, 413, { ok: false, error: 'Payload too large' })
        req.destroy()
        return
      }
      chunks.push(chunk)
    })
    req.on('end', () => {
      if (received <= MAX_BODY) done(Buffer.concat(chunks))
    })
  }

  private reply(res: http.ServerResponse, status: number, body: unknown): void {
    res.writeHead(status, { 'content-type': 'application/json' })
    res.end(JSON.stringify(body))
  }
}
