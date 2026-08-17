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
