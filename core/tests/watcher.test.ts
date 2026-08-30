import { test } from 'node:test'
import assert from 'node:assert/strict'
import { AppWatcher } from '../src/system/deploy/app-watcher.js'
import { EventBus } from '../src/services/events.js'

const PROJECT = { id: 1, name: 'blog', domain: 'blog.test' } as any
const silent = { warn() {}, info() {}, error() {} } as any

function watcherOn(statuses: string[]) {
  const seen: Array<{ event: string; name: string }> = []
  const events = new EventBus()
  for (const event of ['monitor:app-down', 'monitor:app-recovered']) {
    events.on(event, (p: any) => seen.push({ event, name: p.name }))
  }
  let step = 0
  const containers = {
    statuses: async (projects: any[]) => {
      const status = statuses[Math.min(step++, statuses.length - 1)]
      return new Map(projects.map((p) => [p.id, status]))
    },
  } as any
  const projects = { all: () => [PROJECT] } as any
  return { watcher: new AppWatcher(projects, containers, events, silent), seen, events }
}

async function run(watcher: AppWatcher, times: number) {
  for (let i = 0; i < times; i += 1) await watcher.check()
}

test('a healthy app that stays healthy never notifies', async () => {
  const { watcher, seen } = watcherOn(['running', 'running', 'running'])
  await run(watcher, 3)
  assert.deepEqual(seen, [])
})

test('a crash notifies once, not on every poll', async () => {
  const { watcher, seen } = watcherOn(['running', 'crashed', 'crashed', 'crashed'])
  await run(watcher, 4)
  assert.deepEqual(seen, [{ event: 'monitor:app-down', name: 'blog' }])
})

test('recovery notifies once after a crash', async () => {
  const { watcher, seen } = watcherOn(['running', 'crashed', 'running', 'running'])
  await run(watcher, 4)
  assert.deepEqual(seen.map((s) => s.event), ['monitor:app-down', 'monitor:app-recovered'])
})

test('an app already crashed when Shelf first looks is still reported', async () => {
  const { watcher, seen } = watcherOn(['crashed', 'crashed'])
  await run(watcher, 2)
  assert.deepEqual(seen, [{ event: 'monitor:app-down', name: 'blog' }], 'a crash present at startup must not be swallowed')
})

test('an app that was cleanly stopped before Shelf started is not an incident', async () => {
  const { watcher, seen } = watcherOn(['stopped', 'stopped'])
  await run(watcher, 2)
  assert.deepEqual(seen, [], 'exit code 0 means someone turned it off')
})

test('stopping an app on purpose silences it until it is started again', async () => {
  const { watcher, seen, events } = watcherOn(['running', 'stopped', 'stopped', 'running'])
  await watcher.check()
  events.emit('deploy:container-stopped', { projectId: PROJECT.id })
  await run(watcher, 2)
  assert.deepEqual(seen, [], 'a deliberate stop is not a failure')

  events.emit('deploy:container-started', { projectId: PROJECT.id })
  await watcher.check()
  assert.deepEqual(seen, [], 'watching resumes quietly from the new baseline')
})

test('an app that was never deployed is skipped', async () => {
  const { watcher, seen } = watcherOn(['none', 'none'])
  await run(watcher, 2)
  assert.deepEqual(seen, [])
})
