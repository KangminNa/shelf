import type { Context, Next } from 'hono'
import { renderShell, type AppNavItem } from '../ui/shell.js'

export function createShellWrap(title: string, getApps: () => Promise<AppNavItem[]>, previewUrl?: string) {
  return async (c: Context, next: Next) => {
    await next()

    const contentType = c.res.headers.get('content-type') || ''
    if (!contentType.includes('text/html')) return

    const body = await c.res.text()

    if (body.trimStart().toLowerCase().startsWith('<!doctype') || body.trimStart().toLowerCase().startsWith('<html')) {
      c.res = new Response(body, c.res)
      return
    }

    const wrapped = renderShell({
      title,
      activePath: new URL(c.req.url).pathname,
      content: body,
      apps: await getApps(),
      previewUrl,
    })

    c.res = new Response(wrapped, {
      status: c.res.status,
      headers: { 'content-type': 'text/html; charset=utf-8' },
    })
  }
}
