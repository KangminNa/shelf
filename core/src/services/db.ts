import Database from 'better-sqlite3'
import { drizzle } from 'drizzle-orm/better-sqlite3'
import { sql } from 'drizzle-orm'
import { existsSync, mkdirSync, readdirSync, readFileSync } from 'fs'
import { join } from 'path'
import type { ModuleDB, PaginationOpts } from '../types.js'
import { buildMeta } from '../helpers/pagination.js'

const DATA_DIR = join(process.cwd(), 'data')

export function ensureDataDir() {
  if (!existsSync(DATA_DIR)) mkdirSync(DATA_DIR, { recursive: true })
}

export function createModuleDB(slug: string, migrationsDir?: string): ModuleDB & { raw: Database.Database } {
  ensureDataDir()
  const dbPath = join(DATA_DIR, `${slug}.db`)
  const sqlite = new Database(dbPath)
  sqlite.pragma('journal_mode = WAL')
  sqlite.pragma('foreign_keys = ON')

  const db = drizzle(sqlite)

  if (migrationsDir && existsSync(migrationsDir)) {
    runMigrations(sqlite, slug, migrationsDir)
  }

  return {
    raw: sqlite,
    async all(table, opts?: PaginationOpts) {
      const page = opts?.page || 1
      const limit = opts?.limit || 20
      const offset = (page - 1) * limit

      const countResult = db.select({ count: sql<number>`count(*)` }).from(table).get()
      const total = (countResult as any)?.count || 0

      const data = db.select().from(table).limit(limit).offset(offset).all()
      return { data: data as any[], meta: buildMeta(total, { page, limit }) }
    },

    async find(table, id) {
      const result = db.select().from(table).where(sql`id = ${id}`).get()
      return (result as any) || null
    },

    async create(table, data) {
      const result = db.insert(table).values(data as any).returning().get()
      return result as any
    },

    async update(table, id, data) {
      const result = db
        .update(table)
        .set(data as any)
        .where(sql`id = ${id}`)
        .returning()
        .get()
      return result as any
    },

    async remove(table, id) {
      db.delete(table).where(sql`id = ${id}`).run()
    },

    query: db,

    async transaction(fn) {
      return sqlite.transaction(() => fn(db))()
    },
  }
}

function runMigrations(sqlite: Database.Database, slug: string, migrationsDir: string) {
  sqlite.exec(`
    CREATE TABLE IF NOT EXISTS _shelf_migrations (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      version TEXT NOT NULL UNIQUE,
      applied_at INTEGER DEFAULT (unixepoch())
    )
  `)

  const applied = new Set(
    sqlite
      .prepare('SELECT version FROM _shelf_migrations')
      .all()
      .map((r: any) => r.version)
  )

  const files = readdirSync(migrationsDir)
    .filter((f) => f.endsWith('.sql'))
    .sort()

  for (const file of files) {
    if (applied.has(file)) continue
    const sqlContent = readFileSync(join(migrationsDir, file), 'utf-8')
    sqlite.exec(sqlContent)
    sqlite.prepare('INSERT INTO _shelf_migrations (version) VALUES (?)').run(file)
    console.log(`[${slug}] migration applied: ${file}`)
  }
}

export function createCoreDB() {
  return createModuleDB('core')
}
