import { test, afterEach } from 'node:test'
import assert from 'node:assert/strict'
import * as crypto from 'node:crypto'
import { SelfDeployer } from '../src/system/deploy/self-deployer.js'

const noopDocker = { spawnDetached() {} } as any
const noopEvents = { emit() {} } as any
const noopLog = { info() {}, warn() {}, error() {} } as any

function make() {
  return new SelfDeployer(noopDocker, noopEvents, noopLog)
}

function sign(secret: string, body: string): string {
  return 'sha256=' + crypto.createHmac('sha256', secret).update(body).digest('hex')
}

afterEach(() => {
  delete process.env.SELF_DEPLOY_SECRET
  delete process.env.SELF_DEPLOY_BRANCH
})

test('not configured until SELF_DEPLOY_SECRET is set', () => {
  const deployer = make()
  assert.equal(deployer.configured, false)
  process.env.SELF_DEPLOY_SECRET = 'abc'
  assert.equal(deployer.configured, true)
})

test('verify accepts correct HMAC, rejects forged and missing', () => {
  process.env.SELF_DEPLOY_SECRET = 'topsecret'
  const deployer = make()
  const body = Buffer.from(JSON.stringify({ ref: 'refs/heads/main' }))

  assert.equal(deployer.verify(body, sign('topsecret', body.toString())), true)
  assert.equal(deployer.verify(body, 'sha256=deadbeef'), false)
  assert.equal(deployer.verify(body, undefined), false)
  assert.equal(deployer.verify(body, sign('wrongsecret', body.toString())), false)
})

test('matchesBranch honors SELF_DEPLOY_BRANCH', () => {
  process.env.SELF_DEPLOY_SECRET = 's'
  process.env.SELF_DEPLOY_BRANCH = 'release'
  const deployer = make()

  assert.equal(deployer.matchesBranch({ ref: 'refs/heads/release' }), true)
  assert.equal(deployer.matchesBranch({ ref: 'refs/heads/main' }), false)
  assert.equal(deployer.matchesBranch({}), true)
})

test('trigger spawns exactly one detached helper with docker.sock + repo mounts', () => {
  process.env.SELF_DEPLOY_SECRET = 's'
  const calls: Array<{ image: string; script: string; mounts: string[] }> = []
  const docker = { spawnDetached(image: string, script: string, mounts: string[]) { calls.push({ image, script, mounts }) } } as any
  const deployer = new SelfDeployer(docker, noopEvents, noopLog)

  deployer.trigger()

  assert.equal(calls.length, 1)
  assert.equal(calls[0].image, SelfDeployer.HELPER_IMAGE)
  assert.ok(calls[0].mounts.some((m) => m.includes('docker.sock')))
  assert.ok(calls[0].mounts.some((m) => m.endsWith(':/repo')))
  assert.match(calls[0].script, /docker compose .* up -d --build/)
})
