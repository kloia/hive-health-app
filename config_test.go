package main

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfigRequiresPassword(t *testing.T) {
	_, err := loadConfig(envMap(nil))
	if err == nil || !strings.Contains(err.Error(), "DB_PASSWORD") {
		t.Fatalf("err = %v, want DB_PASSWORD error", err)
	}
}

func TestLoadConfigBuildsURL(t *testing.T) {
	cfg, err := loadConfig(envMap(map[string]string{
		"DB_HOST":          "db.internal",
		"DB_PASSWORD":      "p@ss:w/rd",
		"DB_SSLMODE":       "require",
		"SHUTDOWN_TIMEOUT": "5s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if pw, _ := u.User.Password(); pw != "p@ss:w/rd" {
		t.Errorf("password = %q, not round-tripped", pw)
	}
	if u.Host != "db.internal:5432" || u.Path != "/hive" || u.Query().Get("sslmode") != "require" {
		t.Errorf("unexpected URL %s", u.Redacted())
	}
	if cfg.ShutdownTimeout != 5*time.Second || !cfg.AutoMigrate || cfg.SeedSampleData {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if got := dbTarget(cfg.DatabaseURL); strings.Contains(got, "p@ss") {
		t.Errorf("dbTarget leaked password: %q", got)
	}
}

func TestLoadConfigRejectsBadValues(t *testing.T) {
	_, err := loadConfig(envMap(map[string]string{
		"DATABASE_URL":      "postgres://x",
		"AUTO_MIGRATE":      "maybe",
		"DB_MAX_OPEN_CONNS": "ten",
	}))
	if err == nil || !strings.Contains(err.Error(), "AUTO_MIGRATE") || !strings.Contains(err.Error(), "DB_MAX_OPEN_CONNS") {
		t.Fatalf("err = %v, want both bad keys reported", err)
	}
}

func TestDBTargetDoesNotLeakKeyValueDSN(t *testing.T) {
	if got := dbTarget("host=db password=secret"); strings.Contains(got, "secret") {
		t.Fatalf("dbTarget leaked password: %q", got)
	}
}
