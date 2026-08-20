package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/oglimmer/easy-host/internal/middleware"
	"github.com/oglimmer/easy-host/internal/model"
	"github.com/oglimmer/easy-host/internal/service"
)

type APIHandler struct {
	svc       *service.ContentService
	maxUpload int64
}

func NewAPIHandler(svc *service.ContentService, maxUpload int64) *APIHandler {
	return &APIHandler{svc: svc, maxUpload: maxUpload}
}

func (h *APIHandler) ListContent(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	items, _, err := h.svc.List(user.Username, 10000, 0)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []model.ContentResponse{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *APIHandler) GetContent(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	slug := chi.URLParam(r, "slug")
	resp, err := h.svc.Get(slug, user.Username)
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) CreateContent(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if err := r.ParseMultipartForm(h.maxUpload); err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, _ := io.ReadAll(file)

	resp, err := h.svc.Create(service.CreateParams{
		Slug:                   r.FormValue("slug"),
		Owner:                  user.Username,
		Title:                  r.FormValue("title"),
		SourceURL:              r.FormValue("sourceUrl"),
		Creator:                r.FormValue("creator"),
		AllowExternalResources: r.FormValue("allowExternalResources") == "true",
		FileData:               data,
		FileName:               header.Filename,
		Passphrase:             r.FormValue("passphrase"),
	})
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (h *APIHandler) UpdateContent(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	slug := chi.URLParam(r, "slug")
	r.ParseMultipartForm(h.maxUpload)

	var fileData []byte
	var fileName string
	file, header, err := r.FormFile("file")
	if err == nil {
		defer file.Close()
		fileData, _ = io.ReadAll(file)
		fileName = header.Filename
	}

	var title, sourceURL, creator *string
	if v := r.FormValue("title"); v != "" {
		title = &v
	}
	if v := r.FormValue("sourceUrl"); v != "" {
		sourceURL = &v
	}
	if v := r.FormValue("creator"); v != "" {
		creator = &v
	}
	var allowExternalResources *bool
	if v := r.FormValue("allowExternalResources"); v != "" {
		b := v == "true"
		allowExternalResources = &b
	}

	resp, err := h.svc.Update(service.UpdateParams{
		Slug:                   slug,
		Owner:                  user.Username,
		Title:                  title,
		SourceURL:              sourceURL,
		Creator:                creator,
		AllowExternalResources: allowExternalResources,
		FileData:               fileData,
		FileName:               fileName,
		Passphrase:             r.FormValue("passphrase"),
		NewPassphrase:          r.FormValue("newPassphrase"),
		RemoveEncryption:       r.FormValue("removeEncryption") == "true",
	})
	if err != nil {
		h.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) DeleteContent(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	slug := chi.URLParam(r, "slug")
	if err := h.svc.Delete(slug, user.Username); err != nil {
		h.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *APIHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	case errors.Is(err, service.ErrSlugExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Slug already exists"})
	case errors.Is(err, service.ErrInvalidSlug):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid slug format"})
	case errors.Is(err, service.ErrPassphraseRequired):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "This content is encrypted: send its passphrase"})
	case errors.Is(err, service.ErrWrongPassphrase):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Wrong passphrase"})
	case errors.Is(err, service.ErrNotEncrypted):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "This content is not encrypted"})
	default:
		http.Error(w, "Internal error", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
