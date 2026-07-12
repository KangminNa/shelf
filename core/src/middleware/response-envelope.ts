import type { MiddlewareHandler } from 'hono'

export const responseEnvelope: MiddlewareHandler = async (c, next) => {
  await next()

  if (!c.res.headers.get('content-type')?.includes('application/json')) return

  const status = c.res.status
  if (status >= 300) return

  try {
    const body = await c.res.json()
    if (body && typeof body === 'object' && 'ok' in body) return

    c.res = c.json({ ok: true, data: body }, status as any)
  } catch {
    // not JSON, leave as-is
  }
}
