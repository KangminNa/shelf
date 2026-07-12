import type { MiddlewareHandler } from 'hono'
import type { PaginationOpts, PaginationMeta } from '../types.js'

export function paginated(): MiddlewareHandler {
  return async (c, next) => {
    const pagination: PaginationOpts = {
      page: Math.max(1, Number(c.req.query('page') || 1)),
      limit: Math.min(100, Math.max(1, Number(c.req.query('limit') || 20))),
      sort: c.req.query('sort') || undefined,
      order: (c.req.query('order') as 'asc' | 'desc') || 'desc',
    }
    c.set('pagination', pagination)
    await next()
  }
}

export function buildMeta(total: number, opts: PaginationOpts): PaginationMeta {
  const page = opts.page || 1
  const limit = opts.limit || 20
  return {
    page,
    limit,
    total,
    totalPages: Math.ceil(total / limit),
  }
}
