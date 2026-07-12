import type { MiddlewareHandler } from 'hono'
import type { z } from 'zod'
import { Errors } from './errors.js'

export function validated<T extends z.ZodType>(schema: T): MiddlewareHandler {
  return async (c, next) => {
    const body = await c.req.json().catch(() => ({}))
    const result = schema.safeParse(body)

    if (!result.success) {
      throw Errors.validation(result.error.flatten())
    }

    c.set('body', result.data)
    await next()
  }
}

export function validatedQuery<T extends z.ZodType>(schema: T): MiddlewareHandler {
  return async (c, next) => {
    const query = c.req.query()
    const result = schema.safeParse(query)

    if (!result.success) {
      throw Errors.validation(result.error.flatten())
    }

    c.set('query', result.data)
    await next()
  }
}
