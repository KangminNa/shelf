import type Database from 'better-sqlite3'
import { QueryBuilder } from './query-builder.js'

export interface Entity {
  id: number
}

export class Repository<T extends Entity> {
  private readonly hasUpdatedAt: boolean

  constructor(
    protected readonly db: Database.Database,
    readonly table: string
  ) {
    const columns = db.prepare(`PRAGMA table_info(${table})`).all() as { name: string }[]
    this.hasUpdatedAt = columns.some((c) => c.name === 'updated_at')
  }

  query(): QueryBuilder<T> {
    return new QueryBuilder<T>(this.db, this.table)
  }

  all(): T[] {
    return this.query().all()
  }

  find(id: number | string): T | undefined {
    return this.query().where('id', Number(id)).first()
  }

  findBy(criteria: Partial<T>): T | undefined {
    return this.applyCriteria(criteria).first()
  }

  findAllBy(criteria: Partial<T>): T[] {
    return this.applyCriteria(criteria).all()
  }

  count(): number {
    return this.query().count()
  }

  create(data: Partial<T>): T {
    const id = this.query().insert(data)
    return this.find(id) as T
  }

  update(id: number | string, data: Partial<T>): T | undefined {
    const payload: Record<string, unknown> = { ...data }
    if (this.hasUpdatedAt && payload.updated_at === undefined) {
      payload.updated_at = Math.floor(Date.now() / 1000)
    }
    this.query().where('id', Number(id)).update(payload as Partial<T>)
    return this.find(id)
  }

  delete(id: number | string): boolean {
    return this.query().where('id', Number(id)).delete() > 0
  }

  private applyCriteria(criteria: Partial<T>): QueryBuilder<T> {
    const qb = this.query()
    for (const [key, value] of Object.entries(criteria)) {
      qb.where(key as keyof T & string, value)
    }
    return qb
  }
}
