package handler

import (
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/oglimmer/easy-host/internal/auth"
	"github.com/oglimmer/easy-host/internal/middleware"
	"github.com/oglimmer/easy-host/internal/service"
)

// OIDCLogout is the interface the WebHandler needs from OIDCHandler for logout.
type OIDCLogout interface {
	LogoutURL() string
}

type WebHandler struct {
	svc          *service.ContentService
	users        *auth.UserStore
	sessions     *sessions.CookieStore
	templates    *template.Template
	maxUpload    int64
	oidcEnabled  bool
	oidcLogout   OIDCLogout
	oidcClientID string
	baseURL      string
}

func NewWebHandler(svc *service.ContentService, users *auth.UserStore, sessionStore *sessions.CookieStore, tmplDir string, maxUpload int64, oidcEnabled bool, oidcLogout OIDCLogout, oidcClientID, baseURL string) *WebHandler {
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"seq": func(start, end int) []int {
			var s []int
			for i := start; i <= end; i++ {
				s = append(s, i)
			}
			return s
		},
	}
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob(filepath.Join(tmplDir, "*.html")))
	return &WebHandler{
		svc:         svc,
		users:       users,
		sessions:    sessionStore,
		templates:   tmpl,
		maxUpload:   maxUpload,
		oidcEnabled:  oidcEnabled,
		oidcLogout:   oidcLogout,
		oidcClientID: oidcClientID,
		baseURL:      baseURL,
	}
}

func (h *WebHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	session, _ := h.sessions.Get(r, "session")
	if username, ok := session.Values["username"].(string); ok && username != "" {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	data := map[string]interface{}{
		"Error":       r.URL.Query().Get("error"),
		"OIDCEnabled": h.oidcEnabled,
	}
	h.render(w, "login.html", data)
}

func (h *WebHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	username := r.FormValue("username")
	password := r.FormValue("password")

	user := h.users.Authenticate(username, password)
	if user == nil || user.Role != "USER" {
		http.Redirect(w, r, "/login?error=Invalid+credentials", http.StatusFound)
		return
	}
	session, _ := h.sessions.Get(r, "session")
	session.Values["username"] = user.Username
	session.Save(r, w)
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *WebHandler) Logout(w http.ResponseWriter, r *http.Request) {
	session, _ := h.sessions.Get(r, "session")
	username, _ := session.Values["username"].(string)
	delete(session.Values, "username")
	session.Save(r, w)

	// For OIDC users, redirect to the provider's end_session_endpoint
	if h.oidcLogout != nil && strings.Contains(username, "|") {
		if endSessionURL := h.oidcLogout.LogoutURL(); endSessionURL != "" {
			redirectURL := endSessionURL + "?post_logout_redirect_uri=" + url.QueryEscape(h.baseURL+"/login") + "&client_id=" + url.QueryEscape(h.oidcClientID)
			http.Redirect(w, r, redirectURL, http.StatusFound)
			return
		}
	}

	http.Redirect(w, r, "/login", http.StatusFound)
}

func (h *WebHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	pageSize := 10
	if ps, err := strconv.Atoi(r.URL.Query().Get("size")); err == nil {
		switch ps {
		case 10, 25, 50, 100:
			pageSize = ps
		}
	}
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}
	offset := (page - 1) * pageSize

	items, total, err := h.svc.List(user.Username, pageSize, offset)
	if err != nil {
		log.Printf("dashboard error: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	session, _ := h.sessions.Get(r, "session")
	flash := getFlash(session, r, w)
	h.render(w, "dashboard.html", map[string]interface{}{
		"User":       user.Username,
		"Items":      items,
		"Count":      total,
		"Page":       page,
		"PageSize":   pageSize,
		"TotalPages": totalPages,
		"Success":    flash["success"],
		"Error":      flash["error"],
	})
}

func (h *WebHandler) UploadPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	h.render(w, "upload.html", map[string]interface{}{"User": user.Username})
}

func (h *WebHandler) UploadSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if err := r.ParseMultipartForm(h.maxUpload); err != nil {
		h.setFlash(w, r, "error", "File too large (max 10MB)")
		http.Redirect(w, r, "/upload", http.StatusFound)
		return
	}

	slug := r.FormValue("slug")
	title := r.FormValue("title")
	sourceURL := r.FormValue("sourceUrl")
	creator := r.FormValue("creator")
	allowExternalResources := r.FormValue("allowExternalResources") == "true"

	file, header, err := r.FormFile("file")
	if err != nil {
		h.setFlash(w, r, "error", "File is required")
		http.Redirect(w, r, "/upload", http.StatusFound)
		return
	}
	defer file.Close()
	data, _ := io.ReadAll(file)

	_, err = h.svc.Create(slug, data, header.Filename, user.Username, title, sourceURL, creator, allowExternalResources)
	if err != nil {
		h.setFlash(w, r, "error", err.Error())
		http.Redirect(w, r, "/upload", http.StatusFound)
		return
	}
	h.setFlash(w, r, "success", "Content '"+slug+"' uploaded successfully")
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *WebHandler) EditPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	slug := chi.URLParam(r, "slug")
	content, err := h.svc.Get(slug, user.Username)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	h.render(w, "edit.html", map[string]interface{}{
		"User":    user.Username,
		"Content": content,
	})
}

func (h *WebHandler) EditSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	slug := chi.URLParam(r, "slug")
	r.ParseMultipartForm(h.maxUpload)

	title := r.FormValue("title")
	sourceURL := r.FormValue("sourceUrl")
	creator := r.FormValue("creator")
	allowExternal := r.FormValue("allowExternalResources") == "true"

	var fileData []byte
	var fileName string
	file, header, err := r.FormFile("file")
	if err == nil {
		defer file.Close()
		fileData, _ = io.ReadAll(file)
		fileName = header.Filename
	}

	_, err = h.svc.Update(slug, user.Username, fileData, fileName, &title, &sourceURL, &creator, &allowExternal)
	if err != nil {
		h.setFlash(w, r, "error", err.Error())
		http.Redirect(w, r, "/edit/"+slug, http.StatusFound)
		return
	}
	h.setFlash(w, r, "success", "Content '"+slug+"' updated successfully")
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *WebHandler) DeleteSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	slug := chi.URLParam(r, "slug")
	if err := h.svc.Delete(slug, user.Username); err != nil {
		h.setFlash(w, r, "error", "Failed to delete: "+err.Error())
	} else {
		h.setFlash(w, r, "success", "Content '"+slug+"' deleted")
	}
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *WebHandler) DevelopersPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "developers.html", nil)
}

func (h *WebHandler) ImprintPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "imprint.html", nil)
}

func (h *WebHandler) PrivacyPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "privacy.html", nil)
}

func (h *WebHandler) TermsPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "terms.html", nil)
}

func (h *WebHandler) render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
	}
}

func (h *WebHandler) setFlash(w http.ResponseWriter, r *http.Request, key, msg string) {
	session, _ := h.sessions.Get(r, "session")
	session.Values["flash_"+key] = msg
	session.Save(r, w)
}

func getFlash(session *sessions.Session, r *http.Request, w http.ResponseWriter) map[string]string {
	flash := map[string]string{}
	for _, key := range []string{"success", "error"} {
		if v, ok := session.Values["flash_"+key].(string); ok && v != "" {
			flash[key] = v
			delete(session.Values, "flash_"+key)
		}
	}
	session.Save(r, w)
	return flash
}
