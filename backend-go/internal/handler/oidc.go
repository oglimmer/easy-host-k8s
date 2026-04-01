package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
)

type OIDCHandler struct {
	provider     *oidc.Provider
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
	sessions     *sessions.CookieStore
	allowedUsers map[string]bool
}

func NewOIDCHandler(issuerURL, clientID, clientSecret, baseURL string, allowedUsers []string, sessionStore *sessions.CookieStore) (*OIDCHandler, error) {
	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, err
	}

	allowed := make(map[string]bool, len(allowedUsers))
	for _, u := range allowedUsers {
		allowed[u] = true
	}

	return &OIDCHandler{
		provider: provider,
		oauth2Config: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  baseURL + "/auth/callback",
			Scopes:       []string{oidc.ScopeOpenID, "email"},
		},
		verifier:     provider.Verifier(&oidc.Config{ClientID: clientID}),
		sessions:     sessionStore,
		allowedUsers: allowed,
	}, nil
}

func (h *OIDCHandler) Login(w http.ResponseWriter, r *http.Request) {
	state := generateState()
	session, _ := h.sessions.Get(r, "session")
	session.Values["oauth_state"] = state
	session.Save(r, w)
	http.Redirect(w, r, h.oauth2Config.AuthCodeURL(state), http.StatusFound)
}

func (h *OIDCHandler) Callback(w http.ResponseWriter, r *http.Request) {
	session, _ := h.sessions.Get(r, "session")

	// Verify state
	savedState, _ := session.Values["oauth_state"].(string)
	delete(session.Values, "oauth_state")
	if savedState == "" || savedState != r.URL.Query().Get("state") {
		http.Redirect(w, r, "/login?error=Invalid+OAuth+state", http.StatusFound)
		return
	}

	// Exchange code for token
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/login?error=No+authorization+code", http.StatusFound)
		return
	}

	token, err := h.oauth2Config.Exchange(r.Context(), code)
	if err != nil {
		log.Printf("OIDC token exchange error: %v", err)
		http.Redirect(w, r, "/login?error=Authentication+failed", http.StatusFound)
		return
	}

	// Extract and verify ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Redirect(w, r, "/login?error=No+ID+token", http.StatusFound)
		return
	}

	idToken, err := h.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		log.Printf("OIDC token verification error: %v", err)
		http.Redirect(w, r, "/login?error=Token+verification+failed", http.StatusFound)
		return
	}

	// Extract email claim
	var claims struct {
		Email string `json:"email"`
		Sub   string `json:"sub"`
	}
	if err := idToken.Claims(&claims); err != nil {
		log.Printf("OIDC claims error: %v", err)
		http.Redirect(w, r, "/login?error=Failed+to+read+claims", http.StatusFound)
		return
	}

	// Check allow-list (if configured)
	if len(h.allowedUsers) > 0 && !h.allowedUsers[claims.Email] {
		log.Printf("OIDC login denied for %s (not in allowed users)", claims.Email)
		http.Redirect(w, r, "/login?error=Access+denied", http.StatusFound)
		return
	}

	// Use issuer+subject as username (same as Java version)
	username := idToken.Issuer + "|" + claims.Sub
	session.Values["username"] = username
	session.Save(r, w)

	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *OIDCHandler) LogoutURL() string {
	// Try to get end_session_endpoint from provider
	var providerClaims struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	if err := h.provider.Claims(&providerClaims); err == nil && providerClaims.EndSessionEndpoint != "" {
		return providerClaims.EndSessionEndpoint
	}
	return ""
}

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}
