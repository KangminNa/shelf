import { test } from 'node:test'
import assert from 'node:assert/strict'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { AuthSystem } from '../src/system/auth/index.js'

test('hashPassword produces salt:hash and verifies round-trip', () => {
  const stored = AuthSystem.hashPassword('correct horse battery staple')
  assert.match(stored, /^[0-9a-f]{32}:[0-9a-f]{128}$/)
  assert.equal(AuthSystem.verifyPassword('correct horse battery staple', stored), true)
})

test('wrong password fails verification', () => {
  const stored = AuthSystem.hashPassword('password-one')
  assert.equal(AuthSystem.verifyPassword('password-two', stored), false)
  assert.equal(AuthSystem.verifyPassword('', stored), false)
})

test('same password hashes differently per salt', () => {
  const a = AuthSystem.hashPassword('same')
  const b = AuthSystem.hashPassword('same')
  assert.notEqual(a, b)
  assert.equal(AuthSystem.verifyPassword('same', a), true)
  assert.equal(AuthSystem.verifyPassword('same', b), true)
})

test('malformed stored hash never verifies', () => {
  assert.equal(AuthSystem.verifyPassword('x', ''), false)
  assert.equal(AuthSystem.verifyPassword('x', 'no-colon'), false)
  assert.equal(AuthSystem.verifyPassword('x', 'deadbeef:zznothex'), false)
})

test('account recovery: password change revokes sessions, reset reopens setup', async (t) => {
  const dir = await import('node:fs/promises').then((fs) =>
    fs.mkdtemp(join(tmpdir(), 'shelf-auth-'))
  )
  const previous = process.env.DATA_DIR
  process.env.DATA_DIR = dir
  t.after(async () => {
    process.env.DATA_DIR = previous
    await import('node:fs/promises').then((fs) => fs.rm(dir, { recursive: true, force: true }))
  })

  const auth = new AuthSystem()

  assert.equal(auth.needsSetup, true)
  assert.deepEqual(auth.accounts, [])

  auth.createAccount('admin', 'first-password')
  assert.deepEqual(auth.accounts, ['admin'])
  assert.equal(auth.needsSetup, false)

  assert.equal(auth.setPassword('nobody', 'whatever-123'), false)
  assert.equal(auth.setPassword('admin', 'second-password'), true)

  auth.forgetEveryone()
  assert.deepEqual(auth.accounts, [])
  assert.equal(auth.needsSetup, true, 'reset must reopen /setup')
})
