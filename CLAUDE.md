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
- `internal/handler/` — HTTP handlers: `api.go` (REST CRUD), `web.go` (UI), `serving.go` (public file serving), `oidc.go` (optional OIDC auth), `health.go` (actuator)
- `internal/service/` — business logic: validation, ZIP extraction, MIME detection
- `internal/store/` — data access layer with raw SQL queries
- `internal/model/` — data structures
- `internal/middleware/` — request logging, security headers, rate limiting (10 req/sec per IP), BasicAuth, SessionAuth
- `internal/auth/` — in-memory user store with bcrypt
- `internal/config/` — env-var config loading (supports `SPRING_DATASOURCE_URL` for backward compat)

### Request Flow

Two auth mechanisms:
1. **BasicAuth**: for `/api/content/**` (role: USER) and `/actuator/**` (role: ACTUATOR)
2. **SessionAuth**: cookie-based for web UI (`/dashboard`, `/upload`, `/edit`, `/delete`)

Public serving at `/s/{slug}` requires no auth. Optional OIDC authentication via env vars.

### Content Lifecycle

- Upload via REST (`POST /api/content`) or Web UI (`/upload`)
- Single file → stored as `index.html`; ZIP → extracted preserving structure (filters `__MACOSX`, hidden files)
- Files stored as `LONGBLOB` in `content_file` table, linked to `content` via FK
- Served publicly at `/s/{slug}` with content-type detection and cache headers

### Data Model

- `content` — slug (unique, validated: `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`), owner, timestamps
- `content_file` — file_path, file_data (LONGBLOB), content_type; FK to content with cascade delete
- Migrations in `cmd/server/migrations/`, embedded via `//go:embed` and applied on startup with golang-migrate

### Configuration

Environment-variable driven (12-factor). Key vars: `PORT`, `DATABASE_URL` (or `SPRING_DATASOURCE_URL`), `DB_HOST`/`DB_PORT`/`DB_NAME`, `SPRING_DATASOURCE_USERNAME`/`PASSWORD`, `APP_ADMIN_USERNAME`/`PASSWORD`, `ACTUATOR_USERNAME`/`PASSWORD`, `SESSION_SECRET`, `OIDC_ISSUER_URL`/`OIDC_CLIENT_ID`/`OIDC_CLIENT_SECRET`/`OIDC_ALLOWED_USERS`. 10MB upload limit.

## CI/CD

GitHub Actions (`.github/workflows/build.yml`): builds Docker image on push/PR to main, pushes to `registry.oglimmer.com` on main only. Helm chart in `helm/easy-host/` deploys to K8s with host `content.oglimmer.com`.

**Note:** CI still references `backend/` (former Spring Boot). The Go backend lives in `backend-go/`.
