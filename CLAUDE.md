# Naru — Project Guide

Naru is a self-hosted web server manager for one server: Caddy as the engine (managed from a screen, never by hand),
plus push-to-deploy. Written in Go; the admin screen is server-rendered HTML (`html/template`, no JS framework).

> Branch `v2` is the Go rewrite. Production still runs v1 (Node) from `main` until M7.

## Read first (Korean)

| Doc | Question it answers |
|---|---|
| [docs/design/v2-objects.md](docs/design/v2-objects.md) | **Who does what** — behaviors A–H, one object per job, every interface (§6 is `contract.go` verbatim), who uses what (§7), rules R1–R11 (§10), how to work (§11) |
| [docs/design/v2.md](docs/design/v2.md) | **What and when** — features, screens, milestones M1–M7, progress log |
| [docs/design/caddy-engine.md](docs/design/caddy-engine.md) | **Invariants** for driving Caddy (§4) and certificates (§5). The rest is v1-era |

## How we work

Any change that needs a decision starts with a **design note** (which behavior, new/changed interfaces as Go signatures,
changed §7 rows, invariants, pattern and why, alternatives not taken) → user approval → tests against the interface
with fakes → implementation → `archtest` + update `v2-objects.md`. Bug fixes that don't change an interface skip the note.

`internal/archtest` enforces R1–R11 (imports only `model`/`contract`, relations via interfaces, `model` is data only,
≤5 methods per interface, every `model`/`contract` type named in the design doc, no vague names like
Handler/Data/Info/Util, SQL only in `store`, outside world only in tool packages, kind branching only in `kinds`,
no secrets in `web`/`cli`, regexp only in `model`). A failure there is a design violation, not a test to loosen.

## Structure

```
cmd/naru/            main — server, or a shell recovery command (users | passwd | reset | version)
internal/
├── model/           data and value objects (Parse… validates once) — no behavior beyond keeping values valid
├── contract/        every interface between objects
├── access settings services kinds deploy webhook webserver views   ← one package per behavior (§4 A–H)
├── web/ cli/        entry points: turn requests into interface calls, results into screens
├── store/           SQLite (the only SQL) incl. v1 import
├── docker git files caddy netcheck stats events system           ← tools that touch the outside world
├── app/             composition root — the only place concrete types meet (§7 table as code)
└── archtest/        R1–R11
caddy/bootstrap.json Caddy's first config (admin socket only); Naru pushes the full config over the socket
deploy/docker/       production compose (Docker install) + rehearsal.yml (shadow run on loopback ports)
deploy/host/         systemd units for the host install
docs/switch-v1.md    switching the live server from v1 to v2 — follow it command by command
```

## Commands

No local Go needed — `scripts/go.sh` runs Go in Docker.

```bash
scripts/go.sh vet ./...
scripts/go.sh test ./...
scripts/go.sh test ./internal/caddy -update   # rewrite Caddy config goldens after an intended change
scripts/caddy-validate.sh                     # goldens must pass real `caddy validate`
docker compose up -d --build                  # local stack: Caddy on 127.0.0.1:8088/8443, Naru on 127.0.0.1:8080
docker compose logs naru | grep setup         # first-run setup link (token printed only in the log)
```

Local stack data lives in `data-v2/` (gitignored). Use a copy of real data, never the production server.

## Security rules (do not regress)

- Admin = root equivalent (Naru mounts `docker.sock`). Accounts are created only via the setup token printed in the log;
  recovery only from the server shell (`naru passwd|reset`).
- Caddy's admin API is a unix socket shared only by Caddy and Naru. Every pushed config must contain that admin block —
  `caddy.AdminSocketGuard` refuses to send otherwise. Never expose admin on TCP.
- Redirect HTTP→HTTPS only when a usable certificate exists (`siteMapBuilder`), never via Caddy's automatic redirects;
  never redirect `/.well-known/acme-challenge/*`.
- Secrets (git tokens, webhook secrets, env) live in `data/` in plain text: never in Caddy config, logs, screens or API
  responses. The screen shows only whether a token exists.
- Expose only 80/443; Naru's own port binds to loopback. Webhooks are verified (GitHub HMAC, GitLab token, `?secret=`), body ≤1MB.
- Static sites never publish hidden files (except `.well-known`). Validate git URLs/branches/paths/images via `model` values.
- The Caddy container name must not start with `shelf-` (v1 treats those as apps).

## Conventions

- Comments and screen text are Korean first; screens are bilingual (`internal/web/messages.go`, both languages must have every key).
- No inline `style=` in templates (CSP blocks it) — use classes in `static/naru.css`.
- Names say what the thing does (interfaces) or how (implementations): `ImagePuller` / `docker.Puller`.
