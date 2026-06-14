package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/oglimmer/easy-host/internal/model"
)

// --- OAuth clients (RFC 7591 dynamic registration) ------------------------

func (s *Store) CreateOAuthClient(ctx context.Context, c *model.OAuthClient) error {
	redirectURIs, _ := json.Marshal(c.RedirectURIs)
	grantTypes, _ := json.Marshal(c.GrantTypes)
	responseTypes, _ := json.Marshal(c.ResponseTypes)

	var secret interface{}
	if c.ClientSecretHash != "" {
		secret = c.ClientSecretHash
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO oauth_clients
		 (client_id, client_secret_hash, client_name, redirect_uris, grant_types, response_types, token_endpoint_auth_method, scope, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW())`,
		c.ClientID, secret, c.ClientName, redirectURIs, grantTypes, responseTypes, c.TokenEndpointAuthMethod, c.Scope)
	return err
}

// GetOAuthClient returns the client, or (nil, nil) if it does not exist.
func (s *Store) GetOAuthClient(ctx context.Context, clientID string) (*model.OAuthClient, error) {
	var (
		secretHash    sql.NullString
		clientName    sql.NullString
		redirectURIs  []byte
		grantTypes    []byte
		responseTypes []byte
		authMethod    string
		scope         sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT client_id, client_secret_hash, client_name, redirect_uris, grant_types, response_types, token_endpoint_auth_method, scope
		 FROM oauth_clients WHERE client_id=?`, clientID).
		Scan(&clientID, &secretHash, &clientName, &redirectURIs, &grantTypes, &responseTypes, &authMethod, &scope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	c := &model.OAuthClient{
		ClientID:                clientID,
		ClientSecretHash:        secretHash.String,
		ClientName:              clientName.String,
		TokenEndpointAuthMethod: authMethod,
		Scope:                   scope.String,
	}
	_ = json.Unmarshal(redirectURIs, &c.RedirectURIs)
	_ = json.Unmarshal(grantTypes, &c.GrantTypes)
	_ = json.Unmarshal(responseTypes, &c.ResponseTypes)
	return c, nil
}

// --- Authorization codes --------------------------------------------------

func (s *Store) CreateAuthCode(ctx context.Context, c *model.AuthCode) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO oauth_auth_codes
		 (code, client_id, username, redirect_uri, code_challenge, code_challenge_method, scope, resource, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Code, c.ClientID, c.Username, c.RedirectURI, c.CodeChallenge, c.CodeChallengeMethod, c.Scope, c.Resource, c.ExpiresAt)
	return err
}

// ConsumeAuthCode atomically fetches and deletes an authorization code so it
// can only be redeemed once. Returns (nil, nil) if the code does not exist.
func (s *Store) ConsumeAuthCode(ctx context.Context, code string) (*model.AuthCode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var c model.AuthCode
	err = tx.QueryRowContext(ctx,
		`SELECT code, client_id, username, redirect_uri, code_challenge, code_challenge_method, scope, resource, expires_at
		 FROM oauth_auth_codes WHERE code=? FOR UPDATE`, code).
		Scan(&c.Code, &c.ClientID, &c.Username, &c.RedirectURI, &c.CodeChallenge, &c.CodeChallengeMethod, &c.Scope, &c.Resource, &c.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_auth_codes WHERE code=?`, code); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &c, nil
}

// --- Refresh tokens -------------------------------------------------------

func (s *Store) CreateRefreshToken(ctx context.Context, t *model.RefreshToken) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO oauth_refresh_tokens (token_hash, client_id, username, scope, resource, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		t.TokenHash, t.ClientID, t.Username, t.Scope, t.Resource, t.ExpiresAt)
	return err
}

// ConsumeRefreshToken atomically fetches and deletes a refresh token to support
// rotation. Returns (nil, nil) if the token does not exist.
func (s *Store) ConsumeRefreshToken(ctx context.Context, tokenHash string) (*model.RefreshToken, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var t model.RefreshToken
	err = tx.QueryRowContext(ctx,
		`SELECT token_hash, client_id, username, scope, resource, expires_at
		 FROM oauth_refresh_tokens WHERE token_hash=? FOR UPDATE`, tokenHash).
		Scan(&t.TokenHash, &t.ClientID, &t.Username, &t.Scope, &t.Resource, &t.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_refresh_tokens WHERE token_hash=?`, tokenHash); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &t, nil
}
