import { Hono } from 'hono'
import type { MiddlewareHandler } from 'hono'
import { getCookie, setCookie, deleteCookie } from 'hono/cookie'
import * as crypto from 'node:crypto'
import { join } from 'node:path'
import type Database from 'better-sqlite3'
import { AppDatabase } from '../../db/database.js'
import { Repository } from '../../db/repository.js'
import { Logger } from '../../services/log.js'
import { loginPage, setupPage } from './views.js'

export interface User {
  id: number
  username: string
  password_hash: string
  created_at: number
}

export interface Session {
  id: number
  token: string
  user_id: number
  expires_at: number
  created_at: number
}

class UserRepository extends Repository<User> {
  constructor(db: Database.Database) {
    super(db, 'users')
  }

  findByUsername(username: string): User | undefined {
    return this.findBy({ username } as Partial<User>)
  }
}

class SessionRepository extends Repository<Session> {
  constructor(db: Database.Database) {
    super(db, 'sessions')
  }

  findByToken(token: string): Session | undefined {
    return this.findBy({ token } as Partial<Session>)
  }

  deleteExpired(): void {
    this.query().where('expires_at', '<', Math.floor(Date.now() / 1000)).delete()
  }
}

/**
 * 코어 내장 인증 시스템.
 * - 최초 접속 시 /setup에서 관리자 계정 생성 (1회)
 * - 세션 쿠키(shelf_session, httpOnly) 기반 로그인
 * - requireAuth() 미들웨어로 /admin, /api 보호
 * 비밀번호는 scrypt(salt 16B, key 64B)로 해시한다.
 */
export class AuthSystem {
  static readonly COOKIE = 'shelf_session'
  static readonly SESSION_TTL_SECONDS = 7 * 24 * 3600

  /** 공개 라우트: /login, /setup, /api/auth/* */
  readonly routes = new Hono()

  private readonly users: UserRepository
  private readonly sessions: SessionRepository
  private readonly log = new Logger('auth')

  constructor() {
    const db = new AppDatabase('auth', join(process.cwd(), 'core', 'migrations', 'auth'))
    this.users = new UserRepository(db.raw)
    this.sessions = new SessionRepository(db.raw)
    this.sessions.deleteExpired()
    this.registerRoutes()
    this.log.info(`auth system ready (${this.users.count()} user${this.users.count() === 1 ? '' : 's'})`)
  }

  get needsSetup(): boolean {
    return this.users.count() === 0
  }

  /** 보호 미들웨어 — 미인증 시 페이지는 /login으로, API는 401 */
  requireAuth(): MiddlewareHandler {
    return async (c, next) => {
      const token = getCookie(c, AuthSystem.COOKIE)
      if (token && this.validateSession(token)) {
        return next()
      }
      if (c.req.path.startsWith('/api/')) {
        return c.json({ ok: false, error: { code: 'UNAUTHORIZED', message: 'Sign in required' } }, 401)
      }
      return c.redirect(this.needsSetup ? '/setup' : '/login')
    }
  }

  // --- 내부 ---

  private registerRoutes(): void {
    this.routes.get('/login', (c) => {
      if (this.needsSetup) return c.redirect('/setup')
      return c.html(loginPage())
    })

    this.routes.get('/setup', (c) => {
      if (!this.needsSetup) return c.redirect('/login')
      return c.html(setupPage())
    })

    this.routes.post('/api/auth/setup', async (c) => {
      if (!this.needsSetup) {
        return c.json({ ok: false, error: { code: 'FORBIDDEN', message: 'Setup is already complete' } }, 403)
      }
      const { username, password } = await c.req.json()
      if (!username || !/^[a-zA-Z0-9-_.]{2,32}$/.test(username)) {
        return c.json({ ok: false, error: { code: 'VALIDATION', message: 'Invalid username (2-32 chars, alphanumeric)' } }, 400)
      }
      if (!password || password.length < 8) {
        return c.json({ ok: false, error: { code: 'VALIDATION', message: 'Password must be at least 8 characters' } }, 400)
      }
      const user = this.users.create({ username, password_hash: AuthSystem.hashPassword(password) })
      this.log.info(`admin account created: ${username}`)
      this.issueSession(c, user)
      return c.json({ ok: true, data: { username } }, 201)
    })

    this.routes.post('/api/auth/login', async (c) => {
      const { username, password } = await c.req.json()
      const user = username ? this.users.findByUsername(username) : undefined
      if (!user || !AuthSystem.verifyPassword(password || '', user.password_hash)) {
        this.log.warn(`failed login attempt for "${username}"`)
        return c.json({ ok: false, error: { code: 'UNAUTHORIZED', message: 'Invalid username or password' } }, 401)
      }
      this.issueSession(c, user)
      return c.json({ ok: true, data: { username: user.username } })
    })

    this.routes.post('/api/auth/logout', (c) => {
      const token = getCookie(c, AuthSystem.COOKIE)
      if (token) {
        const session = this.sessions.findByToken(token)
        if (session) this.sessions.delete(session.id)
      }
      deleteCookie(c, AuthSystem.COOKIE, { path: '/' })
      return c.json({ ok: true, data: null })
    })
  }

  private issueSession(c: any, user: User): void {
    const token = crypto.randomBytes(32).toString('hex')
    this.sessions.create({
      token,
      user_id: user.id,
      expires_at: Math.floor(Date.now() / 1000) + AuthSystem.SESSION_TTL_SECONDS,
    })
    setCookie(c, AuthSystem.COOKIE, token, {
      path: '/',
      httpOnly: true,
      sameSite: 'Lax',
      maxAge: AuthSystem.SESSION_TTL_SECONDS,
    })
  }

  private validateSession(token: string): boolean {
    const session = this.sessions.findByToken(token)
    if (!session) return false
    if (session.expires_at < Math.floor(Date.now() / 1000)) {
      this.sessions.delete(session.id)
      return false
    }
    return true
  }

  // --- 비밀번호 해시 (scrypt) ---

  static hashPassword(password: string): string {
    const salt = crypto.randomBytes(16).toString('hex')
    const hash = crypto.scryptSync(password, salt, 64).toString('hex')
    return `${salt}:${hash}`
  }

  static verifyPassword(password: string, stored: string): boolean {
    const [salt, hash] = stored.split(':')
    if (!salt || !hash) return false
    const candidate = crypto.scryptSync(password, salt, 64)
    try {
      return crypto.timingSafeEqual(Buffer.from(hash, 'hex'), candidate)
    } catch {
      return false
    }
  }
}
