# Shelf — Project Guide

## Overview

Shelf is a self-hosted app platform, shipped as a **single Docker image**. Apps are Docker containers deployed from Git repos (with a Dockerfile) or plain Docker images. The core orchestrates: build, CI/CD webhooks, reverse proxy (80/443), SSL, and an admin UI.

**Before writing code, read these in order** (all Korean):

| Doc | Question it answers |
|---|---|
| [docs/SPEC.md](docs/SPEC.md) | **What** we provide — every feature has an F-number and a status. Nothing outside this list gets built. |
| [docs/OBJECTS.md](docs/OBJECTS.md) | **Who** does it — one responsibility sentence per class. Pick the owner before adding code; if none fits, write the sentence first. |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | **What shape** — layers, dependency direction, patterns, review checklist. |
| [docs/PROCESS.md](docs/PROCESS.md) | **How we work** — the feature loop and what `npm test` enforces automatically. |
| [docs/HISTORY.md](docs/HISTORY.md) | **Why it looks like this** — three direction changes and the lessons from production. |

[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) is the class-level walkthrough.

`core/tests/architecture.test.ts` fails when code drifts from these docs (cross-system imports, raw SQL outside `db/`, undocumented classes, undeclared events). Treat a failure there as a spec violation, not a test to loosen.

## Structure

```
core/
├── migrations/{scope}/     # NNN_name.sql, applied once in order (never edit applied files)
└── src/
    ├── index.ts            # Entry: ShelfApplication.instance.start()
    ├── config.ts           # Env-based server settings
    ├── kernel/application.ts  # Singleton — owns systems, routes, lifecycle
    ├── system/             # Domain features (auth, deploy, proxy, docker.ts)
    │   └── {name}/         #   index.ts(Facade) · repositories · controller · views
    ├── db/                 # AppDatabase → Repository<T> → QueryBuilder<T>
    ├── services/           # EventBus · Logger · Scheduler (classes)
    ├── middleware/          # error-boundary, request-logger, shell-wrap
    ├── admin/              # Core admin pages (dashboard/system/guide/settings)
    └── ui/                 # Design system: shell, components, icons, styles
```

## Commands

```bash
npm run dev                        # tsx watch, http://localhost:9666/admin
npx tsc --noEmit --project core    # typecheck (run before committing)
docker compose up -d               # production (docker.sock mount required)

# dev with non-default ports:
PORT=9667 PROXY_HTTP_PORT=8087 PROXY_HTTPS_PORT=8447 WEBHOOK_PORT=9100 npx tsx core/src/index.ts
```

## Hard Rules

- **Systems never import each other** — communicate via `EventBus` (`{system}:{action}` naming)
- **No raw SQL outside `db/`** — add methods to the system's `repositories.ts`
- **HTML only in `views.ts` pure functions** — data in, string out; no DB/network access
- **Hono only in controllers and kernel**
- **Secrets never in API responses or logs** — use `sanitize()` / masking (see deploy controller)
- API responses: `{ ok: true, data }` / `{ ok: false, error: { code, message } }`
- Runtime deps stay minimal: hono, @hono/node-server, better-sqlite3 (+ acme-client at root)

## Key Facts

- Auth: session cookie (`shelf_session`), first run → `/setup`; all `/admin` + `/api/{deploy,proxy}` protected; webhook port 9100 uses HMAC instead
- Lost password: `npm run admin passwd <user> <new>` (or `reset` to reopen `/setup`) — server shell only, no HTTP recovery path
- Webhooks: `POST https://{ADMIN_DOMAIN}/hooks/{projectId}` (via proxy on 80/443) — `WebhookHandler` decides, two transports (`:9100` server + main app `/hooks`) only adapt
- App containers: named `shelf-{app}`, built images `shelf-app-{app}`, `--restart unless-stopped`
- When Shelf itself runs in Docker: proxy reaches apps via `APP_HOST=host.docker.internal`
- Runtime data lives in `data/` (gitignored) — one dir to back up
- Tech: Node 20 + TypeScript ESM, Hono, better-sqlite3 (WAL). No React, no ORM.
