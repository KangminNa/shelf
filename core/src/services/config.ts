import Database from 'better-sqlite3'
import { join } from 'path'
import { existsSync, mkdirSync } from 'fs'
import type { ConfigService } from '../types.js'

const DATA_DIR = join(process.cwd(), 'data')

export function createConfigService(slug: string): ConfigService {
  if (!existsSync(DATA_DIR)) mkdirSync(DATA_DIR, { recursive: true })

  const dbPath = join(DATA_DIR, 'core.db')
  const db = new Database(dbPath)

  db.exec(`
    CREATE TABLE IF NOT EXISTS module_config (
      module TEXT NOT NULL,
      key TEXT NOT NULL,
      value TEXT,
      PRIMARY KEY (module, key)
    )
  `)

  return {
    async get<T = string>(key: string, defaultValue?: T): Promise<T> {
      const row = db
        .prepare('SELECT value FROM module_config WHERE module = ? AND key = ?')
        .get(slug, key) as any
      if (!row) return defaultValue as T
      try {
        return JSON.parse(row.value)
      } catch {
        return row.value as T
      }
    },

    async set(key: string, value: any) {
      const serialized = typeof value === 'string' ? value : JSON.stringify(value)
      db.prepare(
        'INSERT OR REPLACE INTO module_config (module, key, value) VALUES (?, ?, ?)'
      ).run(slug, key, serialized)
    },

    async getAll() {
      const rows = db
        .prepare('SELECT key, value FROM module_config WHERE module = ?')
        .all(slug) as any[]
      const result: Record<string, any> = {}
      for (const row of rows) {
        try {
          result[row.key] = JSON.parse(row.value)
        } catch {
          result[row.key] = row.value
        }
      }
      return result
    },
  }
}
