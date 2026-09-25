package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	instana "github.com/instana/go-sensor"
	"github.com/instana/go-sensor/acceptor"
	"github.com/instana/go-sensor/autoprofile"
)

type alwaysReadyAgent struct{}

func (alwaysReadyAgent) Ready() bool                              { return true }
func (alwaysReadyAgent) SendMetrics(acceptor.Metrics) error       { return nil }
func (alwaysReadyAgent) SendEvent(*instana.EventData) error       { return nil }
func (alwaysReadyAgent) SendSpans([]instana.Span) error           { return nil }
func (alwaysReadyAgent) SendProfiles([]autoprofile.Profile) error { return nil }
func (alwaysReadyAgent) Flush(context.Context) error              { return nil }

func TestTracedRoutes(t *testing.T) {
	recorder := instana.NewTestRecorder()
	tracer := instana.InitCollector(&instana.Options{
		Service:     instanaServiceName,
		AgentClient: alwaysReadyAgent{},
		Recorder:    recorder,
	})
	defer instana.ShutdownCollector()

	st := &fakeStore{products: map[int]Product{7: {ID: 7, Name: "Acetone"}}}
	h := newServer(st, slog.New(slog.NewTextHandler(io.Discard, nil)), tracer).routes()

	// The tracing wrapper must not break path parameters.
	if rec := do(t, h, http.MethodGet, "/api/stock/7", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/stock/7 = %d %q", rec.Code, rec.Body.String())
	}
	spans := recorder.GetQueuedSpans()
	if len(spans) != 1 || spans[0].Name != "g.http" {
		t.Fatalf("got %d spans %+v, want one g.http entry span", len(spans), spans)
	}
	data, ok := spans[0].Data.(instana.HTTPSpanData)
	if !ok || data.Tags.PathTemplate != "/api/stock/{id}" || data.Tags.Status != http.StatusOK {
		t.Errorf("unexpected span data %+v", spans[0].Data)
	}

	// GetQueuedSpans drains the queue, so only spans from here on are counted.
	do(t, h, http.MethodGet, "/healthz", "")
	do(t, h, http.MethodGet, "/readyz", "")
	if n := len(recorder.GetQueuedSpans()); n != 0 {
		t.Errorf("probes produced %d spans, want 0", n)
	}
}
