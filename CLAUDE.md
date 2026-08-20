# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**easy-host** is a web content hosting service where users upload HTML/files via API or web UI and serve them on unique slug-based URLs. Built with Go 1.24, MariaDB, deployed via Docker/Kubernetes with Helm.

## Build & Development Commands

```bash
# Build
cd backend-go && CGO_ENABLED=0 go build -o server ./cmd/server

# Run tests
cd backend-go && go test ./...

# Run single test
cd backend-go && go test -run TestName ./internal/service/

# Local development (start DB first)
docker compose up db
cd backend-go && go run ./cmd/server

# Full stack via Docker
docker compose up

# Helm chart validation
helm lint helm/easy-host/
```

**Local access:** API at `localhost:8080/api/content` (Basic Auth: admin/changeme), Web UI at `localhost:8080/dashboard` (form login).

## Architecture

### Backend (backend-go/)

Go application using chi router, plain `database/sql` (no ORM), and `html/template` for server-side rendering.

**Layer structure:**
- `cmd/server/main.go` — entry point, wiring, route definitions, embedded SQL migrations
- `internal/handler/` — HTTP handlers: `api.go` (REST CRUD), `web.go` (UI), `serving.go` (public file serving), `oidc.go` (optional OIDC auth), `mcpoauth.go` (MCP OAuth 2.1 authorization server), `health.go` (actuator)
- `internal/mcp/` — Model Context Protocol server (`/mcp`, Streamable HTTP via the official Go SDK) exposing content CRUD as tools
- `internal/crypto/` — at-rest encryption for passphrase-protected content: Argon2id KDF, XChaCha20-Poly1305 envelope encryption, and the sealed unlock tokens visitors carry in a cookie
- `internal/service/` — business logic: validation, ZIP extraction, MIME detection; `mcpoauth.go` (DCR, PKCE, token issuance/verification)
- `internal/store/` — data access layer with raw SQL queries; `oauth.go` (OAuth client/code/refresh-token persistence)
- `internal/model/` — data structures
- `internal/middleware/` — request logging, security headers, rate limiting (10 req/sec per IP), BasicAuth, SessionAuth
- `internal/auth/` — in-memory user store with bcrypt
- `internal/config/` — env-var config loading (supports `SPRING_DATASOURCE_URL` for backward compat)

### Request Flow

Two auth mechanisms:
1. **BasicAuth**: for `/api/content/**` (role: USER) and `/actuator/**` (role: ACTUATOR)
2. **SessionAuth**: cookie-based for web UI (`/dashboard`, `/upload`, `/edit`, `/delete`)

Public serving at `/s/{slug}` requires no auth. Optional OIDC authentication via env vars.

3. **MCP OAuth bearer**: `/mcp` is protected by OAuth 2.1 bearer tokens. easy-host acts as its own
   Authorization Server with Dynamic Client Registration (RFC 7591), authorization-code + PKCE, and
   metadata discovery (RFC 9728/8414). The authorize endpoint reuses the web session login. Access
   tokens are HMAC-signed JWTs (scope `mcp`, audience = `BASE_URL + /mcp`); the user identity carried
   is the owner username. Endpoints (`/oauth/*`, `/.well-known/*`, `/mcp`) sit under a CORS group.
4. **Passphrase unlock**: encrypted content served from `/s/{slug}` needs no account, but answers 401
   with a passphrase prompt until the visitor posts the passphrase to `/unlock/{slug}`; the resulting
   sealed cookie (path-scoped to `/s/{slug}`, TTL `UNLOCK_TTL`) carries the data key for later
   requests.

### Content Lifecycle

- Upload via REST (`POST /api/content`) or Web UI (`/upload`)
- Single file → stored as `index.html`; ZIP → extracted preserving structure (filters `__MACOSX`, hidden files)
- Files stored as `LONGBLOB` in `content_file` table, linked to `content` via FK
- Served publicly at `/s/{slug}` with content-type detection and cache headers
- Optional at-rest encryption: a passphrase at upload derives an Argon2id key that wraps a random
  per-content data key; files are sealed with XChaCha20-Poly1305, bound by AAD to their slug and file
  path. Only the wrapped key is stored, so a lost passphrase means lost content. Encryption can be
  added, rotated (rekeys, invalidating open sessions), or removed on update; replacing the files of an
  encrypted entry requires the current passphrase. Encrypted responses are `no-store`/`noindex`.

### Data Model

- `content` — slug (unique, validated: `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`), owner, timestamps, and the
  encryption envelope (`enc_kdf`, `enc_salt`, `enc_wrapped_key`; all NULL when unencrypted)
- `content_file` — file_path, file_data (LONGBLOB, `nonce || ciphertext || tag` when the content is
  encrypted), content_type; FK to content with cascade delete
- `oauth_clients` / `oauth_auth_codes` / `oauth_refresh_tokens` — MCP OAuth state (dynamically registered clients, one-time PKCE auth codes, hashed refresh tokens); keyed by owner `username`, no FK to a users table
- Migrations in `cmd/server/migrations/`, embedded via `//go:embed` and applied on startup with golang-migrate

### Configuration

Environment-variable driven (12-factor). Key vars: `PORT`, `DATABASE_URL` (or `SPRING_DATASOURCE_URL`), `DB_HOST`/`DB_PORT`/`DB_NAME`, `SPRING_DATASOURCE_USERNAME`/`PASSWORD`, `APP_ADMIN_USERNAME`/`PASSWORD`, `ACTUATOR_USERNAME`/`PASSWORD`, `SESSION_SECRET`, `OIDC_ISSUER_URL`/`OIDC_CLIENT_ID`/`OIDC_CLIENT_SECRET`/`OIDC_ALLOWED_USERS`, `BASE_URL` (OAuth issuer / MCP resource), `MCP_TOKEN_SECRET` (MCP access-token signing key; defaults to `SESSION_SECRET`), `UNLOCK_TOKEN_SECRET` (seals unlock cookies; defaults to `SESSION_SECRET`), `UNLOCK_TTL` (Go duration, default `12h`). 10MB upload limit.

## CI/CD

GitHub Actions (`.github/workflows/build.yml`): builds Docker image on push/PR to main, pushes to `registry.oglimmer.com` on main only. Helm chart in `helm/easy-host/` deploys to K8s with host `content.oglimmer.com`.

**Note:** CI still references `backend/` (former Spring Boot). The Go backend lives in `backend-go/`.
