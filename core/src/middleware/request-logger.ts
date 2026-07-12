import type { MiddlewareHandler } from 'hono'

export const requestLogger: MiddlewareHandler = async (c, next) => {
  const start = Date.now()
  const method = c.req.method
  const path = c.req.path

  await next()

  const duration = Date.now() - start
  const status = c.res.status
  const level = status >= 500 ? 'ERROR' : status >= 400 ? 'WARN' : 'INFO'

  const timestamp = new Date().toISOString().slice(0, 19).replace('T', ' ')
  console.log(`[${timestamp}] ${level} ${method} ${path} ${status} ${duration}ms`)
}
