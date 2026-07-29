import { test } from 'node:test'
import assert from 'node:assert/strict'
import Database from 'better-sqlite3'
import { QueryBuilder } from '../src/db/query-builder.js'

interface Item {
  id: number
  name: string
  score: number
  tag: string
}

function setup() {
  const db = new Database(':memory:')
  db.exec(`CREATE TABLE items (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, score INTEGER, tag TEXT)`)
  const qb = () => new QueryBuilder<Item>(db, 'items')
  qb().insert({ name: 'a', score: 10, tag: 'x' })
  qb().insert({ name: 'b', score: 20, tag: 'x' })
  qb().insert({ name: 'c', score: 30, tag: 'y' })
  return qb
}

test('insert returns lastInsertRowid and all() reads rows', () => {
  const qb = setup()
  assert.equal(qb().all().length, 3)
  const id = qb().insert({ name: 'd', score: 40, tag: 'z' })
  assert.equal(id, 4)
})

test('where equality and operator forms', () => {
  const qb = setup()
  assert.equal(qb().where('tag', 'x').count(), 2)
  assert.equal(qb().where('score', '>=', 20).count(), 2)
  assert.equal(qb().where('tag', 'x').where('score', '>', 15).count(), 1)
})

test('whereIn handles values and empty list', () => {
  const qb = setup()
  assert.equal(qb().whereIn('name', ['a', 'c']).count(), 2)
  assert.equal(qb().whereIn('name', []).count(), 0)
})

test('orderBy / limit / offset / first', () => {
  const qb = setup()
  const rows = qb().orderBy('score', 'desc').limit(2).all()
  assert.deepEqual(rows.map((r) => r.name), ['c', 'b'])
  assert.equal(qb().orderBy('score', 'desc').offset(1).limit(1).all()[0].name, 'b')
  assert.equal(qb().orderBy('score').first()?.name, 'a')
})

test('pluck returns single column values', () => {
  const qb = setup()
  assert.deepEqual(qb().where('tag', 'x').orderBy('score').pluck('name'), ['a', 'b'])
})

test('update applies only to where-matched rows', () => {
  const qb = setup()
  const changed = qb().where('tag', 'x').update({ score: 99 })
  assert.equal(changed, 2)
  assert.equal(qb().where('score', 99).count(), 2)
  assert.equal(qb().where('name', 'c').first()?.score, 30)
})

test('delete respects where and returns count', () => {
  const qb = setup()
  assert.equal(qb().where('tag', 'y').delete(), 1)
  assert.equal(qb().count(), 2)
})

test('values are parameter-bound (no injection via values)', () => {
  const qb = setup()
  qb().insert({ name: `x'; DROP TABLE items; --`, score: 1, tag: 'evil' })
  assert.equal(qb().where('tag', 'evil').count(), 1) // 테이블이 살아있음
})
