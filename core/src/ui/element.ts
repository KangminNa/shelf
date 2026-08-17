const HTML_ESCAPES: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}

const VOID_TAGS = new Set(['area', 'br', 'col', 'hr', 'img', 'input', 'link', 'meta', 'source'])

export class Html {
  constructor(readonly value: string) {}

  toString(): string {
    return this.value
  }
}

export type Child = Html | string | number | null | undefined | false | Child[]

export type Attrs = Record<string, string | number | boolean | null | undefined>

export function escapeHtml(value: unknown): string {
  return String(value ?? '').replace(/[&<>"']/g, (ch) => HTML_ESCAPES[ch])
}

export function raw(html: string): Html {
  return new Html(html)
}

export function join(children: Child[], separator = ''): Html {
  return raw(children.map(renderChild).join(separator))
}

function renderChild(child: Child): string {
  if (child === null || child === undefined || child === false) return ''
  if (child instanceof Html) return child.value
  if (Array.isArray(child)) return child.map(renderChild).join('')
  return escapeHtml(child)
}

function renderAttrs(attrs: Attrs): string {
  return Object.entries(attrs)
    .filter(([, value]) => value !== null && value !== undefined && value !== false)
    .map(([name, value]) => (value === true ? ` ${name}` : ` ${name}="${escapeHtml(value)}"`))
    .join('')
}

export function element(tag: string, attrs: Attrs = {}, ...children: Child[]): Html {
  const open = `<${tag}${renderAttrs(attrs)}>`
  if (VOID_TAGS.has(tag)) return raw(open)
  return raw(`${open}${children.map(renderChild).join('')}</${tag}>`)
}

type ElementFactory = (attrsOrChild?: Attrs | Child, ...children: Child[]) => Html

function isAttrs(value: Attrs | Child): value is Attrs {
  return (
    typeof value === 'object' &&
    value !== null &&
    !(value instanceof Html) &&
    !Array.isArray(value)
  )
}

const TAGS = [
  'a', 'b', 'button', 'code', 'div', 'form', 'h1', 'h2', 'h3', 'iframe', 'img', 'input',
  'label', 'li', 'main', 'nav', 'option', 'p', 'pre', 'section', 'select', 'span',
  'table', 'tbody', 'td', 'textarea', 'th', 'thead', 'tr', 'ul',
] as const

export type TagName = (typeof TAGS)[number]

export const el = Object.fromEntries(
  TAGS.map((tag) => [
    tag,
    ((attrsOrChild?: Attrs | Child, ...children: Child[]) =>
      attrsOrChild !== undefined && isAttrs(attrsOrChild)
        ? element(tag, attrsOrChild, ...children)
        : element(tag, {}, attrsOrChild as Child, ...children)) as ElementFactory,
  ])
) as Record<TagName, ElementFactory>
