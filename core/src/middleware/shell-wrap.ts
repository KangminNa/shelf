import type { Context, Next } from 'hono'
import { renderShell, type AppNavItem } from '../ui/shell.js'

/**
 * 시스템 관리 페이지 자동 셸 래핑 미들웨어.
 * 응답이 HTML 조각이면 사이드바+탑바 셸로 감싼다 (완성 문서면 그대로 통과).
 */
export function createShellWrap(title: string, getApps: () => Promise<AppNavItem[]>, previewUrl?: string) {
  return async (c: Context, next: Next) => {
    await next()

    const contentType = c.res.headers.get('content-type') || ''
    if (!contentType.includes('text/html')) return

    const body = await c.res.text()

    // 이미 완성된 HTML 문서면 래핑 생략
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
