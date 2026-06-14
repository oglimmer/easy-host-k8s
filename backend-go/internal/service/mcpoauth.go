package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/oglimmer/easy-host/internal/model"
	"github.com/oglimmer/easy-host/internal/store"
)

const (
	// MCPScope is the scope carried by MCP access tokens. It distinguishes
	// tokens minted through the MCP OAuth flow from other credentials, so only
	// these are accepted at the /mcp endpoint.
	MCPScope = "mcp"

	accessTokenTTL  = time.Hour
	authCodeTTL     = 5 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
)

// MCPOAuthService implements the OAuth 2.1 authorization-server logic backing
// the MCP endpoint: dynamic client registration (RFC 7591), authorization-code
// issuance with PKCE, and token issuance/refresh. Access tokens are HMAC-signed
// JWTs whose audience is bound to the MCP resource (RFC 8707). User identity is
// the easy-host owner username.
type MCPOAuthService struct {
	store      *store.Store
	baseURL    string
	signSecret []byte
}

func NewMCPOAuthService(s *store.Store, baseURL, signSecret string) *MCPOAuthService {
	return &MCPOAuthService{
		store:      s,
		baseURL:    strings.TrimRight(baseURL, "/"),
		signSecret: []byte(signSecret),
	}
}

// OAuthError is a protocol-level error rendered per RFC 6749 §5.2.
type OAuthError struct {
	Code        string
	Description string
	Status      int
}

func (e *OAuthError) Error() string { return e.Code + ": " + e.Description }

func oauthErr(status int, code, desc string) *OAuthError {
	return &OAuthError{Code: code, Description: desc, Status: status}
}

// ---- URLs / identifiers --------------------------------------------------

func (s *MCPOAuthService) Issuer() string   { return s.baseURL }
func (s *MCPOAuthService) Resource() string { return s.baseURL + "/mcp" }

func (s *MCPOAuthService) authorizationEndpoint() string { return s.baseURL + "/oauth/authorize" }
func (s *MCPOAuthService) tokenEndpoint() string         { return s.baseURL + "/oauth/token" }
func (s *MCPOAuthService) registrationEndpoint() string  { return s.baseURL + "/oauth/register" }

// ProtectedResourceMetadataURL is the RFC 9728 document URL advertised in the
// WWW-Authenticate header on 401s from /mcp.
func (s *MCPOAuthService) ProtectedResourceMetadataURL() string {
	return s.baseURL + "/.well-known/oauth-protected-resource"
}

// ---- Metadata documents --------------------------------------------------

type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

func (s *MCPOAuthService) ProtectedResourceMetadata() ProtectedResourceMetadata {
	return ProtectedResourceMetadata{
		Resource:               s.Resource(),
		AuthorizationServers:   []string{s.Issuer()},
		ScopesSupported:        []string{MCPScope},
		BearerMethodsSupported: []string{"header"},
	}
}

type AuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

func (s *MCPOAuthService) AuthorizationServerMetadata() AuthorizationServerMetadata {
	return AuthorizationServerMetadata{
		Issuer:                            s.Issuer(),
		AuthorizationEndpoint:             s.authorizationEndpoint(),
		TokenEndpoint:                     s.tokenEndpoint(),
		RegistrationEndpoint:              s.registrationEndpoint(),
		ScopesSupported:                   []string{MCPScope},
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none", "client_secret_post", "client_secret_basic"},
	}
}

// ---- Dynamic client registration (RFC 7591) ------------------------------

// ClientRegistrationRequest is the subset of RFC 7591 client metadata we accept.
type ClientRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

// ClientRegistrationResponse is the RFC 7591 registration response. ClientSecret
// is omitted for public clients.
type ClientRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope,omitempty"`
}

func (s *MCPOAuthService) RegisterClient(ctx context.Context, req ClientRegistrationRequest) (*ClientRegistrationResponse, *OAuthError) {
	if len(req.RedirectURIs) == 0 {
		return nil, oauthErr(http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
	}
	for _, u := range req.RedirectURIs {
		parsed, err := url.Parse(u)
		if err != nil || !parsed.IsAbs() {
			return nil, oauthErr(http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris must be absolute URIs")
		}
	}

	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code"}
	}
	responseTypes := req.ResponseTypes
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}
	authMethod := req.TokenEndpointAuthMethod
	if authMethod == "" {
		authMethod = "none"
	}
	if authMethod != "none" && authMethod != "client_secret_post" && authMethod != "client_secret_basic" {
		return nil, oauthErr(http.StatusBadRequest, "invalid_client_metadata", "unsupported token_endpoint_auth_method")
	}

	clientID, err := cryptoRandString(32)
	if err != nil {
		return nil, oauthErr(http.StatusInternalServerError, "server_error", "failed to generate client id")
	}

	client := &model.OAuthClient{
		ClientID:                clientID,
		ClientName:              req.ClientName,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              grantTypes,
		ResponseTypes:           responseTypes,
		TokenEndpointAuthMethod: authMethod,
		Scope:                   req.Scope,
	}

	var plaintextSecret string
	if authMethod != "none" {
		plaintextSecret, err = cryptoRandString(48)
		if err != nil {
			return nil, oauthErr(http.StatusInternalServerError, "server_error", "failed to generate client secret")
		}
		client.ClientSecretHash = hashToken(plaintextSecret)
	}

	if err := s.store.CreateOAuthClient(ctx, client); err != nil {
		return nil, oauthErr(http.StatusInternalServerError, "server_error", "failed to persist client")
	}

	resp := &ClientRegistrationResponse{
		ClientID:                client.ClientID,
		ClientSecret:            plaintextSecret,
		ClientIDIssuedAt:        time.Now().Unix(),
		ClientName:              client.ClientName,
		RedirectURIs:            client.RedirectURIs,
		GrantTypes:              client.GrantTypes,
		ResponseTypes:           client.ResponseTypes,
		TokenEndpointAuthMethod: client.TokenEndpointAuthMethod,
		Scope:                   client.Scope,
	}
	return resp, nil
}

func (s *MCPOAuthService) GetClient(ctx context.Context, clientID string) (*model.OAuthClient, error) {
	return s.store.GetOAuthClient(ctx, clientID)
}

// ---- Authorization code issuance -----------------------------------------

// IssueAuthorizationCode creates and stores a one-time authorization code for
// the given authenticated user, bound to the client, redirect URI, PKCE
// challenge, and requested resource.
func (s *MCPOAuthService) IssueAuthorizationCode(ctx context.Context, clientID, username, redirectURI, codeChallenge, codeChallengeMethod, scope, resource string) (string, error) {
	code, err := cryptoRandString(48)
	if err != nil {
		return "", err
	}
	ac := &model.AuthCode{
		Code:                code,
		ClientID:            clientID,
		Username:            username,
		RedirectURI:         redirectURI,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		Scope:               scope,
		Resource:            resource,
		ExpiresAt:           time.Now().Add(authCodeTTL),
	}
	if err := s.store.CreateAuthCode(ctx, ac); err != nil {
		return "", err
	}
	return code, nil
}

// ---- Token endpoint ------------------------------------------------------

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// ExchangeAuthorizationCode redeems an authorization code for tokens, enforcing
// client identity, redirect URI match, and PKCE (S256).
func (s *MCPOAuthService) ExchangeAuthorizationCode(ctx context.Context, clientID, clientSecret, redirectURI, code, codeVerifier string) (*TokenResponse, *OAuthError) {
	client, oerr := s.authenticateClient(ctx, clientID, clientSecret)
	if oerr != nil {
		return nil, oerr
	}

	if code == "" || codeVerifier == "" {
		return nil, oauthErr(http.StatusBadRequest, "invalid_request", "code and code_verifier are required")
	}

	ac, err := s.store.ConsumeAuthCode(ctx, code)
	if err != nil {
		return nil, oauthErr(http.StatusInternalServerError, "server_error", "failed to read authorization code")
	}
	if ac == nil {
		return nil, oauthErr(http.StatusBadRequest, "invalid_grant", "unknown or already-used authorization code")
	}
	if time.Now().After(ac.ExpiresAt) {
		return nil, oauthErr(http.StatusBadRequest, "invalid_grant", "authorization code expired")
	}
	if ac.ClientID != client.ClientID {
		return nil, oauthErr(http.StatusBadRequest, "invalid_grant", "authorization code was issued to another client")
	}
	if ac.RedirectURI != redirectURI {
		return nil, oauthErr(http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
	}
	if !verifyPKCE(codeVerifier, ac.CodeChallenge, ac.CodeChallengeMethod) {
		return nil, oauthErr(http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
	}

	return s.issueTokens(ctx, client.ClientID, ac.Username, ac.Resource)
}

// ExchangeRefreshToken issues a new access token (and rotates the refresh token)
// for a valid refresh token.
func (s *MCPOAuthService) ExchangeRefreshToken(ctx context.Context, clientID, clientSecret, refreshToken string) (*TokenResponse, *OAuthError) {
	client, oerr := s.authenticateClient(ctx, clientID, clientSecret)
	if oerr != nil {
		return nil, oerr
	}
	if refreshToken == "" {
		return nil, oauthErr(http.StatusBadRequest, "invalid_request", "refresh_token is required")
	}

	rt, err := s.store.ConsumeRefreshToken(ctx, hashToken(refreshToken))
	if err != nil {
		return nil, oauthErr(http.StatusInternalServerError, "server_error", "failed to read refresh token")
	}
	if rt == nil {
		return nil, oauthErr(http.StatusBadRequest, "invalid_grant", "unknown or already-used refresh token")
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, oauthErr(http.StatusBadRequest, "invalid_grant", "refresh token expired")
	}
	if rt.ClientID != client.ClientID {
		return nil, oauthErr(http.StatusBadRequest, "invalid_grant", "refresh token was issued to another client")
	}

	return s.issueTokens(ctx, client.ClientID, rt.Username, rt.Resource)
}

// issueTokens mints an access token (JWT) and a rotated refresh token.
func (s *MCPOAuthService) issueTokens(ctx context.Context, clientID, username, resource string) (*TokenResponse, *OAuthError) {
	accessToken, expiresIn, err := s.generateAccessToken(username)
	if err != nil {
		return nil, oauthErr(http.StatusInternalServerError, "server_error", "failed to mint access token")
	}

	refreshPlaintext, err := cryptoRandString(48)
	if err != nil {
		return nil, oauthErr(http.StatusInternalServerError, "server_error", "failed to mint refresh token")
	}
	rt := &model.RefreshToken{
		TokenHash: hashToken(refreshPlaintext),
		ClientID:  clientID,
		Username:  username,
		Scope:     MCPScope,
		Resource:  resource,
		ExpiresAt: time.Now().Add(refreshTokenTTL),
	}
	if err := s.store.CreateRefreshToken(ctx, rt); err != nil {
		return nil, oauthErr(http.StatusInternalServerError, "server_error", "failed to persist refresh token")
	}

	return &TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    expiresIn,
		RefreshToken: refreshPlaintext,
		Scope:        MCPScope,
	}, nil
}

// generateAccessToken mints an MCP access-token JWT bound to the user and to the
// MCP resource (audience), signed with the HMAC secret.
func (s *MCPOAuthService) generateAccessToken(username string) (string, int, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":   username,
		"scope": MCPScope,
		"aud":   s.Resource(),
		"iss":   s.Issuer(),
		"iat":   now.Unix(),
		"exp":   now.Add(accessTokenTTL).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.signSecret)
	if err != nil {
		return "", 0, err
	}
	return signed, int(accessTokenTTL.Seconds()), nil
}

// VerifiedToken holds the validated claims of an MCP access token.
type VerifiedToken struct {
	Username  string
	Scopes    []string
	ExpiresAt time.Time
}

// VerifyAccessToken validates an MCP access token: signature, expiry, MCP scope,
// and audience binding to this resource. Used by the /mcp bearer middleware.
func (s *MCPOAuthService) VerifyAccessToken(tokenStr string) (*VerifiedToken, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return s.signSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims")
	}

	scope, _ := claims["scope"].(string)
	scopes := strings.Fields(scope)
	if !containsString(scopes, MCPScope) {
		return nil, fmt.Errorf("token missing %q scope", MCPScope)
	}

	// Audience binding (RFC 8707): reject tokens not minted for this resource.
	if !audienceMatches(claims["aud"], s.Resource()) {
		return nil, fmt.Errorf("token audience mismatch")
	}

	username, _ := claims["sub"].(string)
	if username == "" {
		return nil, fmt.Errorf("token missing subject")
	}

	var expiresAt time.Time
	if expFloat, ok := claims["exp"].(float64); ok {
		expiresAt = time.Unix(int64(expFloat), 0)
	}

	return &VerifiedToken{
		Username:  username,
		Scopes:    scopes,
		ExpiresAt: expiresAt,
	}, nil
}

// authenticateClient looks up the client and, for confidential clients, verifies
// the presented secret.
func (s *MCPOAuthService) authenticateClient(ctx context.Context, clientID, clientSecret string) (*model.OAuthClient, *OAuthError) {
	if clientID == "" {
		return nil, oauthErr(http.StatusBadRequest, "invalid_client", "client_id is required")
	}
	client, err := s.store.GetOAuthClient(ctx, clientID)
	if err != nil {
		return nil, oauthErr(http.StatusInternalServerError, "server_error", "failed to read client")
	}
	if client == nil {
		return nil, oauthErr(http.StatusUnauthorized, "invalid_client", "unknown client")
	}
	if !client.IsPublic() {
		if clientSecret == "" {
			return nil, oauthErr(http.StatusUnauthorized, "invalid_client", "client authentication required")
		}
		if subtle.ConstantTimeCompare([]byte(hashToken(clientSecret)), []byte(client.ClientSecretHash)) != 1 {
			return nil, oauthErr(http.StatusUnauthorized, "invalid_client", "invalid client secret")
		}
	}
	return client, nil
}

// ---- helpers -------------------------------------------------------------

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// verifyPKCE checks a code_verifier against a stored challenge. Only S256 is
// supported, as required by OAuth 2.1 / MCP.
func verifyPKCE(verifier, challenge, method string) bool {
	if method != "S256" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

func containsString(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

// audienceMatches reports whether the JWT "aud" claim (string or []string)
// contains the expected resource.
func audienceMatches(aud interface{}, expected string) bool {
	switch v := aud.(type) {
	case string:
		return v == expected
	case []interface{}:
		for _, a := range v {
			if s, ok := a.(string); ok && s == expected {
				return true
			}
		}
	}
	return false
}

// cryptoRandString returns a cryptographically random alphanumeric string of
// the given length using crypto/rand.
func cryptoRandString(n int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		b[i] = alphabet[idx.Int64()]
	}
	return string(b), nil
}
