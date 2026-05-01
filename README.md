# UX Anomalies API (v1)

Go backend for `api.uxanomalies.com` behind a Next.js BFF.
Default runtime target is Google Cloud Run + Neon + Google Cloud Storage (GCS).

## Key security model

- Browser never calls this API directly.
- Every `/v1/**` request requires `X-BFF-SECRET`.
- Missing/invalid `X-BFF-SECRET` returns `403 Forbidden`.
- Authenticated routes also require `Authorization: Bearer <app_token>`.

## Current implementation status

This backend currently includes:
- Router + middleware chain (request ID, logging, recover, BFF secret, auth, rate limit)
- Endpoint handlers for all requested routes
- OAuth upsert/linking domain service (Google/GitHub)
- PostgreSQL-backed runtime services for profile/anomalies/images/likes/comments/feed
- PostgreSQL migrations + sqlc query files
- GCS-first image upload adapter (ADC-based auth)
- Dockerfile, docker-compose, Makefile

## Run locally (Neon + GCS, no local DB/storage)

Important `.env` note:
- If your DB URL contains `&`, wrap the value in single quotes in `.env`.
- Example: `DATABASE_URL='postgres://...&sslmode=require'`
- Same for `MIGRATIONS_DATABASE_URL`.

1. Create local env from template:
   - `cp .env.example .env`
2. Fill `.env` with your real local values (Neon URL + GCS values).
3. Authenticate with ADC on your machine:
   - `gcloud auth application-default login`
4. Run migrations against Neon:
   - `make migrate-up`
5. Run API:
   - `make run`

API starts at `http://localhost:8080`.

## Useful commands

- `make test`
- `make fmt`
- `make tidy`
- `make sqlc`
- `make migrate-up` (uses `MIGRATIONS_DATABASE_URL` or falls back to `DATABASE_URL`)

## Environment variables

Use `.env.example` as the canonical template. Do not put real secrets in git.

Required at startup (fail-fast if missing):
- `DATABASE_URL`
- `BFF_SECRET`
- `JWT_SIGNING_KEY`
- `STORAGE_BACKEND`
- `GCP_PROJECT_ID` (when `STORAGE_BACKEND=gcs`)
- `GCS_BUCKET` (when `STORAGE_BACKEND=gcs`)
- `GCS_UPLOAD_PREFIX` (when `STORAGE_BACKEND=gcs`)
- `GCS_SIGNED_URL_TTL` (must be > 0 when `STORAGE_BACKEND=gcs`)

Migration connection:
- `MIGRATIONS_DATABASE_URL` (optional)
- If not set, migrations use `DATABASE_URL`

Configured variables:
- `APP_ENV` (`development` or `production` only)
- `APP_ADDR`
- `LOG_LEVEL`
- `DATABASE_URL`
- `MIGRATIONS_DATABASE_URL`
- `BFF_SECRET`
- `JWT_ISSUER`
- `JWT_AUDIENCE`
- `JWT_SIGNING_KEY`
- `JWT_ACCESS_TTL`
- `MAX_BODY_BYTES`
- `MAX_MULTIPART_BYTES`
- `MAX_IMAGE_BYTES`
- `RATE_LIMIT_BACKEND`
- `STORAGE_BACKEND` (`gcs` default)
- `GCP_PROJECT_ID`
- `GCS_BUCKET`
- `GCS_UPLOAD_PREFIX`
- `GCS_SIGNED_URL_TTL`
- `CORS_ALLOWED_ORIGINS`

Google Cloud note:
- No static service-account key file is required in code.
- Local development uses ADC from your developer machine.
- Cloud Run uses its attached service account.

Production note:
- `.env` is for local development convenience only. In production, inject env vars from your runtime/secret manager.

## Local execution variants

### A) Preferred flow (app/tools read `.env` directly)
1. `gcloud auth application-default login --impersonate-service-account=uxa-api-sa@ux-anomalies.iam.gserviceaccount.com`
2. `make migrate-up`
3. `go test ./...`
4. `make run`

### B) Shell-export flow (if you want manual env export)
1. `gcloud auth application-default login --impersonate-service-account=uxa-api-sa@ux-anomalies.iam.gserviceaccount.com`
2. `set -a; source .env; set +a`
3. `make migrate-up`
4. `go test ./...`
5. `make run`

## Required Google Cloud resources

- One GCP project.
- Cloud Storage bucket for anomaly images (private bucket).
- Cloud Run service account with GCS permissions:
  - object create/read on the bucket
  - permission to sign URLs (via IAM Credentials API role support)
- Enabled APIs:
  - Cloud Storage
  - IAM Service Account Credentials API (for signed URLs)

## Endpoint summary

Public (BFF-gated):  
`GET /v1/feed?q=&category=&author_nickname=&cursor=&limit=`  
`GET /v1/users/search?q=&cursor=&limit=`  
`GET /v1/public/anomalies/sitemap`  
`GET /v1/public/anomalies/{slug}`  
`GET /v1/anomalies/{id}`  
`GET /v1/anomalies/{id}/comments`  
`GET /v1/users/{nickname}` (supports URL-encoded nickname lookup)

Authenticated (BFF + Bearer token):  
`GET /v1/me`  
`PATCH /v1/me`  
`POST /v1/anomalies`  
`PATCH /v1/anomalies/{id}` (description/category/source_url only; no image mutation in v1)  
`POST /v1/anomalies/{id}/like`  
`DELETE /v1/anomalies/{id}/like`  
`POST /v1/anomalies/{id}/comments`

Internal auth (BFF only):  
`POST /v1/internal/auth/oauth-upsert`

## Production hardening notes

- Rotate `BFF_SECRET` and signing keys via secret manager.
- Use asymmetric JWT signing keys for multi-service verification.
- Move rate limiting to Redis for multi-instance deployments.
- Enforce MIME + magic-byte validation for uploads in production path.
- Add anti-abuse protections for internal auth endpoint and anomaly/comment creation.
- Add DB-backed repositories and transactional counter updates for like/comment counts.
