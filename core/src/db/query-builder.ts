import type Database from 'better-sqlite3'

export type WhereOp = '=' | '!=' | '<' | '<=' | '>' | '>=' | 'LIKE'
export type OrderDir = 'asc' | 'desc'

export class QueryBuilder<T extends object> {
  private conditions: string[] = []
  private bindings: unknown[] = []
  private orderClauses: string[] = []
  private limitCount?: number
  private offsetCount?: number

  constructor(
    private readonly db: Database.Database,
    private readonly table: string
  ) {}

  where(column: keyof T & string, opOrValue: WhereOp | unknown, value?: unknown): this {
    if (value === undefined && !isWhereOp(opOrValue)) {
      this.conditions.push(`${column} = ?`)
      this.bindings.push(opOrValue)
    } else {
      this.conditions.push(`${column} ${opOrValue} ?`)
      this.bindings.push(value)
    }
    return this
  }

  whereIn(column: keyof T & string, values: readonly unknown[]): this {
    if (!values.length) {
      this.conditions.push('0 = 1')
      return this
    }
    this.conditions.push(`${column} IN (${values.map(() => '?').join(', ')})`)
    this.bindings.push(...values)
    return this
  }

  whereNull(column: keyof T & string): this {
    this.conditions.push(`${column} IS NULL`)
    return this
  }

  orderBy(column: keyof T & string, direction: OrderDir = 'asc'): this {
    this.orderClauses.push(`${column} ${direction.toUpperCase()}`)
    return this
  }

  limit(n: number): this {
    this.limitCount = n
    return this
  }

  offset(n: number): this {
    this.offsetCount = n
    return this
  }

  all(): T[] {
    const { sql, params } = this.buildSelect('*')
    return this.db.prepare(sql).all(...params) as T[]
  }

  first(): T | undefined {
    this.limitCount = 1
    return this.all()[0]
  }

  count(): number {
    const { sql, params } = this.buildSelect('count(*) as c', { skipOrderLimit: true })
    const row = this.db.prepare(sql).get(...params) as { c: number }
    return row.c
  }

  pluck<V = unknown>(column: keyof T & string): V[] {
    const { sql, params } = this.buildSelect(String(column))
    return (this.db.prepare(sql).all(...params) as Record<string, V>[]).map((r) => r[column])
  }

  insert(data: Partial<T>): number {
    const record = data as Record<string, unknown>
    const keys = Object.keys(record)
    const sql = `INSERT INTO ${this.table} (${keys.join(', ')}) VALUES (${keys.map(() => '?').join(', ')})`
    const result = this.db.prepare(sql).run(...keys.map((k) => record[k]))
    return Number(result.lastInsertRowid)
  }

  update(data: Partial<T>): number {
    const record = data as Record<string, unknown>
    const keys = Object.keys(record)
    if (!keys.length) return 0
    const sql = `UPDATE ${this.table} SET ${keys.map((k) => `${k} = ?`).join(', ')}${this.whereSql()}`
    const result = this.db.prepare(sql).run(...keys.map((k) => record[k]), ...this.bindings)
    return result.changes
  }

  delete(): number {
    const result = this.db.prepare(`DELETE FROM ${this.table}${this.whereSql()}`).run(...this.bindings)
    return result.changes
  }

  private whereSql(): string {
    return this.conditions.length ? ` WHERE ${this.conditions.join(' AND ')}` : ''
  }

  private buildSelect(columns: string, opts: { skipOrderLimit?: boolean } = {}) {
    let sql = `SELECT ${columns} FROM ${this.table}${this.whereSql()}`
    const params = [...this.bindings]
    if (!opts.skipOrderLimit) {
      if (this.orderClauses.length) sql += ` ORDER BY ${this.orderClauses.join(', ')}`
      if (this.limitCount !== undefined) sql += ` LIMIT ${this.limitCount}`
      if (this.offsetCount !== undefined) sql += ` OFFSET ${this.offsetCount}`
    }
    return { sql, params }
  }
}

const WHERE_OPS: readonly string[] = ['=', '!=', '<', '<=', '>', '>=', 'LIKE']
function isWhereOp(v: unknown): v is WhereOp {
  return typeof v === 'string' && WHERE_OPS.includes(v)
}
