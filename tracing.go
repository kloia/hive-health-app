package main

import (
	"context"
	"fmt"
	"log/slog"

	instana "github.com/instana/go-sensor"
)

const instanaServiceName = "hive"

// initTracing starts the Instana collector, or returns nil when tracing is
// disabled. The agent address and service name can be overridden with the
// sensor's own env vars (INSTANA_AGENT_HOST, INSTANA_AGENT_PORT,
// INSTANA_SERVICE_NAME).
func initTracing(cfg config, logger *slog.Logger) instana.TracerLogger {
	if !cfg.InstanaEnabled {
		return nil
	}
	collector := instana.InitCollector(&instana.Options{
		Service: instanaServiceName,
		Tracer:  instana.DefaultTracerOptions(),
	})
	collector.SetLogger(slogAdapter{logger.With("component", "instana")})
	logger.Info("instana tracing enabled")
	return collector
}

// shutdownTracing sends buffered spans to the agent before the process exits.
func shutdownTracing(ctx context.Context, tracer instana.TracerLogger, logger *slog.Logger) {
	if tracer == nil {
		return
	}
	if err := tracer.Flush(ctx); err != nil {
		logger.Warn("flush instana spans", "error", err)
	}
	instana.ShutdownCollector()
}

// slogAdapter routes the Instana sensor's own logs into our JSON logger.
type slogAdapter struct {
	logger *slog.Logger
}

func (a slogAdapter) Debug(v ...interface{}) { a.logger.Debug(fmt.Sprint(v...)) }
func (a slogAdapter) Info(v ...interface{})  { a.logger.Info(fmt.Sprint(v...)) }
func (a slogAdapter) Warn(v ...interface{})  { a.logger.Warn(fmt.Sprint(v...)) }
func (a slogAdapter) Error(v ...interface{}) { a.logger.Error(fmt.Sprint(v...)) }
