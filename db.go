package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"slices"
	"time"

	instana "github.com/instana/go-sensor"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Shared by every replica so that only one of them migrates or seeds at a time.
const migrationLockKey = 72417001

func openDB(ctx context.Context, cfg config, tracer instana.TracerLogger, logger *slog.Logger) (*sql.DB, error) {
	driverName := "pgx"
	if tracer != nil {
		instana.InstrumentSQLDriver(tracer, driverName, stdlib.GetDefaultDriver())
		driverName += "_with_instana"
	}
	db, err := sql.Open(driverName, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(cfg.DBMaxOpenConns)
	db.SetMaxIdleConns(cfg.DBMaxIdleConns)
	db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)

	ctx, cancel := context.WithTimeout(ctx, cfg.DBConnectTimeout)
	defer cancel()
	for {
		err = db.PingContext(ctx)
		if err == nil {
			return db, nil
		}
		logger.Warn("database not ready, retrying", "error", err)
		select {
		case <-ctx.Done():
			db.Close()
			return nil, fmt.Errorf("connect to database: %w", err)
		case <-time.After(2 * time.Second):
		}
	}
}

// setupDatabase applies pending migrations and, if asked, seeds sample data.
// It holds a Postgres advisory lock for the whole run so concurrent replicas
// cannot race each other.
func setupDatabase(ctx context.Context, db *sql.DB, seed bool, logger *slog.Logger) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockKey)

	if err := applyMigrations(ctx, conn, logger); err != nil {
		return err
	}
	if seed {
		return seedSampleData(ctx, conn, logger)
	}
	return nil
}

func applyMigrations(ctx context.Context, conn *sql.Conn, logger *slog.Logger) error {
	_, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)

	for _, name := range names {
		version := path.Base(name)
		var applied bool
		err := conn.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied {
			continue
		}
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			return err
		}
		if err := applyMigration(ctx, conn, version, string(body)); err != nil {
			return err
		}
		logger.Info("applied migration", "version", version)
	}
	return nil
}

func applyMigration(ctx context.Context, conn *sql.Conn, version, body string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("apply migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
		return fmt.Errorf("record migration %s: %w", version, err)
	}
	return tx.Commit()
}

var sampleProducts = []struct {
	name, warehouse string
	quantity        int
}{
	{"Paracetamol 500mg", "Dublin-A", 1200}, {"Ibuprofen 400mg", "Dublin-A", 850},
	{"Amoxicillin 250mg", "Dublin-B", 430}, {"Insulin Glargine 100IU", "Frankfurt-1", 75},
	{"Morphine Sulfate 10mg", "Frankfurt-1", 40}, {"Sodium Chloride 0.9%", "London-C", 3000},
	{"Ethanol 96%", "London-C", 500}, {"Hydrogen Peroxide 3%", "Dublin-B", 620},
	{"Adrenaline 1mg/ml", "Frankfurt-1", 90}, {"Diazepam 5mg", "Dublin-A", 310},
	{"Formaldehyde 37%", "London-C", 120}, {"Ceftriaxone 1g", "Dublin-B", 260},
	{"Omeprazole 20mg", "Dublin-A", 980}, {"Chlorhexidine 2%", "London-C", 440},
	{"Heparin 5000IU", "Frankfurt-1", 150}, {"Lidocaine 2%", "Dublin-B", 380},
	{"Acetone", "London-C", 700}, {"T-Compound (sample)", "Frankfurt-1", 3},
}

// seedSampleData inserts the sample products in one transaction, only when
// the products table is empty.
func seedSampleData(ctx context.Context, conn *sql.Conn, logger *slog.Logger) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, p := range sampleProducts {
		_, err := tx.ExecContext(ctx, "INSERT INTO products (name, warehouse, quantity, updated_at) VALUES ($1, $2, $3, NOW())",
			p.name, p.warehouse, p.quantity)
		if err != nil {
			return fmt.Errorf("seed %q: %w", p.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	logger.Info("seeded sample products", "count", len(sampleProducts))
	return nil
}
