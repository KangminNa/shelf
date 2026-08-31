import { test } from 'node:test'
import assert from 'node:assert/strict'
import { act, submits, loads, live, tab, panel, matches, openDialog, RUNTIME_SCRIPT } from '../src/ui/runtime.js'
import { attrs } from '../src/ui/element.js'

test('act normalizes the method and drops options that were not given', () => {
  assert.deepEqual(act('post', '/api/x'), {
    'data-act': 'POST /api/x',
    'data-confirm': undefined,
    'data-busy': undefined,
    'data-then': undefined,
  })
  assert.equal(attrs(act('delete', '/api/x', { confirm: 'Sure?' })), 'data-act="DELETE /api/x" data-confirm="Sure?"')
})

test('submits marks the form so the runtime sends the body instead of navigating', () => {
  const rendered = attrs(submits('PATCH', '/api/x', { then: 'redirect:/done' }))
  assert.match(rendered, /data-act="PATCH \/api\/x"/)
  assert.match(rendered, /data-then="redirect:\/done"/)
  assert.match(rendered, /data-form=""/)
})

test('loads fetches into another element and can open a dialog with the result', () => {
  const rendered = attrs(loads('/api/deployments/3', { into: '#log', pick: 'log', open: 'log-dialog', empty: '(empty)' }))
  assert.match(rendered, /data-load="\/api\/deployments\/3"/)
  assert.match(rendered, /data-into="#log"/)
  assert.match(rendered, /data-open="log-dialog"/)
})

test('live turns an element into a self-refreshing view of an endpoint', () => {
  const rendered = attrs(live('/api/logs', { pick: 'logs', every: 5000 }))
  assert.match(rendered, /data-live="\/api\/logs"/)
  assert.match(rendered, /data-every="5000"/)
})

test('tab and panel pair by group so only the selected panel is visible', () => {
  assert.equal(tab('source', 'git', true).class, 'shelf-btn shelf-btn-sm active')
  assert.equal(tab('source', 'image').class, 'shelf-btn shelf-btn-sm')
  assert.deepEqual(panel('source', 'git', true), { 'data-panel': 'source:git', hidden: false })
  assert.deepEqual(panel('source', 'image'), { 'data-panel': 'source:image', hidden: true })
  assert.equal(attrs(panel('source', 'image')), 'data-panel="source:image" hidden')
})

test('matches blocks submission when two fields disagree', () => {
  assert.deepEqual(matches('[name=password]', 'Passwords do not match'), {
    'data-match': '[name=password]',
    'data-match-message': 'Passwords do not match',
  })
})

test('openDialog is the only way a view opens a dialog', () => {
  assert.deepEqual(openDialog('add-dialog'), { 'data-open': 'add-dialog' })
})

test('the runtime script handles every declared attribute', () => {
  const handled = ['data-act', 'data-load', 'data-live', 'data-copy', 'data-fill', 'data-tab', 'data-open', 'data-close', 'data-reveal', 'data-match']
  for (const attribute of handled) {
    assert.ok(RUNTIME_SCRIPT.includes(attribute), `runtime ignores ${attribute}`)
  }
  assert.ok(RUNTIME_SCRIPT.includes('omitEmpty'), 'runtime ignores data-omit-empty')
})

test('a failed refresh keeps the surrounding page instead of replacing it with the error', () => {
  const refresh = RUNTIME_SCRIPT.slice(RUNTIME_SCRIPT.indexOf('const refresh'), RUNTIME_SCRIPT.indexOf('const select'))
  assert.match(refresh, /if \(!fields\.length\) fill\(el, '', err\.message\)/,
    'only a field-less live element may be overwritten by its own error')
  assert.doesNotMatch(
    refresh.slice(refresh.indexOf('catch')),
    /^\s*fill\(el, '', err\.message\);\s*$/m,
    'an unconditional fill in the catch would wipe every field it contains'
  )
})
