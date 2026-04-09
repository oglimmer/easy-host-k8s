# easy-host

Simple web content hosting service. Upload HTML/files via API, serve them on unique URLs.

## Tech Stack

- **Backend:** Go 1.24, chi router, html/template
- **Database:** MariaDB 11 with golang-migrate
- **Deployment:** Docker, Helm, Kubernetes
- **CI/CD:** GitHub Actions

## Local Development

Start the database and backend with Docker Compose:

```bash
docker compose up
```

The backend is available at `http://localhost:8080`.

### Run backend locally (against dockerized DB)

```bash
docker compose up db
cd backend-go
go run ./cmd/server
```

### Run tests

```bash
cd backend-go
go test ./...
```

## Configuration

Environment variables:

| Variable | Description | Default |
|---|---|---|
| `DATABASE_URL` | Go DSN connection string | — |
| `SPRING_DATASOURCE_URL` | JDBC URL (parsed for backward compat) | — |
| `DB_HOST` | Database host | `localhost` |
| `DB_PORT` | Database port | `3306` |
| `DB_NAME` | Database name | `easyhost` |
| `SPRING_DATASOURCE_USERNAME` | DB username | `easyhost` |
| `SPRING_DATASOURCE_PASSWORD` | DB password | `easyhost` |
| `ACTUATOR_USERNAME` | Actuator endpoint username | `actuator` |
| `ACTUATOR_PASSWORD` | Actuator endpoint password | `changeme` |
| `APP_ADMIN_USERNAME` | Application admin username | `admin` |
| `APP_ADMIN_PASSWORD` | Application admin password | `changeme` |
| `SESSION_SECRET` | Cookie session secret | (default) |
| `PORT` | Server listen port | `8080` |

### Optional OIDC

| Variable | Description |
|---|---|
| `OIDC_ISSUER_URL` | OIDC provider issuer URL (enables OIDC when set) |
| `OIDC_CLIENT_ID` | OIDC client ID |
| `OIDC_CLIENT_SECRET` | OIDC client secret |
| `OIDC_ALLOWED_USERS` | Comma-separated list of allowed OIDC users |

## Deployment

A Helm chart is provided in `helm/easy-host/`. The CI pipeline builds and pushes a Docker image on every push to `main`.
