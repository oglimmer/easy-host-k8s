package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/oglimmer/easy-host/internal/service"
)

type ServingHandler struct {
	svc *service.ContentService
}

func NewServingHandler(svc *service.ContentService) *ServingHandler {
	return &ServingHandler{svc: svc}
}

func (h *ServingHandler) ServeIndex(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	h.serveFile(w, slug, "index.html")
}

func (h *ServingHandler) ServeFile(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	filePath := chi.URLParam(r, "*")
	if strings.Contains(filePath, "..") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	h.serveFile(w, slug, filePath)
}

func (h *ServingHandler) serveFile(w http.ResponseWriter, slug, filePath string) {
	f, err := h.svc.GetFile(slug, filePath)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) || errors.Is(err, service.ErrInvalidFilePath) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", f.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if f.AllowExternalResources {
		w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline' *; script-src 'self' 'unsafe-inline' *; style-src 'self' 'unsafe-inline' *; img-src 'self' data: *; font-src 'self' *; connect-src 'self' *; frame-ancestors 'none'")
	} else {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; frame-ancestors 'none'")
	}
	w.Write(f.FileData)
}
