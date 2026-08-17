import { test } from 'node:test'
import assert from 'node:assert/strict'
import { el, raw, join, element, escapeHtml } from '../src/ui/element.js'

test('text children are escaped automatically', () => {
  assert.equal(el.div('<script>alert(1)</script>').toString(), '<div>&lt;script&gt;alert(1)&lt;/script&gt;</div>')
  assert.equal(el.span('a & b').toString(), '<span>a &amp; b</span>')
})

test('attribute values are escaped', () => {
  assert.equal(el.a({ href: '/x?a=1&b=2' }, 'go').toString(), '<a href="/x?a=1&amp;b=2">go</a>')
  assert.equal(el.div({ title: '" onload="evil()' }).toString(), '<div title="&quot; onload=&quot;evil()"></div>')
})

test('raw() opts out of escaping for trusted markup', () => {
  assert.equal(el.div(raw('<b>bold</b>')).toString(), '<div><b>bold</b></div>')
})

test('null/undefined/false attributes and children are omitted', () => {
  assert.equal(el.button({ disabled: false, class: 'btn' }, 'ok').toString(), '<button class="btn">ok</button>')
  assert.equal(el.button({ disabled: true }, 'ok').toString(), '<button disabled>ok</button>')
  assert.equal(el.div(null, undefined, false, 'only').toString(), '<div>only</div>')
})

test('children can be nested arrays and elements', () => {
  const list = el.ul({}, [el.li('a'), el.li('b')])
  assert.equal(list.toString(), '<ul><li>a</li><li>b</li></ul>')
})

test('first argument may be a child instead of attrs', () => {
  assert.equal(el.p('text').toString(), '<p>text</p>')
  assert.equal(el.p({ class: 'c' }, 'text').toString(), '<p class="c">text</p>')
})

test('void tags render without a closing tag', () => {
  assert.equal(el.input({ type: 'text', name: 'q' }).toString(), '<input type="text" name="q">')
})

test('join concatenates children with an optional separator', () => {
  assert.equal(join([el.span('a'), el.span('b')]).toString(), '<span>a</span><span>b</span>')
  assert.equal(join(['a', 'b'], ', ').toString(), 'a, b')
})

test('element() supports arbitrary tags', () => {
  assert.equal(element('custom-tag', { id: 'x' }, 'hi').toString(), '<custom-tag id="x">hi</custom-tag>')
})

test('escapeHtml handles nullish input', () => {
  assert.equal(escapeHtml(null), '')
  assert.equal(escapeHtml(undefined), '')
  assert.equal(escapeHtml(0), '0')
})

test('Page (builder-based) still escapes string children', async () => {
  const { Page } = await import('../src/ui/page.js')
  class Modern extends Page {
    render() {
      return this.sectionHeader('Title', '<script>x</script>').toString()
    }
  }
  assert.match(new Modern().render(), /&lt;script&gt;/)
})
