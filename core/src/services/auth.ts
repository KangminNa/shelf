import { randomUUID } from 'crypto'
import type { MiddlewareHandler } from 'hono'
import type { AuthService, User } from '../types.js'
import { Errors } from '../helpers/errors.js'
import Database from 'better-sqlite3'
import { join } from 'path'
import { existsSync, mkdirSync } from 'fs'

const DATA_DIR = join(process.cwd(), 'data')

export function createAuthService(): AuthService {
  if (!existsSync(DATA_DIR)) mkdirSync(DATA_DIR, { recursive: true })
  const db = new Database(join(DATA_DIR, 'core.db'))

  db.exec(`
    CREATE TABLE IF NOT EXISTS users (
      id TEXT PRIMARY KEY,
      name TEXT NOT NULL,
      email TEXT,
      password_hash TEXT NOT NULL,
      role TEXT DEFAULT 'user',
      created_at INTEGER DEFAULT (unixepoch())
    )
  `)

  db.exec(`
    CREATE TABLE IF NOT EXISTS sessions (
      id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL REFERENCES users(id),
      token TEXT NOT NULL UNIQUE,
      expires_at INTEGER NOT NULL,
      created_at INTEGER DEFAULT (unixepoch())
    )
  `)

  function getUserFromToken(token: string): User | null {
    const session = db
      .prepare(
        `SELECT u.id, u.name, u.email, u.role FROM sessions s
         JOIN users u ON s.user_id = u.id
         WHERE s.token = ? AND s.expires_at > unixepoch()`
      )
      .get(token) as User | undefined
    return session || null
  }

  function extractToken(c: any): string | null {
    const cookie = c.req.header('cookie')
    if (cookie) {
      const match = cookie.match(/shelf_session=([^;]+)/)
      if (match) return match[1]
    }
    const auth = c.req.header('authorization')
    if (auth?.startsWith('Bearer ')) return auth.slice(7)
    return null
  }

  const requireAuth: () => MiddlewareHandler = () => async (c, next) => {
    const token = extractToken(c)
    if (!token) throw Errors.unauthorized()
    const user = getUserFromToken(token)
    if (!user) throw Errors.unauthorized()
    c.set('user', user)
    await next()
  }

  const requireRole: (role: string) => MiddlewareHandler = (role) => async (c, next) => {
    const token = extractToken(c)
    if (!token) throw Errors.unauthorized()
    const user = getUserFromToken(token)
    if (!user) throw Errors.unauthorized()
    if (user.role !== role && user.role !== 'admin') throw Errors.forbidden()
    c.set('user', user)
    await next()
  }

  const optionalAuth: () => MiddlewareHandler = () => async (c, next) => {
    const token = extractToken(c)
    if (token) {
      const user = getUserFromToken(token)
      if (user) c.set('user', user)
    }
    await next()
  }

  return {
    requireAuth,
    requireRole,
    optionalAuth,

    getUser(c) {
      return c.get('user') || null
    },

    async createSession(userId: string) {
      const token = randomUUID()
      const expiresAt = Math.floor(Date.now() / 1000) + 7 * 24 * 3600 // 7 days
      db.prepare(
        'INSERT INTO sessions (id, user_id, token, expires_at) VALUES (?, ?, ?, ?)'
      ).run(randomUUID(), userId, token, expiresAt)
      return token
    },

    async destroySession(token: string) {
      db.prepare('DELETE FROM sessions WHERE token = ?').run(token)
    },
  }
}
