import { test } from 'node:test'
import assert from 'node:assert/strict'
import Database from 'better-sqlite3'
import { Repository } from '../src/db/repository.js'

interface Post {
  id: number
  title: string
  published: number
  created_at: number
  updated_at: number
}

function setup() {
  const db = new Database(':memory:')
  db.exec(`CREATE TABLE posts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    published INTEGER DEFAULT 0,
    created_at INTEGER DEFAULT (unixepoch()),
    updated_at INTEGER DEFAULT (unixepoch())
  )`)
  db.exec(`CREATE TABLE tags (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)`)
  return db
}

test('create returns the inserted row', () => {
  const repo = new Repository<Post>(setup(), 'posts')
  const post = repo.create({ title: 'hello', published: 1 })
  assert.equal(post.id, 1)
  assert.equal(post.title, 'hello')
  assert.ok(post.created_at > 0)
})

test('find / findBy / findAllBy / count', () => {
  const repo = new Repository<Post>(setup(), 'posts')
  repo.create({ title: 'a', published: 1 })
  repo.create({ title: 'b', published: 0 })
  repo.create({ title: 'c', published: 1 })

  assert.equal(repo.find(2)?.title, 'b')
  assert.equal(repo.find(999), undefined)
  assert.equal(repo.findBy({ title: 'c' } as Partial<Post>)?.id, 3)
  assert.equal(repo.findAllBy({ published: 1 } as Partial<Post>).length, 2)
  assert.equal(repo.count(), 3)
})

test('update patches fields and auto-bumps updated_at', async () => {
  const db = setup()
  const repo = new Repository<Post>(db, 'posts')
  const post = repo.create({ title: 'old' })
  // updated_at이 확실히 달라지도록 과거로 밀어둔다
  db.prepare('UPDATE posts SET updated_at = 1000 WHERE id = ?').run(post.id)

  const updated = repo.update(post.id, { title: 'new' })
  assert.equal(updated?.title, 'new')
  assert.ok(updated!.updated_at > 1000, 'updated_at should be auto-refreshed')
})

test('update on a table without updated_at does not fail', () => {
  const repo = new Repository<{ id: number; name: string }>(setup(), 'tags')
  const tag = repo.create({ name: 'x' })
  assert.equal(repo.update(tag.id, { name: 'y' })?.name, 'y')
})

test('delete returns boolean by existence', () => {
  const repo = new Repository<Post>(setup(), 'posts')
  const post = repo.create({ title: 'bye' })
  assert.equal(repo.delete(post.id), true)
  assert.equal(repo.delete(post.id), false)
  assert.equal(repo.count(), 0)
})
