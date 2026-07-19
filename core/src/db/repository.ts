import type Database from 'better-sqlite3'
import { QueryBuilder } from './query-builder.js'

export interface Entity {
  id: number
}

/**
 * 테이블 하나를 담당하는 리포지토리 베이스 클래스.
 * 도메인 리포지토리는 이 클래스를 상속해 의미 있는 메서드를 추가한다.
 *
 *   class PostRepository extends Repository<Post> {
 *     constructor(db) { super(db, 'posts') }
 *     published() { return this.query().where('published', 1).all() }
 *   }
 *
 * updated_at 컬럼이 있는 테이블은 update() 시 자동 갱신된다.
 */
export class Repository<T extends Entity> {
  private readonly hasUpdatedAt: boolean

  constructor(
    protected readonly db: Database.Database,
    readonly table: string
  ) {
    const columns = db.prepare(`PRAGMA table_info(${table})`).all() as { name: string }[]
    this.hasUpdatedAt = columns.some((c) => c.name === 'updated_at')
  }

  /** 체이닝 쿼리 시작점 */
  query(): QueryBuilder<T> {
    return new QueryBuilder<T>(this.db, this.table)
  }

  all(): T[] {
    return this.query().all()
  }

  find(id: number | string): T | undefined {
    return this.query().where('id', Number(id)).first()
  }

  /** 단일 조건 일치 첫 행 */
  findBy(criteria: Partial<T>): T | undefined {
    return this.applyCriteria(criteria).first()
  }

  /** 조건 일치 전체 */
  findAllBy(criteria: Partial<T>): T[] {
    return this.applyCriteria(criteria).all()
  }

  count(): number {
    return this.query().count()
  }

  /** 삽입 후 생성된 행 반환 */
  create(data: Partial<T>): T {
    const id = this.query().insert(data)
    return this.find(id) as T
  }

  /** 부분 업데이트 후 갱신된 행 반환 (updated_at 자동) */
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
