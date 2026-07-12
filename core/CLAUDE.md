# Core — Framework Engine

## Responsibility

The core is the framework itself. It boots the server, loads modules, provides services, and handles all production concerns automatically. Target: under 2000 LOC.

## Key Components

### index.ts — Entry Point
- Creates Hono app with global middleware
- Calls `loadModules()` to scan and initialize plugins
- Mounts module routes (`/api/{slug}/*`, `/{slug}/*`)
- Provides admin API endpoints (`/api/admin/modules`, `/api/admin/system`)
- Handles graceful shutdown

### loader.ts — Plugin Lifecycle
- Scans `plugins/` for `manifest.json` files
- For each module: creates DB → runs migrations → builds context → calls `setup()`
- Mounts API and page sub-routers on the main app
- Lifecycle: DISCOVERED → VALIDATED → DB_READY → ACTIVE

### services/ — Injected into Modules
| Service | File | Purpose |
|---------|------|---------|
| db | db.ts | Per-module SQLite + migration runner |
| auth | auth.ts | Session management, role-based middleware |
| events | events.ts | EventBus for module-to-module communication |
| notify | notify.ts | Notification dispatch |
| storage | storage.ts | Scoped file storage per module |
| config | config.ts | Per-module key-value settings |
| external-api | external-api.ts | HTTP client factory with auto-auth |
| ai | ai.ts | AI provider abstraction (Anthropic/OpenAI/Ollama) |
| scheduler | scheduler.ts | Cron job registration |
| log | log.ts | Scoped structured logger |

### middleware/ — Request Pipeline
Applied to ALL requests automatically:
1. CORS
2. Request logger (method, path, status, duration)
3. Error boundary (AppError → JSON, unhandled → 500)

### helpers/ — Shared Utilities
- `errors.ts` — AppError class + Errors factory (notFound, validation, etc.)
- `validated.ts` — Zod schema middleware for request body/query
- `pagination.ts` — Pagination params middleware + meta builder

## Editing Guidelines

- Never break the ModuleContext interface without updating types.ts
- Services must be stateless or scoped — no shared mutable state between modules
- Error boundary must catch ALL thrown errors — modules should never crash the server
- Keep config in `config/` — no magic numbers in core source
