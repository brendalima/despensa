package rest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// The split is deliberate. /healthz answers whether the process is up
// and touches nothing: it is what the platform's health check calls,
// and a sleeping database must not restart a healthy machine.
// /readyz answers whether the pantry can actually be read, and is what
// a person or a deploy asks before believing anything.
const readyTimeout = 3 * time.Second

type Health struct {
	db Pinger
}

func NewHealth(db Pinger) (*Health, error) {
	if db == nil {
		return nil, errors.New("missing database dependency")
	}
	return &Health{db: db}, nil
}

func MustNewHealth(db Pinger) *Health {
	h, err := NewHealth(db)
	if err != nil {
		panic(fmt.Errorf("couldn't create health handler: %w", err))
	}
	return h
}

func (h *Health) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		slog.Warn("not ready", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
