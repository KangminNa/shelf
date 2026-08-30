import { test } from 'node:test'
import assert from 'node:assert/strict'
import * as crypto from 'node:crypto'
import { WebhookHandler } from '../src/system/deploy/webhook-handler.js'

const SECRET = 'test-webhook-secret'
const PROJECT = { id: 7, name: 'blog', branch: 'main', auto_deploy: 1, webhook_secret: SECRET } as any

function sign(body: string, secret = SECRET): string {
  return 'sha256=' + crypto.createHmac('sha256', secret).update(Buffer.from(body)).digest('hex')
}

function handlerFor(project: any = PROJECT) {
  const deployed: string[] = []
  const handler = new WebhookHandler(
    { find: (id: number) => (project && Number(id) === project.id ? project : undefined) } as any,
    { deploy: (p: any, trigger: string) => { deployed.push(`${p.name}:${trigger}`); return Promise.resolve({ ok: true }) } } as any,
    { configured: false } as any,
    { warn() {}, info() {} } as any
  )
  return { handler, deployed }
}

function push(body: string, signature?: string) {
  return { path: '/7', body: Buffer.from(body), signature }
}

test('a correctly signed push to the tracked branch deploys', () => {
  const { handler, deployed } = handlerFor()
  const body = '{"ref":"refs/heads/main"}'
  const reply = handler.handle(push(body, sign(body)))
  assert.equal(reply.status, 202)
  assert.deepEqual(deployed, ['blog:webhook'])
})

test('missing, forged, and wrong-secret signatures are all rejected without deploying', () => {
  const body = '{"ref":"refs/heads/main"}'
  for (const signature of [undefined, 'sha256=deadbeef', sign(body, 'other-secret')]) {
    const { handler, deployed } = handlerFor()
    assert.equal(handler.handle(push(body, signature)).status, 401)
    assert.deepEqual(deployed, [], 'rejected webhook must not deploy')
  }
})

test('a signature bound to one body does not authorize a different body', () => {
  const { handler, deployed } = handlerFor()
  const signature = sign('{"ref":"refs/heads/main"}')
  const reply = handler.handle(push('{"ref":"refs/heads/main","injected":true}', signature))
  assert.equal(reply.status, 401)
  assert.deepEqual(deployed, [])
})

test('pushes to other branches are acknowledged but ignored', () => {
  const { handler, deployed } = handlerFor()
  const body = '{"ref":"refs/heads/dev"}'
  const reply = handler.handle(push(body, sign(body)))
  assert.equal(reply.status, 200)
  assert.match(reply.body.message!, /Ignoring push/)
  assert.deepEqual(deployed, [])
})

test('auto_deploy off means a valid push is acknowledged but not deployed', () => {
  const { handler, deployed } = handlerFor({ ...PROJECT, auto_deploy: 0 })
  const body = '{"ref":"refs/heads/main"}'
  assert.equal(handler.handle(push(body, sign(body))).status, 200)
  assert.deepEqual(deployed, [])
})

test('unknown project and unknown path are not found', () => {
  const { handler } = handlerFor()
  assert.equal(handler.handle({ path: '/999', body: Buffer.from('{}'), signature: sign('{}') }).status, 404)
  assert.equal(handler.handle({ path: '/nonsense', body: Buffer.from('{}') }).status, 404)
})

test('self-deploy stays closed until SELF_DEPLOY_SECRET is configured', () => {
  const { handler } = handlerFor()
  assert.equal(handler.handle({ path: '/self', body: Buffer.from('{}') }).status, 404)
})

test('gitlab token and secret query param authorize the same way', () => {
  const body = '{"ref":"refs/heads/main"}'
  const viaToken = handlerFor()
  assert.equal(viaToken.handler.handle({ path: '/7', body: Buffer.from(body), gitlabToken: SECRET }).status, 202)

  const viaQuery = handlerFor()
  assert.equal(viaQuery.handler.handle({ path: '/7', body: Buffer.from(body), secretParam: SECRET }).status, 202)

  const wrong = handlerFor()
  assert.equal(wrong.handler.handle({ path: '/7', body: Buffer.from(body), gitlabToken: 'nope' }).status, 401)
  assert.deepEqual(wrong.deployed, [])
})
