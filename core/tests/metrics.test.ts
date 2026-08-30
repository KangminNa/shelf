import { test } from 'node:test'
import assert from 'node:assert/strict'
import { HostMetrics } from '../src/services/host-metrics.js'
import { formatBytes, formatPercent, formatDuration } from '../src/ui/format.js'

test('formatBytes scales to a readable unit', () => {
  assert.equal(formatBytes(512), '512 B')
  assert.equal(formatBytes(1024), '1.0 KB')
  assert.equal(formatBytes(1024 ** 3 * 4), '4.0 GB')
  assert.equal(formatBytes(1024 ** 3 * 460), '460 GB')
})

test('formatPercent says nothing rather than guessing when a value is missing', () => {
  assert.equal(formatPercent(null), '—')
  assert.equal(formatPercent(0), '0%')
  assert.equal(formatPercent(0.187), '19%')
})

test('formatDuration collapses to the two largest units', () => {
  assert.equal(formatDuration(45), '45s')
  assert.equal(formatDuration(3700), '1h 1m')
  assert.equal(formatDuration(90000), '1d 1h')
})

test('a snapshot reports cores, load and uptime from the machine it runs on', () => {
  const metrics = new HostMetrics(process.cwd())
  const snapshot = metrics.snapshot()

  assert.ok(snapshot.cpu.cores > 0, 'a machine has at least one core')
  assert.ok(Number.isFinite(snapshot.cpu.load), 'load average is a number')
  assert.ok(snapshot.uptime > 0, 'uptime counts up from boot')
  assert.ok(snapshot.memory && snapshot.memory.total > 0, 'total memory is known')
  assert.ok(snapshot.memory!.used <= snapshot.memory!.total, 'used memory cannot exceed the total')
})

test('cpu busy is a ratio between two samples, not a cumulative counter', () => {
  const metrics = new HostMetrics(process.cwd())
  const busy = metrics.snapshot().cpu.busy
  assert.ok(busy === null || (busy >= 0 && busy <= 1), `busy must be a 0..1 ratio, got ${busy}`)
})

test('an unreadable disk path reports unknown instead of throwing', () => {
  const metrics = new HostMetrics('/no/such/path/for/shelf-tests')
  assert.equal(metrics.snapshot().disk, null)
})
