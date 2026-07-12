import { Hono } from 'hono'
import { existsSync, readdirSync, readFileSync } from 'fs'
import { join } from 'path'
import type { ModuleManifest, ModuleContext, LoadedModule, EventBus } from './types.js'
import { createModuleDB } from './services/db.js'
import { createStorage } from './services/storage.js'
import { createConfigService } from './services/config.js'
import { createNotifyService } from './services/notify.js'
import { createExternalApiClients } from './services/external-api.js'
import { createAiService } from './services/ai.js'
import { createScheduler } from './services/scheduler.js'
import { createLogger } from './services/log.js'
import type { AuthService } from './types.js'

const PLUGINS_DIR = join(process.cwd(), 'plugins')

export async function loadModules(events: EventBus, auth: AuthService): Promise<LoadedModule[]> {
  if (!existsSync(PLUGINS_DIR)) return []

  const dirs = readdirSync(PLUGINS_DIR, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => d.name)

  const modules: LoadedModule[] = []

  for (const dir of dirs) {
    const manifestPath = join(PLUGINS_DIR, dir, 'manifest.json')
    if (!existsSync(manifestPath)) continue

    try {
      const manifest: ModuleManifest = JSON.parse(readFileSync(manifestPath, 'utf-8'))
      const module = await loadModule(dir, manifest, events, auth)
      modules.push(module)
      console.log(`[loader] ✓ ${manifest.displayName || manifest.name} (${manifest.version})`)
    } catch (err) {
      console.error(`[loader] ✗ failed to load ${dir}:`, err)
    }
  }

  events.emit('core:modules-loaded', modules.map((m) => m.manifest.name))
  return modules
}

async function loadModule(
  dir: string,
  manifest: ModuleManifest,
  events: EventBus,
  auth: AuthService
): Promise<LoadedModule> {
  const slug = manifest.name
  const pluginDir = join(PLUGINS_DIR, dir)
  const migrationsDir = join(pluginDir, 'migrations')

  const db = createModuleDB(slug, existsSync(migrationsDir) ? migrationsDir : undefined)
  const storage = createStorage(slug)
  const config = createConfigService(slug)
  const notify = createNotifyService(slug, events)
  const log = createLogger(slug)
  const scheduler = createScheduler(slug)
  const apiClients = await createExternalApiClients(manifest.externalApis || [], config)
  const ai = await createAiService(config)

  const apiRouter = new Hono()
  const pagesRouter = new Hono()

  const shutdownHandlers: Array<() => Promise<void> | void> = []

  const context: ModuleContext = {
    slug,
    dataDir: join(process.cwd(), 'data'),
    db,
    auth,
    notify,
    events,
    storage,
    config,
    api: apiClients,
    ai,
    log,
    scheduler,
    onShutdown: (fn) => shutdownHandlers.push(fn),
  }

  // Load module entry point
  const entryPath = join(pluginDir, 'src', 'index.ts')
  const distPath = join(pluginDir, 'dist', 'index.js')
  const modulePath = existsSync(distPath) ? distPath : entryPath

  if (existsSync(modulePath)) {
    const mod = await import(modulePath)
    if (typeof mod.setup === 'function') {
      await mod.setup({
        ...context,
        api: apiRouter,
        pages: pagesRouter,
      })
    } else if (typeof mod.default === 'function') {
      // logic.ts style: export default ({ db, api, ... }) => ({ ... })
      const logic = mod.default(context)

      // Auto-register cron jobs
      if (logic.jobs) {
        for (const job of logic.jobs) {
          scheduler.register(job.cron, job.handler, logic[job.handler].bind(logic))
        }
      }

      // Auto-register event handlers
      if (logic.events) {
        for (const [event, handler] of Object.entries(logic.events)) {
          events.on(event, handler as any)
        }
      }
    }
  }

  return {
    manifest,
    context,
    api: apiRouter,
    pages: pagesRouter,
    shutdown: async () => {
      for (const fn of shutdownHandlers) await fn()
    },
  }
}
