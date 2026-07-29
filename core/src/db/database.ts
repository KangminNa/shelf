import BetterSqlite3 from 'better-sqlite3'
import { existsSync, mkdirSync, readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { Repository, type Entity } from './repository.js'

const DATA_DIR = process.env.DATA_DIR || join(process.cwd(), 'data')

export function ensureDataDir(): void {
  if (!existsSync(DATA_DIR)) mkdirSync(DATA_DIR, { recursive: true })
}

export class AppDatabase {
  readonly raw: BetterSqlite3.Database
  private readonly repos = new Map<string, Repository<any>>()

  constructor(scope: string, migrationsDir?: string) {
    ensureDataDir()
    this.raw = new BetterSqlite3(join(DATA_DIR, `${scope}.db`))
    this.raw.pragma('journal_mode = WAL')
    this.raw.pragma('foreign_keys = ON')
    if (migrationsDir && existsSync(migrationsDir)) {
      this.runMigrations(scope, migrationsDir)
    }
  }

  repo<T extends Entity>(table: string): Repository<T> {
    if (!this.repos.has(table)) this.repos.set(table, new Repository<T>(this.raw, table))
    return this.repos.get(table) as Repository<T>
  }

  private runMigrations(scope: string, migrationsDir: string): void {
    this.raw.exec(`
      CREATE TABLE IF NOT EXISTS _shelf_migrations (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        version TEXT NOT NULL UNIQUE,
        applied_at INTEGER DEFAULT (unixepoch())
      )
    `)

    const applied = new Set(
      (this.raw.prepare('SELECT version FROM _shelf_migrations').all() as { version: string }[]).map((r) => r.version)
    )

    const files = readdirSync(migrationsDir).filter((f) => f.endsWith('.sql')).sort()
    for (const file of files) {
      if (applied.has(file)) continue
      this.raw.exec(readFileSync(join(migrationsDir, file), 'utf-8'))
      this.raw.prepare('INSERT INTO _shelf_migrations (version) VALUES (?)').run(file)
      console.log(`[${scope}] migration applied: ${file}`)
    }
  }
}
