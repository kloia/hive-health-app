package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"
)

const (
	maxBodyBytes   = 64 << 10
	movementsLimit = 100
	reportsLimit   = 50
)

//go:embed templates/index.html
var indexHTML string

type server struct {
	store  store
	tmpl   *template.Template
	logger *slog.Logger
}

func newServer(st store, logger *slog.Logger) *server {
	return &server{
		store:  st,
		tmpl:   template.Must(template.New("index").Parse(indexHTML)),
		logger: logger,
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleLive)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /health", s.handleReady) // kept for existing monitors
	mux.HandleFunc("GET /api/stock", s.handleListStock)
	mux.HandleFunc("POST /api/stock", s.handleCreateMovement)
	// mobil uygulama bu endpoint'i kullaniyor, degistirme!
	mux.HandleFunc("GET /api/stock/{id}", s.handleGetStock)
	mux.HandleFunc("GET /api/movements", s.handleListMovements)
	mux.HandleFunc("GET /api/reports", s.handleListReports)
	mux.HandleFunc("GET /{$}", s.handleIndex)
	return s.logRequests(mux)
}

func (s *server) handleLive(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		s.logger.Warn("readiness check failed", "error", err)
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error"})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleListStock(w http.ResponseWriter, r *http.Request) {
	products, err := s.store.ListProducts(r.Context(), orderByID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, products)
}

func (s *server) handleGetStock(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	p, err := s.store.GetProduct(r.Context(), id)
	if errors.Is(err, errNotFound) {
		http.Error(w, "product not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, p)
}

func (s *server) handleCreateMovement(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var m Movement
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if msg := validateMovement(m); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	created, err := s.store.ApplyMovement(r.Context(), m)
	switch {
	case errors.Is(err, errNotFound):
		http.Error(w, "product not found", http.StatusNotFound)
		return
	case errors.Is(err, errInsufficientStock):
		http.Error(w, "insufficient stock", http.StatusConflict)
		return
	case err != nil:
		s.serverError(w, r, err)
		return
	}
	s.logger.Info("stock movement recorded",
		"movement_id", created.ID, "product_id", created.ProductID, "delta", created.Delta, "note", created.Note)
	s.writeJSON(w, http.StatusCreated, created)
}

func validateMovement(m Movement) string {
	switch {
	case m.ProductID <= 0:
		return "product_id must be a positive integer"
	case m.Delta == 0:
		return "delta must be non-zero"
	case m.Delta > math.MaxInt32 || m.Delta < math.MinInt32:
		return "delta is out of range"
	}
	return ""
}

func (s *server) handleListMovements(w http.ResponseWriter, r *http.Request) {
	movements, err := s.store.ListMovements(r.Context(), movementsLimit)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, movements)
}

func (s *server) handleListReports(w http.ResponseWriter, r *http.Request) {
	reports, err := s.store.ListReports(r.Context(), reportsLimit)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, reports)
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	products, err := s.store.ListProducts(r.Context(), orderByWarehouse)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	// Render into a buffer so a template error becomes a clean 500.
	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, products); err != nil {
		s.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// serverError logs the real error and returns a generic message, so database
// details never reach the client.
func (s *server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func (s *server) writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.logger.Warn("write JSON response", "error", err)
	}
}

func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		switch r.URL.Path {
		case "/healthz", "/readyz", "/health":
			return
		}
		s.logger.Info("http request",
			"method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
