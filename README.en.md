# Naru

> Manage your proxy from a screen like Nginx Proxy Manager — and deploy on push.

[한국어](README.md) · [Landing page](https://kangminna.github.io/shelf-site/) · MIT

Editing an nginx config to point one more domain somewhere, wiring a certbot cron, keeping a spreadsheet of which
port is taken — that's the chore this exists to remove. Naru is a **reverse proxy that owns 80/443** and lets you
manage domains, certificates and upstreams from a screen.

App deployment sits on top of that. The proxy has to know about your containers anyway, so it may as well start them too.

## Why it exists

I used Nginx Proxy Manager a lot. Two things were missing.

- **I wanted it for Caddy.** Caddy gives you automatic HTTPS and HTTP/3 by default, but you drive it from the CLI and config files. I wanted to drive it from a screen, the way NPM does.
- **I needed CI/CD too.** NPM only points domains. Naru also builds, deploys and wires up the domain when you push.

## Name: Shelf → Naru

This project used to be called Shelf. Why it changed:

- **The name didn't say what it does.** A shelf holds things; this is a web server that takes requests, routes them to services and deploys them.
- **The name was taken in the same field.** Dart's web server library [`shelf`](https://pub.dev/packages/shelf) crowds the search results.
- **It is being rebuilt.** The name changes along with v2, which moves the web server engine to Caddy.

*Naru* (나루) is Korean for a ferry landing — where requests arrive and cross to each service, and new versions dock.
New containers are named `naru-*`. Containers carried over from v1 (`shelf-*`) and their webhook URLs are kept as they are.

> **This branch (`v2`) is the Go rewrite, in progress.** The v1 (Node) running in production lives on [`main`](https://github.com/KangminNa/shelf/tree/main).
> Progress: [v2 plan](docs/design/v2.md) §9 (Korean).

---

## What works now

**Web server — Caddy is the engine, you only see the screen**

- Write a domain and it routes to the service, by container name — no host ports to open.
- **Automatic HTTPS** — Caddy gets and renews the certificate as soon as a domain appears. HTTP is redirected to HTTPS (307) **only once the certificate exists**,
  so a missing or expired certificate never locks you out of your own admin screen. Each address shows "HTTPS · until …", or where its DNS points while it waits.
- HSTS per address. Unknown hosts get 404. Naru draws the whole Caddy config and pushes it over Caddy's admin unix socket — there is no config file to edit by hand.

**Web server settings per service** — response headers, allowed IPs, password protection (basic auth), maintenance mode, path routing (`/api` to another service), and Caddyfile directives in an advanced box.
Paste an nginx config and it is carried over into the fields. Applying checks with the web server right away and rolls back if it refuses. Compression is always on.

**Services — four kinds**

| Kind | What |
|---|---|
| Repository | Built from the repo's `Dockerfile`, run as a container. Monorepos via a build path. The port comes from `EXPOSE` |
| Image | Pulled from Docker Hub / GHCR, run as a container |
| Static site | A folder of the repo, served by the web server directly — no container. Hidden files (`.env`, `.github` …) are never published |
| External | Something already running (NAS, router, a program on the host) — just an address. `localhost` means this server |

**Deploys**

- **Push to deploy** — a webhook URL and secret per service. GitHub HMAC · GitLab token · `?secret=` for registries. Only the registered branch.
- **No dropped requests** — the new container joins under the same name and the old one is retired only after the new one answers. If the new one dies, the old one keeps serving.
  (Measured locally: 3,657 of 3,657 requests answered 200 during a redeploy.)
- Pushes during a deploy collapse into one more deploy. Steps and the full log update live on screen.
- **Roll back** — with the image or files of that deploy, no rebuild. Keeps 3 built images and 5 static releases.

**Setup and admin**

- No `.env`: a first-run wizard — account → admin address (with a DNS check) → HTTPS contact. The wizard link is printed only in the server log.
- Korean and English screens. Server CPU / memory / disk. v1 data (accounts, apps, proxy hosts, deploy history) is imported on first start, keeping IDs so webhook URLs survive.

**Not yet** — monitoring, alerts, diagnostics and the logs tab (M6), production switch-over (M7).

---

## Ways to install

The same binary installs two ways, with the same features:

- **Docker** (`docker compose up`) — the simplest. The web server and app containers share a Docker network.
- **Host** (systemd) — the web server reaches programs on the server at `localhost`, and app containers by IP on any network. Linux servers. [Host install](docs/install-host.md) (Korean)

## Try it locally

Only Docker is needed (no Go install).

```bash
git clone -b v2 https://github.com/KangminNa/shelf && cd shelf
docker compose up -d --build
docker compose logs naru | grep setup      # first-run link — only someone who can read the log can create the admin
```

The local setup opens the web server on `127.0.0.1:8088` (HTTP) and `8443` (HTTPS) with certificates from Caddy's internal CA.
Browsers send `*.localhost` to your own machine — e.g. set the admin address to `naru.localhost` and open `http://naru.localhost:8088`.

Optional environment variables (otherwise set on screen): `ADMIN_DOMAIN`, `ACME_EMAIL`. When set, they win over the screen.

Forgot the password? From the server shell:

```bash
docker compose exec naru naru users
docker compose exec naru naru passwd admin 'new-password'   # signs out every session of that account
docker compose exec naru naru reset                        # removes all accounts → setup reopens on restart
```

---

## Your first service

1. **Services → New service** — pick a kind, enter a repository or image and a domain. Name and port can stay empty.
2. The first deploy starts right away and its steps are drawn on screen.
3. Paste the service's **webhook** URL and secret into GitHub → Settings → Webhooks; every push deploys from then on.

The [landing page](https://github.com/KangminNa/shelf-site) is deployed this way — as a static site, served without a container.

---

## Make it yours

MIT. Fork it, change it, take only the parts you need. What keeps it easy to change:

- **One object per job, related only through interfaces** — packages import only `model` (data) and `contract` (interfaces). Concrete types meet in one place, `app`.
- **The outside world sits behind tools** — Docker, git, Caddy, SQLite and files each live in a tool package, so tests swap in fakes and still exercise the real wiring through the screen.
- **The docs are enforced** — `internal/archtest` checks rules R1–R11 of the [object design](docs/design/v2-objects.md). A type missing from the design fails the tests.
- **Two dependencies** — `modernc.org/sqlite` (pure-Go SQLite) and `golang.org/x/crypto` (scrypt). HTML is rendered on the server; no JavaScript framework.

```
cmd/naru/            server · shell recovery commands
internal/
├── model/ contract/ data · every interface
├── access settings services kinds deploy webhook webserver views   one package per job
├── web/ cli/        entry points
├── store/           SQLite (the only SQL)
├── docker git files caddy netcheck stats events system           tools that touch the outside
├── app/             composition root
└── archtest/        rule checks
```

```bash
scripts/go.sh test ./...        # Go runs in Docker
scripts/caddy-validate.sh       # the drawn Caddy configs must pass real Caddy
```

---

## Running it

- Naru mounts `/var/run/docker.sock`, which means full control of the host's Docker — so **an admin account is root-equivalent by design**.
  Use it on your own server and only give accounts to people you'd trust with the server.
- Deploying a service runs someone's code on your server. Only deploy repositories you trust.
- Secrets (git tokens, webhook secrets, environment variables) are stored in the data folder. Protect it — permissions, disk encryption, backups.
  They never go to the screen, the logs or the web server config.
- **Expose only 80/443.** The admin screen sits behind the web server too, and Naru's own port binds to 127.0.0.1.
  Caddy's admin API is opened only on a unix socket shared by Caddy and Naru.
- Built-in defenses: scrypt passwords, 15-minute lockout after 5 failed sign-ins, Secure/httpOnly/SameSite cookies, cross-site form refusal, CSP,
  verified webhooks with a body limit, validation of git URLs, branches, paths and image names.

---

## License

MIT © Kangmin Na
