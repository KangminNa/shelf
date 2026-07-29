import { test } from 'node:test'
import assert from 'node:assert/strict'
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
