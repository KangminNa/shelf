import { existsSync, mkdirSync, readFileSync, writeFileSync, unlinkSync, readdirSync } from 'fs'
import { join, resolve, relative } from 'path'
import type { StorageService } from '../types.js'

const STORAGE_ROOT = join(process.cwd(), 'data', 'storage')

export function createStorage(slug: string): StorageService {
  const baseDir = join(STORAGE_ROOT, slug)
  if (!existsSync(baseDir)) mkdirSync(baseDir, { recursive: true })

  function safePath(filename: string): string {
    const resolved = resolve(baseDir, filename)
    if (!resolved.startsWith(baseDir)) {
      throw new Error('Path traversal detected')
    }
    return resolved
  }

  return {
    async save(filename, data) {
      const filepath = safePath(filename)
      const dir = filepath.substring(0, filepath.lastIndexOf('/'))
      if (!existsSync(dir)) mkdirSync(dir, { recursive: true })
      writeFileSync(filepath, data)
      return filename
    },

    async read(filename) {
      const filepath = safePath(filename)
      if (!existsSync(filepath)) return null
      return readFileSync(filepath)
    },

    async delete(filename) {
      const filepath = safePath(filename)
      if (existsSync(filepath)) unlinkSync(filepath)
    },

    async list(prefix = '') {
      const dir = safePath(prefix || '.')
      if (!existsSync(dir)) return []
      return readdirSync(dir, { recursive: true })
        .map((f) => relative(baseDir, join(dir, f as string)))
    },

    url(filename) {
      return `/storage/${slug}/${filename}`
    },
  }
}
