# Shelf — Project Guide

## Overview

Shelf is a modular home server framework. Modules define data (schema), logic, and pages — the core handles everything else (routing, auth, DB, error handling, deployment).

## Project Structure

```
shelf/
├── core/src/           # Framework core (~2000 LOC target)
│   ├── index.ts        # Server entry, boots Hono + loads modules
│   ├── loader.ts       # Plugin scanner + lifecycle manager
│   ├── types.ts        # All shared interfaces
│   ├── services/       # Injected services (auth, db, events, ai, etc.)
│   ├── middleware/     # Request pipeline (error, logging, envelope)
│   └── helpers/        # Utilities (errors, validation, pagination)
├── plugins/            # Each subfolder = one module
│   └── {name}/
│       ├── manifest.json
│       ├── migrations/
│       └── src/index.ts
├── shared/             # Shared types + UI components (future)
├── config/             # All server configuration constants
├── data/               # Runtime data (SQLite files, storage) — gitignored
└── package.json        # npm workspaces root
```

## Commands

```bash
npm run dev          # Start dev server with watch (tsx watch core/src/index.ts)
npm run build        # Build all workspaces
npm start            # Production start (node core/dist/index.js)
npm run create       # Scaffold a new module
```

## Tech Stack

- Runtime: Node.js + TypeScript (ES2022, ESM)
- HTTP: Hono
- DB: SQLite via better-sqlite3 + Drizzle ORM
- Validation: Zod
- Frontend: React SSR (MPA, future)

## Architecture Principles

- Module developers only write: schema.ts, logic.ts, pages/, manifest.json
- Core auto-handles: routing, auth, error handling, DB lifecycle, response format
- Each module gets its own SQLite file (`data/{module}.db`)
- Modules communicate via EventBus only — no direct imports
- All config lives in `config/` as typed constants

## Module Contract

A module's `setup(ctx)` receives a `ModuleContext` with: db, auth, events, notify, storage, config, api (external), ai, log, scheduler. Module registers routes on `ctx.api` (Hono sub-app for `/api/{slug}/*`) and `ctx.pages` (for `/{slug}/*`).

## Key Files

- `core/src/types.ts` — All interfaces. Read this first.
- `core/src/loader.ts` — Module loading logic.
- `core/src/index.ts` — Server bootstrap.
- `config/server.ts` — Server constants (port, limits, paths).
- `config/defaults.ts` — Default values for module settings.

## Conventions

- Use `AppError` + `Errors` factory from `helpers/errors.ts` for all errors
- API responses always follow `{ ok, data, meta?, error? }` shape
- Module DB access: prefer `db.raw` (better-sqlite3 instance) for queries
- Event naming: `{module}:{action}` (e.g. `blog:post-created`)
- Config keys: `SCREAMING_SNAKE_CASE` in config files
