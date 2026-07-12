export function setup(ctx: any) {
  const { api, pages, db, events, log } = ctx
  const sqlite = db.raw

  api.get('/posts', async (c: any) => {
    const page = Number(c.req.query('page') || 1)
    const limit = Number(c.req.query('limit') || 20)
    const offset = (page - 1) * limit

    const posts = sqlite.prepare('SELECT * FROM posts ORDER BY created_at DESC LIMIT ? OFFSET ?').all(limit, offset)
    const { total } = sqlite.prepare('SELECT count(*) as total FROM posts').get() as any

    return c.json({ ok: true, data: posts, meta: { page, limit, total } })
  })

  api.get('/posts/:id', async (c: any) => {
    const id = c.req.param('id')
    const post = sqlite.prepare('SELECT * FROM posts WHERE id = ?').get(id)
    if (!post) return c.json({ ok: false, error: { code: 'NOT_FOUND', message: 'Post not found' } }, 404)
    return c.json({ ok: true, data: post })
  })

  api.post('/posts', async (c: any) => {
    const body = await c.req.json()
    const slug = body.title.toLowerCase().replace(/[^a-z0-9]+/g, '-')

    const result = sqlite
      .prepare('INSERT INTO posts (title, slug, content, published) VALUES (?, ?, ?, ?)')
      .run(body.title, slug, body.content || '', body.published ? 1 : 0)

    events.emit('blog:post-created', { id: result.lastInsertRowid, title: body.title, slug })
    log.info('post created', { title: body.title })

    return c.json({ ok: true, data: { id: result.lastInsertRowid, slug, title: body.title } }, 201)
  })

  api.patch('/posts/:id', async (c: any) => {
    const id = c.req.param('id')
    const body = await c.req.json()

    const sets: string[] = []
    const values: any[] = []
    if (body.title) { sets.push('title = ?'); values.push(body.title) }
    if (body.content !== undefined) { sets.push('content = ?'); values.push(body.content) }
    if (body.published !== undefined) { sets.push('published = ?'); values.push(body.published ? 1 : 0) }
    sets.push('updated_at = unixepoch()')
    values.push(id)

    sqlite.prepare(`UPDATE posts SET ${sets.join(', ')} WHERE id = ?`).run(...values)
    const post = sqlite.prepare('SELECT * FROM posts WHERE id = ?').get(id)

    return c.json({ ok: true, data: post })
  })

  api.delete('/posts/:id', async (c: any) => {
    const id = c.req.param('id')
    sqlite.prepare('DELETE FROM posts WHERE id = ?').run(id)
    return c.json({ ok: true, data: null })
  })

  // Page routes
  pages.get('/', (c: any) => {
    const posts = sqlite.prepare('SELECT id, title, slug, published, created_at FROM posts ORDER BY created_at DESC LIMIT 20').all()
    const postList = (posts as any[])
      .map((p: any) => `<li><a href="/blog/posts/${p.slug}">${p.title}</a> ${p.published ? '✓' : '(draft)'}</li>`)
      .join('')

    return c.html(`
      <html>
        <head><title>Blog - Shelf</title></head>
        <body style="font-family: system-ui; max-width: 640px; margin: 2rem auto; padding: 0 1rem;">
          <h1>Blog</h1>
          <ul>${postList || '<li>No posts yet.</li>'}</ul>
        </body>
      </html>
    `)
  })

  log.info('blog module loaded')
}
