# thandra-api

Go backend for the Thandra customer app and admin app. PostgreSQL on Neon, deployed on Render.

## Run locally

```sh
cp .env.example .env       # fill in DATABASE_URL, DATABASE_URL_POOLED, JWT_ACCESS_SECRET
make migrate-up            # uses the direct (non-pooled) database URL
make seed                  # first admin from SEED_ADMIN_* in .env
make run                   # http://localhost:8080
```

Check it: `curl localhost:8080/readyz`

The API contract is [api/openapi.yaml](api/openapi.yaml).

## Layout

```
cmd/api        HTTP server entry point (wiring only)
cmd/migrate    runs SQL migrations (up | down | status)
cmd/seed       creates the first admin
db/migrations  versioned SQL migrations (goose), embedded in the binary
internal/
  config       environment loading and validation
  server       router and middleware stack
  auth         login, JWT access tokens, rotating refresh tokens
  hubs cars    one package per feature: handler -> service -> repository
  platform/    shared plumbing: database, httpx, crypto, logger, observability, dberr
api/           OpenAPI contract
```

Each feature keeps three layers: the handler parses and validates HTTP, the service holds business rules and authorization, the repository holds SQL. Repositories return `dberr` sentinels so services never see driver types.

## Security notes

- Passwords: argon2id. Login errors are identical for unknown email, wrong password, locked and disabled accounts, with a dummy hash to even out timing. 5 failures lock the account for 15 minutes.
- Access tokens: HS256 JWT, 15 minutes, algorithm pinned, issuer checked. Refresh tokens: random, stored hashed, single use, rotated; reusing one revokes the whole session family.
- Every SQL statement is parameterized. Request bodies are capped at 1 MiB, reject unknown fields and are validated.
- There is a single admin level: every admin has full access. Roles and hub scoping were removed on purpose.
- Rate limits per client IP, stricter on auth routes. `TRUSTED_PROXY_HOPS` must match the number of proxies in front of the app, or the limiter can be spoofed or collapse to one IP.
- Sentry events are scrubbed of headers, cookies, bodies and query strings. Logs never include query strings.
- Secrets live only in environment variables. `.env` is git-ignored.

## Deploy on Render

Native build (no Docker):

- Build command: `go build -o bin/api ./cmd/api && go build -o bin/migrate ./cmd/migrate`
- Pre-deploy command: `./bin/migrate up`
- Start command: `./bin/api`
- Health check path: `/healthz`
- Use a paid instance (the free tier sleeps), and set every variable from `.env.example` in the dashboard. Set `APP_ENV=production`.
- On Render, `DATABASE_URL` (direct) is used for the migration step and `DATABASE_URL_POOLED` by the API.

The `Dockerfile` builds a minimal non-root image if you prefer Docker.

## Not built yet

Customer users, bookings (with the overlap-proof exclusion constraint), contacts sync, FCM notifications, photo uploads to object storage, OTP login, background jobs (token and contact cleanup). Handler and service tests against a database are also still to do; current tests cover config, crypto, tokens, plate rules and IP handling.
