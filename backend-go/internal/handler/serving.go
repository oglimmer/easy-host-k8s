package handler

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/oglimmer/easy-host/internal/crypto"
	"github.com/oglimmer/easy-host/internal/service"
)

// unlockCookie is the name of the cookie holding a visitor's sealed decryption
// key. It is scoped to the content's own path, so one encrypted site's key is
// never sent to another.
const unlockCookie = "eh_unlock"

// UnlockPrompt renders the passphrase form for encrypted content. It is the
// WebHandler in practice; the interface keeps the template set in one place.
type UnlockPrompt interface {
	RenderUnlock(w http.ResponseWriter, status int, data UnlockPromptData)
}

// UnlockPromptData is the view model for the passphrase form.
type UnlockPromptData struct {
	Slug  string
	Next  string
	Error string
}

type ServingHandler struct {
	svc      *service.ContentService
	unlocker *crypto.Unlocker
	prompt   UnlockPrompt
	// secureCookies marks unlock cookies Secure. Set when the service is served
	// over HTTPS.
	secureCookies bool
}

func NewServingHandler(svc *service.ContentService, unlocker *crypto.Unlocker, prompt UnlockPrompt, secureCookies bool) *ServingHandler {
	return &ServingHandler{svc: svc, unlocker: unlocker, prompt: prompt, secureCookies: secureCookies}
}

func (h *ServingHandler) ServeIndex(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, chi.URLParam(r, "slug"), "index.html")
}

func (h *ServingHandler) ServeFile(w http.ResponseWriter, r *http.Request) {
	filePath := chi.URLParam(r, "*")
	if strings.Contains(filePath, "..") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	h.serveFile(w, r, chi.URLParam(r, "slug"), filePath)
}

// Unlock verifies a passphrase submitted from the prompt and, on success, hands
// the visitor a sealed unlock cookie before sending them back to the page they
// asked for.
func (h *ServingHandler) Unlock(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	r.ParseForm()
	passphrase := r.FormValue("passphrase")
	next := safeNext(slug, r.FormValue("next"))

	dek, err := h.svc.Unlock(slug, passphrase)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPassphraseRequired):
			h.promptUnlock(w, slug, next, "Please enter the passphrase.")
		case errors.Is(err, service.ErrWrongPassphrase):
			h.promptUnlock(w, slug, next, "Wrong passphrase. Please try again.")
		case errors.Is(err, service.ErrNotFound):
			http.Error(w, "Not Found", http.StatusNotFound)
		case errors.Is(err, service.ErrNotEncrypted):
			// Nothing to unlock — just show the content.
			http.Redirect(w, r, next, http.StatusFound)
		default:
			log.Printf("unlock %s: %v", slug, err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
		}
		return
	}

	token, err := h.unlocker.Seal(slug, dek)
	if err != nil {
		log.Printf("sealing unlock token for %s: %v", slug, err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	h.setUnlockCookie(w, slug, token)
	http.Redirect(w, r, next, http.StatusFound)
}

func (h *ServingHandler) serveFile(w http.ResponseWriter, r *http.Request, slug, filePath string) {
	dek := h.dekFromCookie(r, slug)

	f, err := h.svc.GetFile(slug, filePath, dek)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPassphraseRequired):
			h.promptUnlock(w, slug, r.URL.Path, "")
		case errors.Is(err, crypto.ErrCorrupted):
			// The key we were handed no longer opens this content — most likely
			// the passphrase was changed since the visitor unlocked it.
			log.Printf("serving %s/%s: %v", slug, filePath, err)
			h.clearUnlockCookie(w, slug)
			h.promptUnlock(w, slug, r.URL.Path, "This link was locked again. Please enter the passphrase.")
		case errors.Is(err, service.ErrNotFound), errors.Is(err, service.ErrInvalidFilePath):
			http.Error(w, "Not Found", http.StatusNotFound)
		default:
			log.Printf("serving %s/%s: %v", slug, filePath, err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", f.ContentType)
	if f.Encrypted {
		// Decrypted content must never be held by a shared cache, and should not
		// be indexed.
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Add("Vary", "Cookie")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	if f.AllowExternalResources {
		w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline' *; script-src 'self' 'unsafe-inline' *; style-src 'self' 'unsafe-inline' *; img-src 'self' data: *; font-src 'self' *; connect-src 'self' *; frame-ancestors 'none'")
	} else {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; frame-ancestors 'none'")
	}
	w.Write(f.FileData)
}

// promptUnlock renders the passphrase form. It answers 401 so that automated
// clients and sub-resource requests see a failure rather than a page.
func (h *ServingHandler) promptUnlock(w http.ResponseWriter, slug, next, errMsg string) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	h.prompt.RenderUnlock(w, http.StatusUnauthorized, UnlockPromptData{
		Slug:  slug,
		Next:  safeNext(slug, next),
		Error: errMsg,
	})
}

// dekFromCookie recovers the decryption key a visitor already unlocked with,
// returning nil when there is no usable cookie. An expired or forged cookie is
// treated exactly like a missing one: the visitor is asked to unlock again.
func (h *ServingHandler) dekFromCookie(r *http.Request, slug string) []byte {
	c, err := r.Cookie(unlockCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	dek, err := h.unlocker.Open(slug, c.Value)
	if err != nil {
		return nil
	}
	return dek
}

func (h *ServingHandler) setUnlockCookie(w http.ResponseWriter, slug, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     unlockCookie,
		Value:    token,
		Path:     contentPath(slug),
		MaxAge:   int(h.unlocker.TTL().Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *ServingHandler) clearUnlockCookie(w http.ResponseWriter, slug string) {
	http.SetCookie(w, &http.Cookie{
		Name:     unlockCookie,
		Value:    "",
		Path:     contentPath(slug),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// contentPath is the URL prefix a content entry is served under, and the cookie
// path that scopes its unlock key.
func contentPath(slug string) string {
	return "/s/" + slug
}

// safeNext keeps post-unlock redirects inside the content that was unlocked, so
// the form cannot be turned into an open redirect.
func safeNext(slug, next string) string {
	base := contentPath(slug)
	if next == base || strings.HasPrefix(next, base+"/") {
		// Reject anything that could be read as another host, walk back out of
		// the content, or smuggle a control character into the Location header.
		if !strings.ContainsAny(next, "\\\r\n") && !strings.Contains(next, "//") && !strings.Contains(next, "..") {
			return next
		}
	}
	return base
}
