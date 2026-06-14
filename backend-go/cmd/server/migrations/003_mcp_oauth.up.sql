-- OAuth 2.1 Authorization Server tables backing the MCP endpoint.
-- easy-host acts as its own Authorization Server (with RFC 7591 Dynamic Client
-- Registration) and Resource Server for /mcp. Access tokens are HMAC-signed JWTs,
-- so no access-token table is needed; only dynamically registered clients,
-- one-time authorization codes, and refresh tokens are persisted.
--
-- User identity is the owner username (a string), matching the rest of the app
-- (users live in config/OIDC, not a DB table), so there is no FK to a users table.

CREATE TABLE IF NOT EXISTS oauth_clients (
    client_id                  VARCHAR(64) PRIMARY KEY,
    client_secret_hash         VARCHAR(128),
    client_name                VARCHAR(255),
    redirect_uris              JSON NOT NULL,
    grant_types                JSON NOT NULL,
    response_types             JSON NOT NULL,
    token_endpoint_auth_method VARCHAR(64) NOT NULL DEFAULT 'none',
    scope                      VARCHAR(1024),
    created_at                 TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS oauth_auth_codes (
    code                  VARCHAR(128) PRIMARY KEY,
    client_id             VARCHAR(64) NOT NULL,
    username              VARCHAR(255) NOT NULL,
    redirect_uri          VARCHAR(2048) NOT NULL,
    code_challenge        VARCHAR(255) NOT NULL,
    code_challenge_method VARCHAR(16) NOT NULL,
    scope                 VARCHAR(1024),
    resource              VARCHAR(2048),
    expires_at            TIMESTAMP NOT NULL,
    created_at            TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (
    token_hash  VARCHAR(128) PRIMARY KEY,
    client_id   VARCHAR(64) NOT NULL,
    username    VARCHAR(255) NOT NULL,
    scope       VARCHAR(1024),
    resource    VARCHAR(2048),
    expires_at  TIMESTAMP NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
