import type Database from 'better-sqlite3'

export type WhereOp = '=' | '!=' | '<' | '<=' | '>' | '>=' | 'LIKE'
export type OrderDir = 'asc' | 'desc'

/**
 * 체이닝 방식의 타입 안전 쿼리 빌더.
 * 비즈니스 로직에서 SQL 문자열을 직접 다루지 않도록 한다.
 * 컬럼/테이블 이름은 코드에서만 오고(신뢰), 값은 전부 파라미터 바인딩된다.
 *
 *   qb.where('domain', 'a.com').orderBy('created_at', 'desc').limit(10).all()
 */
export class QueryBuilder<T extends Record<string, unknown>> {
  private conditions: string[] = []
  private bindings: unknown[] = []
  private orderClauses: string[] = []
  private limitCount?: number
  private offsetCount?: number

  constructor(
    private readonly db: Database.Database,
    private readonly table: string
  ) {}

  /** where('col', value) 또는 where('col', '>=', value) */
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

  // --- 실행 ---

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

  /** 단일 컬럼 값 배열 */
  pluck<V = unknown>(column: keyof T & string): V[] {
    const { sql, params } = this.buildSelect(String(column))
    return (this.db.prepare(sql).all(...params) as Record<string, V>[]).map((r) => r[column])
  }

  insert(data: Partial<T>): number {
    const keys = Object.keys(data)
    const sql = `INSERT INTO ${this.table} (${keys.join(', ')}) VALUES (${keys.map(() => '?').join(', ')})`
    const result = this.db.prepare(sql).run(...keys.map((k) => data[k]))
    return Number(result.lastInsertRowid)
  }

  /** 현재 where 조건에 해당하는 행 업데이트. 변경된 행 수 반환 */
  update(data: Partial<T>): number {
    const keys = Object.keys(data)
    if (!keys.length) return 0
    const sql = `UPDATE ${this.table} SET ${keys.map((k) => `${k} = ?`).join(', ')}${this.whereSql()}`
    const result = this.db.prepare(sql).run(...keys.map((k) => data[k]), ...this.bindings)
    return result.changes
  }

  /** 현재 where 조건에 해당하는 행 삭제. 삭제된 행 수 반환 */
  delete(): number {
    const result = this.db.prepare(`DELETE FROM ${this.table}${this.whereSql()}`).run(...this.bindings)
    return result.changes
  }

  // --- 내부 ---

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
