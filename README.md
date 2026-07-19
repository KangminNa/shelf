# Shelf

> Your server, your apps, your rules.

Shelf is a self-hosted app platform. Deploy any Git repository or Docker image as a container — Shelf handles builds, CI/CD, domain routing, and SSL.

## What is Shelf?

Point Shelf at a Git repo with a `Dockerfile` (or any Docker image), and it becomes a running app on your server:

```
You provide:              Shelf handles:
──────────────            ─────────────────────────────────────
a Git repo with           git pull → docker build → run
a Dockerfile              webhook CI/CD (push-to-deploy)
(or a Docker image)       domain routing on 80/443
                          SSL issue & auto-renewal (Let's Encrypt)
                          container lifecycle, logs, restart policy
```

**The app contract is one line**: a `Dockerfile` in the repo root, serving HTTP on one port. Language, framework, and database are entirely up to the app.

## Features

- **Apps as containers** — deploy from Git (built with `docker build`) or run any public Docker image
- **Push-to-deploy** — GitHub/GitLab webhook (HMAC-verified) triggers rebuild & redeploy
- **Built-in reverse proxy** — domain-based routing on 80/443, WebSocket support, access logs
- **SSL management** — one-click Let's Encrypt issuance, auto-renewal, manual PEM upload
- **Admin dashboard** — apps, deploy history with full build logs, container logs, proxy hosts

## Quick start

```bash
npm install
npm run dev
# open http://localhost:9666/admin
```

Production (Shelf itself runs in Docker, needs docker.sock):

```bash
docker compose up -d
# admin on :81, proxy on :80/:443, webhooks on :9100
```

## Deploying your first app

1. **Admin → Apps → New app** — enter a Git URL (repo must contain a `Dockerfile`) or a Docker image name, set port mapping (host → container), optionally a domain.
2. **Deploy** — Shelf clones, builds, and starts the container (`shelf-{name}`, `--restart unless-stopped`).
3. **CI/CD** — copy the webhook URL + secret from the app detail page into GitHub → Settings → Webhooks. Every push redeploys automatically.
4. **Domain & SSL** — a domain registers itself with the proxy; issue SSL from the Proxy page.

A minimal example lives in [`examples/hello-app/`](examples/hello-app/).

## Architecture

```
core/src/
├── kernel/application.ts   ShelfApplication — global instance, boot & routing
├── system/
│   ├── deploy/             apps: repositories, pipeline, containers, webhook, controller
│   ├── proxy/              proxy: server (SNI/ACME), ssl-manager, controller
│   └── docker.ts           DockerService — docker CLI wrapper
├── db/                     AppDatabase → Repository<T> → QueryBuilder<T> (no raw SQL)
├── services/               EventBus · Logger · Scheduler
├── middleware/             error boundary, request logger, shell wrap
├── admin/ + ui/            dashboard pages & design system
└── config.ts               env-based settings
```

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for the full development guide (Korean).

## License

MIT
