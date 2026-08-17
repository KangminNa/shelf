import { Hono } from 'hono'
import type { Context } from 'hono'
import type { Logger } from '../services/log.js'

const STATUS_BY_CODE: Record<string, number> = {
  NOT_FOUND: 404,
  VALIDATION: 400,
  NOT_SUPPORTED: 400,
  CONFLICT: 409,
}

export class HttpError extends Error {
  constructor(
    readonly code: string,
    message: string,
    readonly data: unknown = null
  ) {
    super(message)
  }

  get status(): number {
    return STATUS_BY_CODE[this.code] ?? 500
  }
}

export const notFound = (what: string): never => {
  throw new HttpError('NOT_FOUND', `${what} not found`)
}

export const invalid = (message: string): never => {
  throw new HttpError('VALIDATION', message)
}

export const conflict = (message: string): never => {
  throw new HttpError('CONFLICT', message)
}

export const failed = (code: string, message: string, data?: unknown): never => {
  throw new HttpError(code, message, data)
}

export interface Request {
  readonly id: string
  readonly body: Record<string, any>
  param(name: string): string
  query(name: string, fallback?: string): string
  number(name: string, fallback: number): number
}

type Handler = (request: Request) => unknown

export abstract class Controller {
  readonly api = new Hono()
  readonly pages = new Hono()

  protected constructor(private readonly logger?: Logger) {}

  protected get(path: string, handle: Handler): void {
    this.route('get', path, handle, 200)
  }

  protected post(path: string, handle: Handler, status = 200): void {
    this.route('post', path, handle, status)
  }

  protected patch(path: string, handle: Handler): void {
    this.route('patch', path, handle, 200)
  }

  protected delete(path: string, handle: Handler): void {
    this.route('delete', path, handle, 200)
  }

  protected page(path: string, render: (request: Request) => string | Promise<string>): void {
    this.pages.get(path, async (c) => c.html(await render(await this.read(c))))
  }

  private route(method: 'get' | 'post' | 'patch' | 'delete', path: string, handle: Handler, status: number): void {
    this.api[method](path, async (c) => {
      try {
        const data = await handle(await this.read(c))
        return c.json({ ok: true, data: data ?? null }, status as never)
      } catch (err) {
        return this.failure(c, err)
      }
    })
  }

  private async read(c: Context): Promise<Request> {
    const body = c.req.method === 'GET' ? {} : await c.req.json().catch(() => ({}))
    return {
      id: c.req.param('id') ?? '',
      body,
      param: (name) => c.req.param(name) ?? '',
      query: (name, fallback = '') => c.req.query(name) || fallback,
      number: (name, fallback) => Number(c.req.query(name) || fallback),
    }
  }

  private failure(c: Context, err: unknown) {
    const known = err instanceof HttpError || (err instanceof Error && 'code' in err && typeof (err as any).code === 'string')
    const code = known ? String((err as any).code) : 'INTERNAL'
    const message = err instanceof Error ? err.message : String(err)
    const status = err instanceof HttpError ? err.status : STATUS_BY_CODE[code] ?? 500
    const data = err instanceof HttpError ? err.data : null

    if (status >= 500) this.logger?.error(`${c.req.method} ${c.req.path}: ${message}`)
    return c.json({ ok: false, error: { code, message }, data }, status as never)
  }
}

export function fields<T>(body: Record<string, any>, names: readonly string[]): Partial<T> {
  const patch: Record<string, unknown> = {}
  for (const name of names) {
    if (body[name] === undefined) continue
    patch[name] = typeof body[name] === 'boolean' ? (body[name] ? 1 : 0) : body[name]
  }
  return patch as Partial<T>
}
