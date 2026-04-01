package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

type HealthHandler struct {
	db *sql.DB
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	status := "UP"
	dbStatus := "UP"
	if err := h.db.Ping(); err != nil {
		status = "DOWN"
		dbStatus = "DOWN"
	}
	code := http.StatusOK
	if status == "DOWN" {
		code = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": status,
		"components": map[string]interface{}{
			"db": map[string]string{"status": dbStatus},
		},
	})
}

func (h *HealthHandler) Info(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"app": map[string]string{
			"name":    "easy-host",
			"version": "1.0.0",
		},
	})
}
