# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**easy-host** is a web content hosting service where users upload HTML/files via API or web UI and serve them on unique slug-based URLs. Built with Spring Boot 3.4 (Java 21), MariaDB, deployed via Docker/Kubernetes with Helm.

## Build & Development Commands

```bash
# Build
cd backend && ./mvnw package

# Unit tests only
cd backend && ./mvnw test

# Integration tests (requires Docker for Testcontainers)
cd backend && ./mvnw verify

# Run single test
cd backend && ./mvnw test -Dtest=ClassName#methodName

# Local development (start DB first)
docker compose up db
cd backend && ./mvnw spring-boot:run

# Full stack
docker compose up

# Helm chart validation
helm lint helm/easy-host/
```

**Local access:** API at `localhost:8080/api/content` (Basic Auth: admin/changeme), Web UI at `localhost:8080/dashboard` (form login).

## Architecture

### Request Flow

Two Spring Security filter chains:
1. **Order 1 (API)**: Basic Auth for `/api/**` and `/actuator/**` — roles: USER, ACTUATOR
2. **Order 2 (Web)**: Form login for dashboard/upload/edit — role: USER

Public serving at `/s/{slug}` requires no auth. Slug resolves to stored files (index.html for base URL).

### Content Lifecycle

- Upload via REST (`POST /api/content`) or Web UI (`/upload`)
- Single file → stored as `index.html`; ZIP → extracted preserving structure (filters `__MACOSX`, hidden files)
- Files stored as `LONGBLOB` in `content_file` table, linked to `content` via FK
- Served publicly at `/s/{slug}` with content-type guessing and 1-hour cache headers

### Key Components

- **ContentController** — REST API CRUD for content
- **WebController** — Thymeleaf-based web UI (dashboard, upload, edit)
- **ServingController** — Public file serving at `/s/{slug}/{path}`
- **ContentService** — Business logic: file handling, ZIP extraction, MIME detection
- **RateLimitFilter** — Guava-based 10 req/sec per IP, 429 on exceed

### Data Model

- `content` — slug (unique, validated: `^[a-z0-9][a-z0-9-]*[a-z0-9]$`), owner, timestamps
- `content_file` — file_path, file_data (LONGBLOB), content_type; FK to content with cascade delete
- Flyway migrations in `src/main/resources/db/migration/`

### Configuration

Environment-variable driven (12-factor). Key settings in `application.yml`:
- Database connection, credentials
- User credentials (actuator + app user) via env vars
- 10MB upload limit, JPA validate mode (schema via Flyway only)

## CI/CD

GitHub Actions (`.github/workflows/build.yml`): builds Docker image on push/PR to main, pushes to `registry.oglimmer.com` on main only. Helm chart deploys to K8s with host `content.oglimmer.com`.
