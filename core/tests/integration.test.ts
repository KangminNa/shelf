import { test, before, after } from 'node:test'
import assert from 'node:assert/strict'
import { spawn, type ChildProcess } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import * as crypto from 'node:crypto'

/**
 * 통합 테스트 — 실제 서버를 임시 DATA_DIR로 띄우고 HTTP로 검증한다.
 * Docker가 필요한 배포 실행은 다루지 않는다 (앱 CRUD/인증/webhook 검증까지).
 */

const PORT = 19000 + Math.floor(Math.random() * 500)
const WEBHOOK_PORT = PORT + 500
const BASE = `http://127.0.0.1:${PORT}`
let server: ChildProcess
let dataDir: string
let cookie = ''

async function api(path: string, init: RequestInit = {}, useCookie = true): Promise<Response> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json', ...(init.headers as any) }
  if (useCookie && cookie) headers['Cookie'] = cookie
  return fetch(`${BASE}${path}`, { redirect: 'manual', ...init, headers })
}

before(async () => {
  dataDir = mkdtempSync(join(tmpdir(), 'shelf-test-'))
  server = spawn('npx', ['tsx', 'core/src/index.ts'], {
    cwd: join(import.meta.dirname, '..', '..'),
    env: {
      ...process.env,
      DATA_DIR: dataDir,
      PORT: String(PORT),
      PROXY_HTTP_PORT: String(PORT + 1),
      PROXY_HTTPS_PORT: String(PORT + 2),
      WEBHOOK_PORT: String(WEBHOOK_PORT),
    },
    stdio: 'ignore',
  })

  // 서버 기동 대기 (최대 15초)
  for (let i = 0; i < 75; i++) {
    try {
      const res = await fetch(`${BASE}/health`)
      if (res.ok) return
    } catch {}
    await new Promise((r) => setTimeout(r, 200))
  }
  throw new Error('server did not start')
})

after(() => {
  server?.kill('SIGKILL')
  rmSync(dataDir, { recursive: true, force: true })
})

test('unauthenticated: pages redirect to /setup, APIs return 401', async () => {
  const page = await api('/admin', {}, false)
  assert.equal(page.status, 302)
  assert.match(page.headers.get('location') || '', /\/setup$/)

  const apiRes = await api('/api/deploy/projects', {}, false)
  assert.equal(apiRes.status, 401)
})

test('setup rejects weak passwords and bad usernames', async () => {
  const short = await api('/api/auth/setup', { method: 'POST', body: JSON.stringify({ username: 'admin', password: 'short' }) })
  assert.equal(short.status, 400)
  const badName = await api('/api/auth/setup', { method: 'POST', body: JSON.stringify({ username: 'a b!', password: 'long-enough-pw' }) })
  assert.equal(badName.status, 400)
})

test('setup creates admin, issues session, and locks itself', async () => {
  const res = await api('/api/auth/setup', { method: 'POST', body: JSON.stringify({ username: 'admin', password: 'integration-pw-1' }) })
  assert.equal(res.status, 201)
  cookie = (res.headers.get('set-cookie') || '').split(';')[0]
  assert.match(cookie, /^shelf_session=/)

  const again = await api('/api/auth/setup', { method: 'POST', body: JSON.stringify({ username: 'evil', password: 'evil-password-1' }) }, false)
  assert.equal(again.status, 403)
})

test('session grants access; login validates credentials', async () => {
  const admin = await api('/admin')
  assert.equal(admin.status, 200)

  const bad = await api('/api/auth/login', { method: 'POST', body: JSON.stringify({ username: 'admin', password: 'wrong' }) }, false)
  assert.equal(bad.status, 401)

  const good = await api('/api/auth/login', { method: 'POST', body: JSON.stringify({ username: 'admin', password: 'integration-pw-1' }) }, false)
  assert.equal(good.status, 200)
})

test('app CRUD: create, secrets hidden, patch, validation', async () => {
  const created = await api('/api/deploy/projects', {
    method: 'POST',
    body: JSON.stringify({ name: 'itest', source_type: 'git', repo_url: 'https://example.com/r.git', port: 18999, container_port: 3000, git_token: 'ghp_secret_xyz' }),
  })
  assert.equal(created.status, 201)
  const id = (await created.json() as any).data.id

  // 목록/단건 응답에 시크릿이 없어야 한다
  const list = await api('/api/deploy/projects')
  const text = JSON.stringify(await list.json())
  assert.ok(!text.includes('ghp_secret_xyz'), 'git_token must not leak')
  assert.ok(text.includes('"has_token":true'))

  const patched = await api(`/api/deploy/projects/${id}`, { method: 'PATCH', body: JSON.stringify({ domain: 'itest.local' }) })
  const patchedBody = await patched.json() as any
  assert.equal(patchedBody.data.domain, 'itest.local')
  assert.ok(!JSON.stringify(patchedBody).includes('ghp_secret_xyz'))

  const dup = await api('/api/deploy/projects', {
    method: 'POST',
    body: JSON.stringify({ name: 'itest', source_type: 'git', repo_url: 'https://example.com/r.git' }),
  })
  assert.equal(dup.status, 409)

  const noRepo = await api('/api/deploy/projects', { method: 'POST', body: JSON.stringify({ name: 'x2', source_type: 'git' }) })
  assert.equal(noRepo.status, 400)

  // 셸 메타문자가 든 저장소 URL/브랜치는 거부 (명령 주입 방어)
  const evilUrl = await api('/api/deploy/projects', {
    method: 'POST',
    body: JSON.stringify({ name: 'x3', source_type: 'git', repo_url: 'https://example.com/r.git; rm -rf /' }),
  })
  assert.equal(evilUrl.status, 400)
  const evilBranch = await api('/api/deploy/projects', {
    method: 'POST',
    body: JSON.stringify({ name: 'x4', source_type: 'git', repo_url: 'https://example.com/r.git', branch: 'main`whoami`' }),
  })
  assert.equal(evilBranch.status, 400)

  const del = await api(`/api/deploy/projects/${id}`, { method: 'DELETE' })
  assert.equal(del.status, 200)
})

test('webhook server rejects bad signatures, accepts valid HMAC', async () => {
  const created = await api('/api/deploy/projects', {
    method: 'POST',
    body: JSON.stringify({ name: 'hooked', source_type: 'git', repo_url: 'https://example.com/r.git', auto_deploy: false }),
  })
  const { id, webhook_secret } = (await created.json() as any).data

  const hookBase = `http://127.0.0.1:${WEBHOOK_PORT}`
  const body = JSON.stringify({ ref: 'refs/heads/main' })

  const unsigned = await fetch(`${hookBase}/hooks/${id}`, { method: 'POST', body })
  assert.equal(unsigned.status, 401)

  const forged = await fetch(`${hookBase}/hooks/${id}`, {
    method: 'POST',
    headers: { 'X-Hub-Signature-256': 'sha256=deadbeef' },
    body,
  })
  assert.equal(forged.status, 401)

  const sig = 'sha256=' + crypto.createHmac('sha256', webhook_secret).update(body).digest('hex')
  const valid = await fetch(`${hookBase}/hooks/${id}`, {
    method: 'POST',
    headers: { 'X-Hub-Signature-256': sig },
    body,
  })
  // auto_deploy=false → 서명은 통과하고 배포는 건너뛴다
  assert.equal(valid.status, 200)
  assert.match((await valid.json() as any).message, /Auto-deploy disabled/)

  await api(`/api/deploy/projects/${id}`, { method: 'DELETE' })
})

// 주의: 이 테스트는 IP 잠금을 걸므로 반드시 마지막에 둔다
test('login rate limit locks after repeated failures', async () => {
  for (let i = 0; i < 5; i++) {
    const res = await api('/api/auth/login', { method: 'POST', body: JSON.stringify({ username: 'admin', password: 'wrong-' + i }) }, false)
    assert.equal(res.status, 401)
  }
  const locked = await api('/api/auth/login', { method: 'POST', body: JSON.stringify({ username: 'admin', password: 'integration-pw-1' }) }, false)
  assert.equal(locked.status, 429)
})

test('proxy host CRUD and rollback guard', async () => {
  const host = await api('/api/proxy/hosts', {
    method: 'POST',
    body: JSON.stringify({ domain: 'itest.example.com', target_host: '127.0.0.1', target_port: 18990 }),
  })
  assert.equal(host.status, 201)
  const hostId = (await host.json() as any).data.id

  const toggled = await api(`/api/proxy/hosts/${hostId}/toggle`, { method: 'POST' })
  assert.equal(((await toggled.json()) as any).data.enabled, 0)

  const rollback = await api('/api/deploy/deployments/99999/rollback', { method: 'POST' })
  assert.equal(rollback.status, 404)

  await api(`/api/proxy/hosts/${hostId}`, { method: 'DELETE' })
})
