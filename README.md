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
  hubs cars bookings contacts customer notifications catalog
               one package per feature: handler -> service -> repository
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

## What the API covers

| Area | Endpoints |
|---|---|
| Admin auth | `POST /v1/auth/login, refresh, logout`, `GET /v1/auth/me`, `GET/POST /v1/admins` |
| Hubs and cars (admin) | `/v1/hubs`, `/v1/cars`, car photo upload and delete, `GET /v1/users` |
| Public catalog | `GET /v1/public/hubs`, `/v1/public/cars` (filter by hub and free time window), `/v1/public/cars/{id}` |
| Customer login | `POST /v1/customer/auth/otp/request, otp/verify, refresh, logout`, `GET/PATCH /v1/customer/me` |
| Bookings | customer: create, list, get, cancel. admin: list, get, start, complete, cancel |
| Contacts | customer: `sync`, `status`, `deletion`. admin: `GET /v1/bookings/{id}/contacts` (audited) |
| Push | customer: register and unregister device. admin: `POST /v1/notifications` |

Rules worth knowing:

- A car can never have two overlapping open bookings. This is enforced by a Postgres exclusion constraint, so it holds even when requests arrive at the same instant.
- Booking is refused with `428 contacts_required` until the customer has synced contacts (`REQUIRE_CONTACTS_FOR_BOOKING`). Phone numbers are normalized to E.164 and deduplicated per customer, so syncing again for another booking never creates duplicates.
- Contacts are encrypted with AES-256-GCM, hashed with HMAC for deduplication, deletable by the customer once no booking is open, and deleted automatically `CONTACTS_RETENTION_DAYS` after the last ride by an hourly job.
- Admins can view a booking's contacts only while it is open or within the retention period afterwards, and each view is written to `audit_log`.

## Before production

- **OTP delivery.** Only a console sender exists, and the API refuses to start in production with it, because it writes login codes to the logs. Add an SMS provider (for example MSG91, which needs DLT registration in India) behind the `otp.Sender` interface.
- **Push notifications.** Set `FIREBASE_CREDENTIALS_B64`. Until then notifications are logged, not sent. Nothing has been tested against real FCM delivery.
- **Storage.** Photo upload is tested against the Neon bucket. Photo links are temporary (one hour) and are generated on every response.
- **Rotate credentials** that were pasted into chat, and set `APP_ENV=production`, `TRUSTED_PROXY_HOPS` and `SENTRY_DSN` on Render.

## Not built yet

Payments, driving licence upload, admin editing of bookings, push topics and broadcast, and database-backed tests for handlers (current tests cover business rules, crypto, tokens, phone numbers, contacts, bookings and push with in-memory fakes).
