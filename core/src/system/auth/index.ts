import { Hono } from 'hono'
import type { MiddlewareHandler } from 'hono'
import { getCookie, setCookie, deleteCookie } from 'hono/cookie'
import * as crypto from 'node:crypto'
import { join } from 'node:path'
import type Database from 'better-sqlite3'
import { AppDatabase } from '../../db/database.js'
import { Repository } from '../../db/repository.js'
import { Logger } from '../../services/log.js'
import { LoginPage, SetupPage } from './views.js'

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

  deleteForUser(userId: number): number {
    return this.query().where('user_id', userId).delete()
  }

  deleteAll(): number {
    return this.query().delete()
  }
}

export class AuthSystem {
  static readonly COOKIE = 'shelf_session'
  static readonly SESSION_TTL_SECONDS = 7 * 24 * 3600
  static readonly MAX_LOGIN_FAILURES = 5
  static readonly LOCKOUT_MS = 15 * 60_000

  readonly routes = new Hono()

  private readonly users: UserRepository
  private readonly sessions: SessionRepository
  private readonly log = new Logger('auth')
  private readonly loginFailures = new Map<string, { count: number; lockedUntil: number }>()

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

  private registerRoutes(): void {
    this.routes.get('/login', (c) => {
      if (this.needsSetup) return c.redirect('/setup')
      return c.html(new LoginPage().render())
    })

    this.routes.get('/setup', (c) => {
      if (!this.needsSetup) return c.redirect('/login')
      return c.html(new SetupPage().render())
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
      const user = this.createAccount(username, password)
      this.issueSession(c, user)
      return c.json({ ok: true, data: { username } }, 201)
    })

    this.routes.post('/api/auth/login', async (c) => {
      const ip = AuthSystem.clientIp(c)
      const lock = this.loginFailures.get(ip)
      if (lock && lock.lockedUntil > Date.now()) {
        const waitMin = Math.ceil((lock.lockedUntil - Date.now()) / 60_000)
        return c.json({ ok: false, error: { code: 'RATE_LIMITED', message: `Too many failed attempts. Try again in ${waitMin}m.` } }, 429)
      }

      const { username, password } = await c.req.json()
      const user = username ? this.users.findByUsername(username) : undefined
      if (!user || !AuthSystem.verifyPassword(password || '', user.password_hash)) {
        this.recordFailure(ip)
        this.log.warn(`failed login attempt for "${username}" from ${ip}`)
        return c.json({ ok: false, error: { code: 'UNAUTHORIZED', message: 'Invalid username or password' } }, 401)
      }

      this.loginFailures.delete(ip)
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

  createAccount(username: string, password: string): User {
    const user = this.users.create({ username, password_hash: AuthSystem.hashPassword(password) } as Partial<User>)
    this.log.info(`admin account created: ${username}`)
    return user
  }

  get accounts(): string[] {
    return this.users.query().orderBy('username').pluck<string>('username')
  }

  setPassword(username: string, password: string): boolean {
    const user = this.users.findByUsername(username)
    if (!user) return false
    this.users.update(user.id, { password_hash: AuthSystem.hashPassword(password) } as Partial<User>)
    this.sessions.deleteForUser(user.id)
    this.log.warn(`password reset for "${username}" — all sessions revoked`)
    return true
  }

  forgetEveryone(): void {
    this.sessions.deleteAll()
    for (const user of this.users.all()) this.users.delete(user.id)
    this.log.warn('all accounts removed — server reopened for setup')
  }

  private issueSession(c: any, user: User): void {
    const token = crypto.randomBytes(32).toString('hex')
    this.sessions.create({
      token,
      user_id: user.id,
      expires_at: Math.floor(Date.now() / 1000) + AuthSystem.SESSION_TTL_SECONDS,
    })
    const isHttps = c.req.header('x-forwarded-proto') === 'https' || new URL(c.req.url).protocol === 'https:'
    setCookie(c, AuthSystem.COOKIE, token, {
      path: '/',
      httpOnly: true,
      sameSite: 'Lax',
      secure: isHttps,
      maxAge: AuthSystem.SESSION_TTL_SECONDS,
    })
  }

  private recordFailure(ip: string): void {
    const entry = this.loginFailures.get(ip) || { count: 0, lockedUntil: 0 }
    entry.count += 1
    if (entry.count >= AuthSystem.MAX_LOGIN_FAILURES) {
      entry.lockedUntil = Date.now() + AuthSystem.LOCKOUT_MS
      entry.count = 0
      this.log.warn(`login locked for ${ip} (${AuthSystem.LOCKOUT_MS / 60_000}m)`)
    }
    this.loginFailures.set(ip, entry)
  }

  private static clientIp(c: any): string {
    return (
      c.req.header('x-real-ip') ||
      c.req.header('x-forwarded-for')?.split(',')[0].trim() ||
      c.env?.incoming?.socket?.remoteAddress ||
      'unknown'
    )
  }

  private validateSession(token: string): boolean {
    const session = this.sessions.findByToken(token)
    if (!session) return false
    const expired = session.expires_at < Math.floor(Date.now() / 1000)
    if (expired || !this.users.find(session.user_id)) {
      this.sessions.delete(session.id)
      return false
    }
    return true
  }

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
