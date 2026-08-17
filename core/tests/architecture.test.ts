import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

const SRC = join(import.meta.dirname, '..', 'src')
const DOCS = join(import.meta.dirname, '..', '..', 'docs')

function sourceFiles(dir = SRC): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = join(dir, entry)
    if (statSync(full).isDirectory()) return sourceFiles(full)
    return full.endsWith('.ts') ? [full] : []
  })
}

function read(path: string): string {
  return readFileSync(path, 'utf-8')
}

function relative(path: string): string {
  return path.slice(SRC.length + 1)
}

test('systems never import each other — they talk through EventBus', () => {
  const systems = ['auth', 'deploy', 'proxy']
  const violations: string[] = []

  for (const file of sourceFiles(join(SRC, 'system'))) {
    const owner = systems.find((s) => relative(file).startsWith(`system/${s}/`))
    if (!owner) continue
    for (const other of systems.filter((s) => s !== owner)) {
      if (new RegExp(`from '[^']*system/${other}/`).test(read(file))) {
        violations.push(`${relative(file)} imports system/${other}`)
      }
    }
  }

  assert.deepEqual(violations, [], violations.join('\n'))
})

test('raw SQL lives only in the db layer', () => {
  const sqlPattern = /\b(SELECT|INSERT INTO|UPDATE\s+\w+\s+SET|DELETE FROM)\b|\.prepare\(/
  const offenders = sourceFiles()
    .filter((file) => !relative(file).startsWith('db/'))
    .filter((file) => sqlPattern.test(read(file)))
    .map(relative)

  assert.deepEqual(offenders, [], `SQL outside db/: ${offenders.join(', ')}`)
})

test('Hono request/response objects appear only in controllers, kernel, middleware, and auth routes', () => {
  const allowed = ['kernel/', 'middleware/', 'admin/routes.ts', 'system/auth/index.ts', 'system/deploy/index.ts']
  const offenders = sourceFiles()
    .filter((file) => !relative(file).endsWith('controller.ts'))
    .filter((file) => !allowed.some((prefix) => relative(file).startsWith(prefix)))
    .filter((file) => /\bc\.req\b|\bc\.json\(|\bc\.html\(/.test(read(file)))
    .map(relative)

  assert.deepEqual(offenders, [], `Hono context outside controllers: ${offenders.join(', ')}`)
})

test('views only render — they import no data or network dependency', () => {
  const forbiddenImports = /from '[^']*(?:db\/|repositories\.js|docker\.js|proxy-server\.js|ssl-manager\.js|pipeline\.js|container-manager\.js)'/
  const offenders = sourceFiles()
    .filter((file) => relative(file).endsWith('views.ts') || relative(file).startsWith('admin/pages/'))
    .filter((file) => {
      const source = read(file)
      const valueImports = [...source.matchAll(/^import (?!type )[^;\n]+/gm)].map((m) => m[0]).join('\n')
      return forbiddenImports.test(valueImports)
    })
    .map(relative)

  assert.deepEqual(offenders, [], `views must receive data as props, not fetch it: ${offenders.join(', ')}`)
})

test('container and image naming has a single source of truth', () => {
  const offenders = sourceFiles()
    .filter((file) => relative(file) !== 'system/deploy/container-manager.ts')
    .filter((file) => /`shelf-\$\{/.test(read(file)))
    .map(relative)

  assert.deepEqual(offenders, [], `container naming duplicated in: ${offenders.join(', ')}`)
})

test('every class in src is documented in docs/OBJECTS.md', () => {
  const dictionary = read(join(DOCS, 'OBJECTS.md'))
  const undocumented: string[] = []

  for (const file of sourceFiles()) {
    for (const match of read(file).matchAll(/^(?:export )?(?:abstract )?class (\w+)/gm)) {
      const name = match[1]
      if (name.endsWith('Error')) continue
      if (!dictionary.includes(name)) undocumented.push(`${name} (${relative(file)})`)
    }
  }

  assert.deepEqual(undocumented, [], `add a responsibility sentence for: ${undocumented.join(', ')}`)
})

test('events emitted in code are declared in the OBJECTS.md contract', () => {
  const dictionary = read(join(DOCS, 'OBJECTS.md'))
  const undeclared = new Set<string>()

  for (const file of sourceFiles()) {
    for (const match of read(file).matchAll(/events\.emit\('([^']+)'/g)) {
      if (!dictionary.includes(match[1])) undeclared.add(match[1])
    }
  }

  assert.deepEqual([...undeclared], [], `declare these events in OBJECTS.md: ${[...undeclared].join(', ')}`)
})

test('SPEC.md numbering stays unique so features can be referenced', () => {
  const ids = [...read(join(DOCS, 'SPEC.md')).matchAll(/^\| F-(\d+) \|/gm)].map((m) => m[1])
  assert.ok(ids.length > 40, `expected the feature table to be populated, found ${ids.length}`)
  assert.equal(new Set(ids).size, ids.length, 'duplicate F-numbers in SPEC.md')
})
