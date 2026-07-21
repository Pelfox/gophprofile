package healthcheck

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

const checkTimeout = time.Second

type databasePinger interface {
	Ping(context.Context) error
}

type rabbitMQConnection interface {
	IsClosed() bool
}

// Handler serves liveness and readiness checks.
type Handler struct {
	database databasePinger
	rabbitMQ rabbitMQConnection
}

// NewHandler creates a healthcheck handler backed by the application's live
// database pool and RabbitMQ connection.
func NewHandler(database databasePinger, rabbitMQ rabbitMQConnection) *Handler {
	return &Handler{
		database: database,
		rabbitMQ: rabbitMQ,
	}
}

type response struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// Liveness reports whether the HTTP process is running. External dependency
// failures must not restart an otherwise healthy process.
func (h *Handler) Liveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, response{Status: "ok"})
}

// Readiness reports whether the API can serve requests that depend on
// PostgreSQL and RabbitMQ.
func (h *Handler) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
	defer cancel()

	checks := map[string]string{
		"database": "ok",
		"rabbitmq": "ok",
	}
	statusCode := http.StatusOK
	status := "ready"

	if err := h.database.Ping(ctx); err != nil {
		checks["database"] = "unavailable"
		statusCode = http.StatusServiceUnavailable
		status = "unavailable"
	}
	if h.rabbitMQ.IsClosed() {
		checks["rabbitmq"] = "unavailable"
		statusCode = http.StatusServiceUnavailable
		status = "unavailable"
	}

	writeJSON(w, statusCode, response{
		Status: status,
		Checks: checks,
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload response) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
