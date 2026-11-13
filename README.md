# B Mansion

Management system for B Mansion, a 159-room dormitory.

- **The owner (admin)** bills each room, reviews payment slips, and manages leases and maintenance requests.
- **Tenants** view their bills, attach payment slips, report maintenance issues and submit documents.

| Part | Status | Stack |
| --- | --- | --- |
| `backend/` | ✅ All phases done · API v1.0.0 | Go 1.26 + Gin, OpenAPI (contract-first, oapi-codegen), pgx + sqlc, goose, PostgreSQL 17, S3-compatible storage (RustFS for dev, Cloudflare R2 for UAT) |
| `frontend/` | Not started | Vite + React + TypeScript + MUI |

The current scope is local development and UAT only. There is no production infrastructure yet.

---

## Running the backend locally

Requirements: **Go 1.26+** and **Docker**. Run every command from `backend/`.

```sh
cd backend
cp .env.example .env        # then fill in every empty value (see below)
docker compose up -d        # Postgres :7432 · RustFS :7900 (S3) / :7901 (console); creates the bucket and CORS
go tool -modfile=tools/go.mod goose up      # migrations + seed data (159 rooms, utility rates)
go run ./cmd/seed-admin     # creates the owner account from ADMIN_USERNAME / ADMIN_PASSWORD
go run ./cmd/api            # http://localhost:8080/api/v1
```

To check that it is running: `curl http://localhost:8080/api/v1/health`

### Required `.env` values

| Variable | Value |
| --- | --- |
| `AUTH_SECRET` | A random string, e.g. `openssl rand -hex 32` |
| `ADMIN_USERNAME`, `ADMIN_PASSWORD` | The owner account. There is exactly one admin. Change its password with `go run ./cmd/seed-admin -reset`. |
| `POSTGRES_PASSWORD` | Password for the Postgres container |
| `S3_SECRET_ACCESS_KEY` | RustFS secret key, at least 8 characters |

> Never commit `.env`. Never put credentials in the compose file, `.env.example` or tests. For UAT, set `DATABASE_URL` (Neon) and `S3_*` (R2) in the UAT environment.

### Tests

```sh
go vet ./...
go test ./...                       # unit tests (no database needed)
go test -tags integration ./...     # full HTTP stack against real Postgres + RustFS (testcontainers; needs Docker)
```

### Changing the API or queries

1. Change `backend/api/openapi.yaml` first (contract-first), or edit the SQL in `backend/db/queries/<context>/`.
2. Run `go generate ./...` to regenerate `api/api.gen.go` and the sqlc code.
3. Add a new migration file in `backend/db/migrations/`. **Never edit a migration that has already run.**

The tools (oapi-codegen, sqlc, goose) are pinned in `backend/tools/go.mod`. Run them with `go tool -modfile=tools/go.mod <tool>`.

---

## Project structure

```
backend/
  api/                 openapi.yaml (the API contract) + generated code
  cmd/api/             entry point, wiring of all contexts, integration tests
  cmd/seed-admin/      create or reset the owner account
  db/migrations/       goose migrations
  db/queries/          sqlc queries, one folder per context
  internal/
    shared/            Money, BillingPeriod, domain events
    platform/          config, postgres, HTTP (errors, auth), storage (S3)
    identity/          admin and tenant accounts, sessions
    tenancy/           rooms, leases, recurring charges, tenant documents + PDPA retention
    billing/           invoices, meter readings, utility rates, payment slips
    maintenance/       maintenance requests
    communication/     announcements, settings, expiring leases
    audit/             audit log of every domain event
frontend/              (empty)
```

Each context is a modular-monolith module with the same layers:

- `domain/`: business rules, with no database or HTTP code
- `app/`: use cases
- `adapter/postgres`: database access
- `adapter/http`: HTTP handlers

---

## Frontend handover

**The API contract is [`backend/api/openapi.yaml`](backend/api/openapi.yaml).** Generate types and the client from it, and do not hand-write duplicate types.

### API conventions

- **Base path:** `/api/v1`.
- **Authentication** uses the httpOnly cookie `bm_session`, which the login endpoints set. JavaScript never sees a token.
  - The simplest local setup is a Vite proxy from `/api` to `http://localhost:8080`. Cross-origin calls need `credentials: 'include'`.
  - `/admin/*` is for the owner only.
  - `/tenant/*` requires a tenant whose account is linked to a room (`ACTIVE`).
  - A `PENDING` tenant can log in. Read `status` from `GET /auth/me` and show the "waiting for room assignment" screen.
  - Admin sessions last 12 hours. Tenant sessions last 30 days and are extended on use.
- **Money** is a decimal string with two places, e.g. `"4512.00"`. Do not compute amounts that will be saved on the client.
- **Times and dates:**
  - Timestamps are ISO 8601 UTC.
  - Dates are `yyyy-mm-dd`.
  - Billing periods are `yyyy-mm`.
- **Errors** are RFC 9457 `application/problem+json` with a stable `code`, such as `INVOICE_NOT_DRAFT` or `CONSENT_REQUIRED`.
  - Map each `code` to a Thai message.
  - Form errors use `VALIDATION_FAILED` (422).
- **Audit log paging:** pass the previous response's `nextCursor` as `?cursor=`. Stop when there is no `nextCursor`.

### File uploads (slips, maintenance photos, documents, payment QR)

Uploads are three steps:

1. Call `POST …/upload-url` with `contentType` and `sizeBytes`. The response contains `uploadUrl`, `headers` and `fileKey`.
2. `PUT` the file directly to `uploadUrl`, sending every header from `headers`. This request does not go through the API.
3. Send the `fileKey` to the confirm endpoint. The API checks the file's type, size and ownership.

File limits:

| File | Types | Max size |
| --- | --- | --- |
| Payment slip | PNG, JPEG | 5 MB |
| Maintenance photo | PNG, JPEG | 5 MB, up to 3 per request |
| Tenant document | PDF, PNG, JPEG | 10 MB; the tenant must accept the consent text first |

URLs for viewing files expire after 5 minutes. Request a fresh one from the API each time you display a file.

**Setting the owner's payment QR code:**

1. Call `POST /admin/settings/payment-qr/upload-url`.
2. `PUT` the image to `uploadUrl`.
3. Call `PUT /admin/settings/payment-qr` with `{ "fileKey": "…" }`.

Tenants get the image from `GET /tenant/payment-qr`. It returns 404 `PAYMENT_QR_NOT_SET` until the owner sets one.

### Main screens and their endpoints

| Screen | Endpoints |
| --- | --- |
| Dashboard | `GET /admin/dashboard` |
| Billing status for a period (all 159 rooms) | `GET /admin/billing-periods/{period}/rooms` |
| Bill a room | 1. `POST /admin/billing-periods/{period}/rooms/{roomNumber}/invoice`<br>2. `PATCH /admin/invoices/{id}/meters`<br>3. `POST /admin/invoices/{id}/lines`<br>4. `POST /admin/invoices/{id}/issue` |
| Slip review | `GET /admin/slips?status=PENDING`<br>`POST /admin/slips/{id}/approve`<br>`POST /admin/slips/{id}/reject` |
| Room map and room details | `GET /admin/rooms`<br>`GET /admin/rooms/{roomNumber}` |
| Tenant accounts | `GET /admin/tenant-accounts?status=`<br>`POST …/assign`, `…/unassign`, `…/password` |
| Tenant bills and slip upload | `GET /tenant/invoices`<br>`POST /tenant/invoices/{id}/slips` |

`openapi.yaml` lists every endpoint (about 60 operations).

### In the design export but not in the system

The design export contains the features below, but the system does not support them. Do not build them in the frontend.

- Owner login with a PIN
- Tenant sign-up and login by phone number (the system uses a username)
- Tenants changing their own password (the owner resets it); Face ID
- OCR for payment slips
- Notes for assigning a technician
- A configurable billing cycle (fixed: cut-off on the 24th, due by the 6th)
- Announcements written by the owner (announcements are created automatically when a utility rate changes)
- Deposits and advance payments
- PDF receipts
- Vehicle registration documents
- Auto-masking
- Tailwind (the frontend uses MUI)
- The owner's name hardcoded in the UI

---

## Open items before going live

- **No login rate limiting yet.** It is planned for production.
- **Consent text v1 needs the owner's review.** It is in migration `00009` and was drafted from the spec. Update it with `PUT /admin/consent-text`, which creates a new version.
- **No production infrastructure yet.**
