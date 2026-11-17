package config

import (
	"strings"
	"testing"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "test")
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost/db")
	t.Setenv("DATABASE_URL_POOLED", "")
	t.Setenv("JWT_ACCESS_SECRET", strings.Repeat("a", 32))
}

func TestLoadValid(t *testing.T) {
	setValidEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "8080" || cfg.AccessTokenTTL.Minutes() != 15 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadPrefersPooledURL(t *testing.T) {
	setValidEnv(t)
	t.Setenv("DATABASE_URL_POOLED", "postgres://pooled/db")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://pooled/db" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestLoadRejectsShortSecret(t *testing.T) {
	setValidEnv(t)
	t.Setenv("JWT_ACCESS_SECRET", "short")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for short JWT secret")
	}
}

func TestLoadRejectsWildcardCORSInProduction(t *testing.T) {
	setValidEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("CORS_ALLOWED_ORIGINS", "*")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for wildcard CORS in production")
	}
}

func TestLoadRequiresDatabase(t *testing.T) {
	setValidEnv(t)
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when database url is missing")
	}
}
