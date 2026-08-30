import { test } from 'node:test'
import assert from 'node:assert/strict'
import { ProxyServer } from '../src/system/proxy/proxy-server.js'
import type { ProxyHost } from '../src/system/proxy/repositories.js'

const host: ProxyHost = {
  id: 1,
  domain: 'app.example.com',
  target_scheme: 'http',
  target_host: 'shelf-app',
  target_port: 9000,
  ssl_enabled: 1,
  force_ssl: 0,
  hsts_enabled: 0,
  hsts_subdomains: 0,
  enabled: 1,
  description: '',
  created_at: 0,
  updated_at: 0,
}

test('Host header is preserved so upstream sees the requested domain', () => {
  const headers = ProxyServer.forwardHeaders({ host: 'app.example.com' }, '1.2.3.4', host, true)
  assert.equal(headers.host, 'app.example.com')
  assert.notEqual(headers.host, 'shelf-app:9000')
  assert.equal(headers['x-forwarded-host'], 'app.example.com')
})

test('falls back to the configured domain when the client sends no Host', () => {
  const headers = ProxyServer.forwardHeaders({}, '1.2.3.4', host, false)
  assert.equal(headers.host, 'app.example.com')
})

test('x-forwarded-proto reflects the client connection, not the upstream scheme', () => {
  assert.equal(ProxyServer.forwardHeaders({ host: 'a' }, '1.2.3.4', host, true)['x-forwarded-proto'], 'https')
  assert.equal(ProxyServer.forwardHeaders({ host: 'a' }, '1.2.3.4', host, false)['x-forwarded-proto'], 'http')
})

test('client ip lands in x-real-ip and appends to an existing forwarded chain', () => {
  const fresh = ProxyServer.forwardHeaders({ host: 'a' }, '1.2.3.4', host, true)
  assert.equal(fresh['x-real-ip'], '1.2.3.4')
  assert.equal(fresh['x-forwarded-for'], '1.2.3.4')

  const chained = ProxyServer.forwardHeaders({ host: 'a', 'x-forwarded-for': '9.9.9.9' }, '1.2.3.4', host, true)
  assert.equal(chained['x-forwarded-for'], '9.9.9.9, 1.2.3.4')
  assert.equal(chained['x-real-ip'], '1.2.3.4', 'x-real-ip is always the directly connecting peer')
})

test('other client headers pass through untouched', () => {
  const headers = ProxyServer.forwardHeaders(
    { host: 'a', cookie: 'session=abc', 'user-agent': 'test-agent' },
    '1.2.3.4',
    host,
    true
  )
  assert.equal(headers.cookie, 'session=abc')
  assert.equal(headers['user-agent'], 'test-agent')
})

test('upsert turns the security flags on and off together', async () => {
  const { ProxyHostRepository } = await import('../src/system/proxy/repositories.js')
  const Database = (await import('better-sqlite3')).default
  const db = new Database(':memory:')
  db.exec(`CREATE TABLE proxy_hosts (
    id INTEGER PRIMARY KEY AUTOINCREMENT, domain TEXT NOT NULL UNIQUE,
    target_scheme TEXT NOT NULL DEFAULT 'http', target_host TEXT NOT NULL, target_port INTEGER NOT NULL,
    ssl_enabled INTEGER NOT NULL DEFAULT 0, force_ssl INTEGER NOT NULL DEFAULT 0,
    hsts_enabled INTEGER NOT NULL DEFAULT 0, hsts_subdomains INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1, description TEXT NOT NULL DEFAULT '',
    created_at INTEGER DEFAULT (unixepoch()), updated_at INTEGER DEFAULT (unixepoch()))`)
  const hosts = new ProxyHostRepository(db)

  const plain = hosts.upsert({ domain: 'admin.test', target_port: 81, secure: false })
  assert.equal(plain.force_ssl, 0, 'no certificate means no redirect to a port that is not listening')

  const secured = hosts.upsert({ domain: 'admin.test', target_port: 81, secure: true })
  assert.equal(secured.ssl_enabled, 1)
  assert.equal(secured.force_ssl, 1)
  assert.equal(secured.hsts_enabled, 1)

  const reverted = hosts.upsert({ domain: 'admin.test', target_port: 81, secure: false })
  assert.equal(reverted.force_ssl, 0, 'losing the certificate must not lock the admin out')

  const untouched = hosts.upsert({ domain: 'admin.test', target_port: 81 })
  assert.equal(untouched.force_ssl, 0, 'omitting secure leaves the flags as they are')
})
