import type { Hono } from 'hono'
import type { z } from 'zod'

export interface ModuleManifest {
  name: string
  displayName: string
  version: string
  description?: string
  icon: string
  menu: MenuItem[]
  permissions?: string[]
  externalApis?: ExternalApiConfig[]
  rateLimit?: { window: string; max: number }
  timeout?: number
}

export interface MenuItem {
  label: string
  path: string
  icon?: string
  badge?: string
}

export interface ExternalApiConfig {
  key: string
  name?: string
  baseUrl: string
  auth: ApiAuth
  required?: boolean
}

export type ApiAuth =
  | { type: 'query'; param: string }
  | { type: 'header'; header: string; prefix?: string }
  | { type: 'basic' }
  | { type: 'oauth2'; tokenUrl: string }

export interface ModuleContext {
  slug: string
  dataDir: string
  db: ModuleDB
  auth: AuthService
  notify: NotifyService
  events: EventBus
  storage: StorageService
  config: ConfigService
  api: ExternalApiClients
  ai: AiService
  log: Logger
  scheduler: Scheduler
  onShutdown: (fn: () => Promise<void> | void) => void
}

export interface ModuleDB {
  /** 날 SQL 없이 테이블을 다루는 리포지토리 (권장) */
  repo<T extends { id: number }>(table: string): import('./db/repository.js').Repository<T>
  all<T>(table: any, opts?: PaginationOpts): Promise<{ data: T[]; meta: PaginationMeta }>
  find<T>(table: any, id: number | string): Promise<T | null>
  create<T>(table: any, data: Partial<T>): Promise<T>
  update<T>(table: any, id: number | string, data: Partial<T>): Promise<T>
  remove(table: any, id: number | string): Promise<void>
  query: any // raw Drizzle instance for complex queries
  transaction<T>(fn: (tx: any) => Promise<T>): Promise<T>
}

export interface PaginationOpts {
  page?: number
  limit?: number
  sort?: string
  order?: 'asc' | 'desc'
}

export interface PaginationMeta {
  page: number
  limit: number
  total: number
  totalPages: number
}

export interface AuthService {
  requireAuth(): any
  requireRole(role: string): any
  optionalAuth(): any
  getUser(c: any): User | null
  createSession(userId: string): Promise<string>
  destroySession(token: string): Promise<void>
}

export interface User {
  id: string
  name: string
  email?: string
  role: 'admin' | 'user' | 'guest'
}

export interface NotifyService {
  send(message: string, opts?: { to?: string; channel?: string }): Promise<void>
}

export interface EventBus {
  emit(event: string, payload?: unknown): void
  on(event: string, handler: (payload: any) => void): void
  off(event: string, handler: Function): void
}

export interface StorageService {
  save(filename: string, data: Buffer | string): Promise<string>
  read(filename: string): Promise<Buffer | null>
  delete(filename: string): Promise<void>
  list(prefix?: string): Promise<string[]>
  url(filename: string): string
}

export interface ConfigService {
  get<T = string>(key: string, defaultValue?: T): Promise<T>
  set(key: string, value: any): Promise<void>
  getAll(): Promise<Record<string, any>>
}

export interface ExternalApiClients {
  [key: string]: ExternalApiClient
}

export interface ExternalApiClient {
  get<T = any>(path: string, opts?: { params?: Record<string, any> }): Promise<T>
  post<T = any>(path: string, body?: any): Promise<T>
  put<T = any>(path: string, body?: any): Promise<T>
  delete<T = any>(path: string): Promise<T>
}

export interface AiService {
  generate(opts: AiGenerateOpts): Promise<string | object>
  stream(opts: AiStreamOpts): Promise<void>
  scaffold(description: string): Promise<ScaffoldResult>
}

export interface AiGenerateOpts {
  prompt: string
  system?: string
  schema?: z.ZodType
  provider?: string
  maxTokens?: number
}

export interface AiStreamOpts {
  prompt: string
  system?: string
  onChunk: (text: string) => void
  provider?: string
}

export interface ScaffoldResult {
  manifest: ModuleManifest
  schema: string
  logic: string
  pages: Record<string, string>
}

export interface Logger {
  info(message: string, data?: any): void
  warn(message: string, data?: any): void
  error(message: string, data?: any): void
  debug(message: string, data?: any): void
}

export interface Scheduler {
  register(cron: string, name: string, handler: () => Promise<void>): void
  unregister(name: string): void
}

export interface LoadedModule {
  manifest: ModuleManifest
  context: ModuleContext
  api: Hono
  pages: Hono
  admin: Hono
  shutdown?: () => Promise<void> | void
}

export interface ApiResponse<T = any> {
  ok: boolean
  data: T | null
  meta?: PaginationMeta
  error?: ApiError
}

export interface ApiError {
  code: string
  message: string
  details?: unknown
}
