package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

type config struct {
	Port              string
	DatabaseURL       string
	AutoMigrate       bool
	SeedSampleData    bool
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration
	DBConnectTimeout  time.Duration
	ShutdownTimeout   time.Duration
}

// loadConfig reads the configuration from the environment. The database is
// configured either with DATABASE_URL or with the DB_* variables; there is no
// default password.
func loadConfig(getenv func(string) string) (config, error) {
	p := envParser{getenv: getenv}
	cfg := config{
		Port:              p.str("PORT", "8080"),
		AutoMigrate:       p.bool("AUTO_MIGRATE", true),
		SeedSampleData:    p.bool("SEED_SAMPLE_DATA", false),
		DBMaxOpenConns:    p.int("DB_MAX_OPEN_CONNS", 10),
		DBMaxIdleConns:    p.int("DB_MAX_IDLE_CONNS", 5),
		DBConnMaxLifetime: p.duration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
		DBConnectTimeout:  p.duration("DB_CONNECT_TIMEOUT", 30*time.Second),
		ShutdownTimeout:   p.duration("SHUTDOWN_TIMEOUT", 15*time.Second),
	}
	cfg.DatabaseURL = p.databaseURL()
	return cfg, errors.Join(p.errs...)
}

type envParser struct {
	getenv func(string) string
	errs   []error
}

func (p *envParser) str(key, def string) string {
	if v := p.getenv(key); v != "" {
		return v
	}
	return def
}

func (p *envParser) bool(key string, def bool) bool {
	v := p.getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s: %w", key, err))
		return def
	}
	return b
}

func (p *envParser) int(key string, def int) int {
	v := p.getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s: %w", key, err))
		return def
	}
	return n
}

func (p *envParser) duration(key string, def time.Duration) time.Duration {
	v := p.getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s: %w", key, err))
		return def
	}
	return d
}

func (p *envParser) databaseURL() string {
	if v := p.getenv("DATABASE_URL"); v != "" {
		return v
	}
	password := p.getenv("DB_PASSWORD")
	if password == "" {
		p.errs = append(p.errs, errors.New("DB_PASSWORD or DATABASE_URL must be set"))
		return ""
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(p.str("DB_USER", "hive"), password),
		Host:     net.JoinHostPort(p.str("DB_HOST", "localhost"), p.str("DB_PORT", "5432")),
		Path:     "/" + p.str("DB_NAME", "hive"),
		RawQuery: url.Values{"sslmode": {p.str("DB_SSLMODE", "prefer")}}.Encode(),
	}
	return u.String()
}

// dbTarget returns host/dbname for logging, never credentials.
func dbTarget(databaseURL string) string {
	u, err := url.Parse(databaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "(dsn)"
	}
	return u.Host + u.Path
}
