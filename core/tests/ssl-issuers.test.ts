import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, rmSync, readFileSync, existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { CertificateStore } from '../src/system/proxy/issuers/certificate-store.js'
import { LetsEncryptIssuer } from '../src/system/proxy/issuers/lets-encrypt.js'
import { SelfSignedIssuer } from '../src/system/proxy/issuers/self-signed.js'
import { ManualUploadIssuer } from '../src/system/proxy/issuers/manual-upload.js'
import { SslError } from '../src/system/proxy/issuers/types.js'
import { SslManager } from '../src/system/proxy/ssl-manager.js'

const noopLog = { info() {}, warn() {}, error() {}, scope: () => noopLog } as any

function tempStore() {
  const dir = mkdtempSync(join(tmpdir(), 'shelf-ssl-'))
  return { dir, store: new CertificateStore(dir) }
}

test('CertificateStore writes a cert/key pair and maps wildcards to a safe dir name', () => {
  const { dir, store } = tempStore()
  try {
    const saved = store.save('example.com', 'CERT', 'KEY')
    assert.equal(readFileSync(saved.certPath, 'utf-8'), 'CERT')
    assert.equal(readFileSync(saved.keyPath, 'utf-8'), 'KEY')

    const wildcard = store.pathsFor('*.example.com')
    assert.ok(wildcard.dir.endsWith('wildcard.example.com'), wildcard.dir)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

test('resolveChallenge defaults to http, forces dns for wildcards', () => {
  assert.equal(LetsEncryptIssuer.resolveChallenge({ domains: ['a.com'] }), 'http')
  assert.equal(LetsEncryptIssuer.resolveChallenge({ domains: ['*.a.com'], dnsToken: 't' }), 'dns')

  assert.throws(
    () => LetsEncryptIssuer.resolveChallenge({ domains: ['*.a.com'], challenge: 'http' }),
    (err: SslError) => err.code === 'VALIDATION'
  )
  assert.throws(
    () => LetsEncryptIssuer.resolveChallenge({ domains: ['a.com'], challenge: 'dns' }),
    (err: SslError) => err.code === 'VALIDATION'
  )
})

test('ManualUploadIssuer rejects non-PEM input and stores valid pairs', () => {
  const { dir, store } = tempStore()
  try {
    const issuer = new ManualUploadIssuer(store)
    assert.throws(() => issuer.accept('a.com', 'not-a-cert', '-----BEGIN PRIVATE KEY-----'), (e: SslError) => e.code === 'VALIDATION')
    assert.throws(() => issuer.accept('a.com', '-----BEGIN CERTIFICATE-----', 'nope'), (e: SslError) => e.code === 'VALIDATION')

    const files = issuer.accept('a.com', '-----BEGIN CERTIFICATE-----\nx', '-----BEGIN PRIVATE KEY-----\ny')
    assert.ok(existsSync(files.certPath))
    assert.equal(files.expiresAt, 0)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

test('SelfSignedIssuer produces a real certificate covering domain and wildcard', async () => {
  const { dir, store } = tempStore()
  try {
    const files = await new SelfSignedIssuer(store, noopLog).issue({ domains: ['shelf.test'] })
    const pem = readFileSync(files.certPath, 'utf-8')
    assert.match(pem, /BEGIN CERTIFICATE/)
    assert.ok(files.expiresAt > Math.floor(Date.now() / 1000))
    assert.equal(CertificateStore.expiryOf(pem), files.expiresAt > 0 ? CertificateStore.expiryOf(pem) : 0)
    assert.deepEqual(SelfSignedIssuer.coveredDomains('shelf.test'), ['shelf.test', '*.shelf.test'])
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

test('normalizeDomains trims, lowercases, and rejects empty input', () => {
  assert.deepEqual(SslManager.normalizeDomains([' A.com ', 'B.COM']), ['a.com', 'b.com'])
  assert.throws(() => SslManager.normalizeDomains([]), (e: SslError) => e.code === 'VALIDATION')
  assert.throws(() => SslManager.normalizeDomains(['  ']), (e: SslError) => e.code === 'VALIDATION')
})
