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
| `MCP_TOKEN_SECRET` | HMAC secret for signing MCP access tokens | `SESSION_SECRET` |
| `UNLOCK_TOKEN_SECRET` | Secret sealing the unlock cookies of encrypted content | `SESSION_SECRET` |
| `UNLOCK_TTL` | How long one passphrase entry keeps encrypted content viewable | `12h` |
| `BASE_URL` | Public base URL, used as the OAuth issuer / MCP resource | `http://localhost:$PORT` |
| `PORT` | Server listen port | `8080` |

### Optional OIDC

| Variable | Description |
|---|---|
| `OIDC_ISSUER_URL` | OIDC provider issuer URL (enables OIDC when set) |
| `OIDC_CLIENT_ID` | OIDC client ID |
| `OIDC_CLIENT_SECRET` | OIDC client secret |
| `OIDC_ALLOWED_USERS` | Comma-separated list of allowed OIDC users |

## Encrypted content

Content can be protected with a passphrase, supplied at upload time (web form, `passphrase` field on
the REST API, or `passphrase` on the MCP `create_content` tool). Its files are then encrypted at rest
and a visitor of `/s/{slug}` is asked for the passphrase before anything is served.

**Scheme** (`backend-go/internal/crypto`) — envelope encryption:

- The passphrase derives a key-encryption key with **Argon2id** (RFC 9106: 64 MiB, t=3, p=4, random
  16-byte salt per entry).
- That key wraps a random per-entry **data-encryption key**, which encrypts each file with
  **XChaCha20-Poly1305** (random 192-bit nonce per payload).
- Only the wrapped key is stored: the passphrase is never persisted and cannot be recovered — if it
  is lost, so is the content.
- Every ciphertext is bound to its location with additional authenticated data (slug, plus file path
  for file payloads), so payloads cannot be moved between entries or paths undetected.

**Visitor flow** — `GET /s/{slug}` answers `401` with a passphrase prompt; `POST /unlock/{slug}`
verifies it and sets a sealed, `HttpOnly` cookie scoped to `/s/{slug}` that carries the data key for
`UNLOCK_TTL` (default 12h), so sub-resources load without re-prompting. Encrypted responses are sent
`Cache-Control: private, no-store` and `X-Robots-Tag: noindex`.

**Managing it** — an owner can add a passphrase to existing content, rotate it (`newPassphrase`), or
remove encryption (`removeEncryption=true`) through the edit form, the REST API, or the MCP
`update_content` tool; replacing the files of an encrypted entry requires its current passphrase.
Rotating re-encrypts with a fresh key, so already-unlocked visitors lose access immediately.

Titles, slugs and other metadata are **not** encrypted — only the file payloads are.

## MCP server

The backend exposes a [Model Context Protocol](https://modelcontextprotocol.io) endpoint at `/mcp`
(Streamable HTTP transport) so AI clients can manage hosted content as tools. Tools:
`list_content`, `get_content`, `create_content`, `update_content`, `delete_content` — each scoped to
the authenticated owner, mirroring the REST API (including the encryption options above).

easy-host is its own **OAuth 2.1 Authorization Server** for the endpoint, so MCP clients connect with
no manual setup beyond the URL:

- **Discovery** — `/.well-known/oauth-protected-resource` (RFC 9728) and
  `/.well-known/oauth-authorization-server` (RFC 8414)
- **Dynamic Client Registration** (RFC 7591) — `POST /oauth/register`
- **Authorization Code + PKCE** (S256, required) — `GET /oauth/authorize`, `POST /oauth/token`

The authorize step reuses the normal web login (form login or OIDC); an unauthenticated client is
bounced through `/login` and resumed automatically. Access tokens are HMAC-signed JWTs (1h TTL, scope
`mcp`, audience bound to `BASE_URL + /mcp`); refresh tokens are supported and rotated. Set `BASE_URL`
to the public origin in production so the advertised issuer/resource URLs are correct.

Point an MCP client at `${BASE_URL}/mcp` — e.g. `https://content.oglimmer.com/mcp`.

## Deployment

A Helm chart is provided in `helm/easy-host/`. The CI pipeline builds and pushes a Docker image on every push to `main`.
