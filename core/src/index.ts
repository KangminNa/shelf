import { Hono } from 'hono'
import { serve } from '@hono/node-server'
import { cors } from 'hono/cors'
import { loadModules } from './loader.js'
import { createEventBus } from './services/events.js'
import { createAuthService } from './services/auth.js'
import { errorBoundary } from './middleware/error-boundary.js'
import { requestLogger } from './middleware/request-logger.js'
import { ensureDataDir } from './services/db.js'
import { SERVER } from '../../config/server.js'
import type { LoadedModule } from './types.js'

const app = new Hono()
const PORT = SERVER.PORT

// Ensure data directory exists
ensureDataDir()

// Global middleware
app.use('*', cors())
app.use('*', requestLogger)
app.onError(errorBoundary)

// Core services
const events = createEventBus()
const auth = createAuthService()

// Health check
app.get('/health', (c) => c.json({ ok: true, uptime: process.uptime() }))

// Load modules
let modules: LoadedModule[] = []

async function boot() {
  modules = await loadModules(events, auth)

  // Mount module routes
  for (const mod of modules) {
    const slug = mod.manifest.name
    app.route(`/api/${slug}`, mod.api)
    app.route(`/${slug}`, mod.pages)
  }

  // Admin API: list modules
  app.get('/api/admin/modules', (c) => {
    return c.json(
      modules.map((m) => ({
        name: m.manifest.name,
        displayName: m.manifest.displayName,
        version: m.manifest.version,
        icon: m.manifest.icon,
        menu: m.manifest.menu,
      }))
    )
  })

  // Admin API: system info
  app.get('/api/admin/system', (c) => {
    return c.json({
      uptime: process.uptime(),
      memory: process.memoryUsage(),
      modules: modules.length,
      nodeVersion: process.version,
    })
  })

  // Start server
  serve({ fetch: app.fetch, port: PORT }, (info) => {
    console.log('')
    console.log(`  ┌─────────────────────────────────────┐`)
    console.log(`  │                                     │`)
    console.log(`  │   Shelf v0.1.0                      │`)
    console.log(`  │   http://localhost:${info.port}            │`)
    console.log(`  │                                     │`)
    console.log(`  │   Modules: ${modules.length.toString().padEnd(25)}│`)
    console.log(`  │                                     │`)
    console.log(`  └─────────────────────────────────────┘`)
    console.log('')

    for (const mod of modules) {
      console.log(`  • ${mod.manifest.displayName || mod.manifest.name} → /${mod.manifest.name}`)
    }
    if (modules.length) console.log('')
  })

  // Graceful shutdown
  const shutdown = async () => {
    console.log('\n[shelf] shutting down...')
    for (const mod of modules) {
      if (mod.shutdown) await mod.shutdown()
    }
    process.exit(0)
  }

  process.on('SIGINT', shutdown)
  process.on('SIGTERM', shutdown)
}

boot().catch((err) => {
  console.error('[shelf] failed to boot:', err)
  process.exit(1)
})
