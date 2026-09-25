package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeStore struct {
	pingErr     error
	products    map[int]Product
	applyErr    error
	listErr     error
	lastApplied Movement
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) ListProducts(context.Context, productOrder) ([]Product, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := []Product{}
	for _, p := range f.products {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeStore) GetProduct(_ context.Context, id int) (Product, error) {
	p, ok := f.products[id]
	if !ok {
		return Product{}, errNotFound
	}
	return p, nil
}

func (f *fakeStore) ApplyMovement(_ context.Context, m Movement) (Movement, error) {
	f.lastApplied = m
	if f.applyErr != nil {
		return m, f.applyErr
	}
	m.ID = 1
	return m, nil
}

func (f *fakeStore) ListMovements(context.Context, int) ([]Movement, error) { return []Movement{}, nil }
func (f *fakeStore) ListReports(context.Context, int) ([]Report, error)     { return []Report{}, nil }

func newTestServer(st *fakeStore) http.Handler {
	return newServer(st, slog.New(slog.NewTextHandler(io.Discard, nil))).routes()
}

func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGetStock(t *testing.T) {
	h := newTestServer(&fakeStore{products: map[int]Product{7: {ID: 7, Name: "Acetone"}}})

	tests := []struct {
		target   string
		wantCode int
		wantBody string
	}{
		{"/api/stock/7", http.StatusOK, `"name":"Acetone"`},
		{"/api/stock/8", http.StatusNotFound, "product not found"},
		{"/api/stock/abc", http.StatusBadRequest, "invalid id"},
	}
	for _, tt := range tests {
		rec := do(t, h, http.MethodGet, tt.target, "")
		if rec.Code != tt.wantCode || !strings.Contains(rec.Body.String(), tt.wantBody) {
			t.Errorf("GET %s = %d %q, want %d containing %q", tt.target, rec.Code, rec.Body.String(), tt.wantCode, tt.wantBody)
		}
	}
}

func TestCreateMovement(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		applyErr error
		wantCode int
		wantBody string
	}{
		{"ok", `{"product_id":1,"delta":-5,"note":"sold"}`, nil, http.StatusCreated, `"id":1`},
		{"bad json", `{`, nil, http.StatusBadRequest, "invalid JSON body"},
		{"missing product", `{"delta":1}`, nil, http.StatusBadRequest, "product_id"},
		{"zero delta", `{"product_id":1,"delta":0}`, nil, http.StatusBadRequest, "delta must be non-zero"},
		{"huge delta", `{"product_id":1,"delta":99999999999}`, nil, http.StatusBadRequest, "out of range"},
		{"unknown product", `{"product_id":1,"delta":1}`, errNotFound, http.StatusNotFound, "product not found"},
		{"insufficient", `{"product_id":1,"delta":-1}`, errInsufficientStock, http.StatusConflict, "insufficient stock"},
		{"db error hidden", `{"product_id":1,"delta":1}`, errors.New(`pq: relation "products" does not exist`),
			http.StatusInternalServerError, "internal server error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestServer(&fakeStore{applyErr: tt.applyErr})
			rec := do(t, h, http.MethodPost, "/api/stock", tt.body)
			if rec.Code != tt.wantCode || !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("got %d %q, want %d containing %q", rec.Code, rec.Body.String(), tt.wantCode, tt.wantBody)
			}
			if strings.Contains(rec.Body.String(), "relation") {
				t.Fatalf("database error leaked to client: %q", rec.Body.String())
			}
		})
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newTestServer(&fakeStore{})
	for _, target := range []string{"/api/stock", "/api/stock/1", "/api/movements", "/api/reports"} {
		if rec := do(t, h, http.MethodDelete, target, ""); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("DELETE %s = %d, want 405", target, rec.Code)
		}
	}
}

func TestProbes(t *testing.T) {
	down := newTestServer(&fakeStore{pingErr: errors.New("dial tcp 10.0.0.5:5432: connection refused")})
	if rec := do(t, down, http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
		t.Errorf("/healthz with DB down = %d, want 200", rec.Code)
	}
	for _, target := range []string{"/readyz", "/health"} {
		rec := do(t, down, http.MethodGet, target, "")
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s with DB down = %d, want 503", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "10.0.0.5") {
			t.Errorf("%s leaked DB error: %q", target, rec.Body.String())
		}
	}
}

func TestIndex(t *testing.T) {
	h := newTestServer(&fakeStore{products: map[int]Product{1: {ID: 1, Name: "Acetone", Quantity: 3}}})
	rec := do(t, h, http.MethodGet, "/", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `class="low">3<`) {
		t.Fatalf("GET / = %d %q", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, http.MethodGet, "/nope", ""); rec.Code != http.StatusNotFound {
		t.Errorf("GET /nope = %d, want 404", rec.Code)
	}
}
