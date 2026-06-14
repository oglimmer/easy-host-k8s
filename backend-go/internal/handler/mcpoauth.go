package handler

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/gorilla/sessions"
	"github.com/oglimmer/easy-host/internal/service"
)

// postLoginRedirectKey is the session value under which the MCP authorize
// endpoint stashes its own URL before bouncing an unauthenticated user to the
// login page. After a successful login the web/OIDC handlers resume here.
const postLoginRedirectKey = "post_login_redirect"

// MCPOAuthHandler serves the OAuth 2.1 authorization-server endpoints that back
// the MCP resource: protected-resource / authorization-server metadata,
// dynamic client registration, the authorize endpoint, and the token endpoint.
type MCPOAuthHandler struct {
	svc      *service.MCPOAuthService
	sessions *sessions.CookieStore
}

func NewMCPOAuthHandler(svc *service.MCPOAuthService, sessionStore *sessions.CookieStore) *MCPOAuthHandler {
	return &MCPOAuthHandler{svc: svc, sessions: sessionStore}
}

// ProtectedResourceMetadata serves the RFC 9728 document.
func (h *MCPOAuthHandler) ProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.ProtectedResourceMetadata())
}

// AuthorizationServerMetadata serves the RFC 8414 document.
func (h *MCPOAuthHandler) AuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.AuthorizationServerMetadata())
}

// Register implements RFC 7591 dynamic client registration.
func (h *MCPOAuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req service.ClientRegistrationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "invalid JSON body")
		return
	}
	resp, oerr := h.svc.RegisterClient(r.Context(), req)
	if oerr != nil {
		writeOAuthError(w, oerr.Status, oerr.Code, oerr.Description)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// Authorize implements the OAuth 2.1 authorization endpoint with PKCE. If the
// user is not yet logged in, it stashes this request and bounces through the
// existing login flow, resuming here afterward.
func (h *MCPOAuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	responseType := q.Get("response_type")
	state := q.Get("state")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	scope := q.Get("scope")
	resource := q.Get("resource")

	// Validate the client and redirect URI before trusting redirect_uri as a
	// place to send error responses.
	client, err := h.svc.GetClient(r.Context(), clientID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if client == nil {
		http.Error(w, "invalid client_id", http.StatusBadRequest)
		return
	}
	if !client.AllowsRedirectURI(redirectURI) {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}

	// From here, protocol errors are returned by redirecting to redirect_uri.
	if responseType != "code" {
		redirectAuthError(w, r, redirectURI, state, "unsupported_response_type", "only response_type=code is supported")
		return
	}
	if codeChallenge == "" {
		redirectAuthError(w, r, redirectURI, state, "invalid_request", "code_challenge is required (PKCE)")
		return
	}
	if codeChallengeMethod != "S256" {
		redirectAuthError(w, r, redirectURI, state, "invalid_request", "code_challenge_method must be S256")
		return
	}
	if resource != "" && resource != h.svc.Resource() {
		redirectAuthError(w, r, redirectURI, state, "invalid_target", "resource does not match this server")
		return
	}

	// Identify the user from the existing web-session cookie.
	username, ok := h.authenticatedUser(r)
	if !ok {
		// Not logged in: stash this request and bounce to the login page.
		session, _ := h.sessions.Get(r, "session")
		session.Values[postLoginRedirectKey] = r.URL.RequestURI()
		session.Save(r, w)
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	code, err := h.svc.IssueAuthorizationCode(r.Context(), clientID, username, redirectURI, codeChallenge, codeChallengeMethod, scope, resource)
	if err != nil {
		redirectAuthError(w, r, redirectURI, state, "server_error", "failed to issue authorization code")
		return
	}

	u, _ := url.Parse(redirectURI)
	rq := u.Query()
	rq.Set("code", code)
	if state != "" {
		rq.Set("state", state)
	}
	u.RawQuery = rq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// Token implements the OAuth 2.1 token endpoint (authorization_code and
// refresh_token grants).
func (h *MCPOAuthHandler) Token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "could not parse form")
		return
	}

	clientID, clientSecret := clientCredentials(r)
	grantType := r.PostFormValue("grant_type")

	var (
		resp *service.TokenResponse
		oerr *service.OAuthError
	)
	switch grantType {
	case "authorization_code":
		resp, oerr = h.svc.ExchangeAuthorizationCode(
			r.Context(), clientID, clientSecret,
			r.PostFormValue("redirect_uri"), r.PostFormValue("code"), r.PostFormValue("code_verifier"))
	case "refresh_token":
		resp, oerr = h.svc.ExchangeRefreshToken(
			r.Context(), clientID, clientSecret, r.PostFormValue("refresh_token"))
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type")
		return
	}

	if oerr != nil {
		writeOAuthError(w, oerr.Status, oerr.Code, oerr.Description)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, resp)
}

// authenticatedUser returns the username from the web-session cookie, if present.
func (h *MCPOAuthHandler) authenticatedUser(r *http.Request) (string, bool) {
	session, _ := h.sessions.Get(r, "session")
	username, ok := session.Values["username"].(string)
	if !ok || username == "" {
		return "", false
	}
	return username, true
}

// PopPostLoginRedirect returns and clears a stashed MCP authorize URL, if any.
// The web and OIDC login handlers call this after a successful login to resume
// an in-progress MCP authorization instead of going to the dashboard.
func PopPostLoginRedirect(session *sessions.Session) (string, bool) {
	dest, ok := session.Values[postLoginRedirectKey].(string)
	if !ok || dest == "" {
		return "", false
	}
	delete(session.Values, postLoginRedirectKey)
	return dest, true
}

// clientCredentials extracts client_id / client_secret from HTTP Basic auth
// (client_secret_basic) or the request body (client_secret_post / public).
func clientCredentials(r *http.Request) (clientID, clientSecret string) {
	if id, secret, ok := r.BasicAuth(); ok {
		return id, secret
	}
	return r.PostFormValue("client_id"), r.PostFormValue("client_secret")
}

func redirectAuthError(w http.ResponseWriter, r *http.Request, redirectURI, state, code, desc string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, code+": "+desc, http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("error", code)
	q.Set("error_description", desc)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{
		"error":             code,
		"error_description": desc,
	})
}
