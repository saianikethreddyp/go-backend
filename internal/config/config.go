// Package config loads and validates runtime configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
	EnvTest        = "test"

	minJWTSecretLen = 32
)

// Config holds all runtime settings. It is immutable after Load.
type Config struct {
	Env  string
	Port string

	// DatabaseURL is the connection string the API uses at runtime. It prefers
	// DATABASE_URL_POOLED (Neon pooler) and falls back to DATABASE_URL.
	DatabaseURL string

	JWTAccessSecret string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	SentryDSN     string
	Release       string
	CORSOrigins   []string
	ProxyHops     int
	RateLimit     int // requests per minute per client, all routes
	LoginRateLimt int // requests per minute per client, login only

	// Contacts (stored only to trace a car after theft or misuse).
	ContactsMasterKey     string // base64, at least 32 bytes
	ContactsRetentionDays int
	RequireContactsToBook bool

	// One-time codes.
	OTPProvider string // "console" (development only)
	OTPTTL      time.Duration

	// Object storage for car photos (all optional together).
	S3Endpoint        string
	S3Region          string
	S3Bucket          string
	S3AccessKeyID     string
	S3SecretAccessKey string

	// Push notifications. Base64 of a Firebase service account JSON; optional.
	FirebaseCredentialsB64 string
}

// IsProduction reports whether the service runs in production mode.
func (c *Config) IsProduction() bool { return c.Env == EnvProduction }

// Load reads configuration from environment variables and validates it.
func Load() (*Config, error) {
	cfg := &Config{
		Env:             getEnv("APP_ENV", EnvDevelopment),
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     firstNonEmpty(os.Getenv("DATABASE_URL_POOLED"), os.Getenv("DATABASE_URL")),
		JWTAccessSecret: os.Getenv("JWT_ACCESS_SECRET"),
		SentryDSN:       os.Getenv("SENTRY_DSN"),
		Release:         firstNonEmpty(os.Getenv("SENTRY_RELEASE"), os.Getenv("RENDER_GIT_COMMIT")),
		CORSOrigins:     splitList(os.Getenv("CORS_ALLOWED_ORIGINS")),

		ContactsMasterKey: strings.TrimSpace(os.Getenv("CONTACTS_MASTER_KEY")),
		OTPProvider:       strings.ToLower(getEnv("OTP_PROVIDER", "console")),

		S3Endpoint:        strings.TrimSpace(os.Getenv("AWS_ENDPOINT_URL_S3")),
		S3Region:          strings.TrimSpace(os.Getenv("AWS_REGION")),
		S3Bucket:          strings.TrimSpace(os.Getenv("S3_BUCKET")),
		S3AccessKeyID:     strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")),
		S3SecretAccessKey: strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY")),

		FirebaseCredentialsB64: strings.TrimSpace(os.Getenv("FIREBASE_CREDENTIALS_B64")),
	}

	var errs []error
	var err error

	if cfg.AccessTokenTTL, err = getDuration("ACCESS_TOKEN_TTL", 15*time.Minute); err != nil {
		errs = append(errs, err)
	}
	if cfg.RefreshTokenTTL, err = getDuration("REFRESH_TOKEN_TTL", 30*24*time.Hour); err != nil {
		errs = append(errs, err)
	}
	if cfg.ProxyHops, err = getInt("TRUSTED_PROXY_HOPS", 0); err != nil {
		errs = append(errs, err)
	}
	if cfg.RateLimit, err = getInt("RATE_LIMIT_PER_MINUTE", 120); err != nil {
		errs = append(errs, err)
	}
	if cfg.LoginRateLimt, err = getInt("LOGIN_RATE_LIMIT_PER_MINUTE", 10); err != nil {
		errs = append(errs, err)
	}

	if cfg.ContactsRetentionDays, err = getInt("CONTACTS_RETENTION_DAYS", 30); err != nil {
		errs = append(errs, err)
	}
	if cfg.RequireContactsToBook, err = getBool("REQUIRE_CONTACTS_FOR_BOOKING", true); err != nil {
		errs = append(errs, err)
	}
	if cfg.OTPTTL, err = getDuration("OTP_TTL", 5*time.Minute); err != nil {
		errs = append(errs, err)
	}

	switch cfg.Env {
	case EnvDevelopment, EnvProduction, EnvTest:
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be development, production or test, got %q", cfg.Env))
	}
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL_POOLED or DATABASE_URL is required"))
	}
	if len(cfg.JWTAccessSecret) < minJWTSecretLen {
		errs = append(errs, fmt.Errorf("JWT_ACCESS_SECRET must be at least %d characters", minJWTSecretLen))
	}
	if cfg.AccessTokenTTL <= 0 || cfg.AccessTokenTTL > time.Hour {
		errs = append(errs, errors.New("ACCESS_TOKEN_TTL must be between 1s and 1h"))
	}
	if cfg.RefreshTokenTTL < cfg.AccessTokenTTL {
		errs = append(errs, errors.New("REFRESH_TOKEN_TTL must be longer than ACCESS_TOKEN_TTL"))
	}
	if cfg.ProxyHops < 0 || cfg.ProxyHops > 5 {
		errs = append(errs, errors.New("TRUSTED_PROXY_HOPS must be between 0 and 5"))
	}
	if cfg.RateLimit < 1 || cfg.LoginRateLimt < 1 {
		errs = append(errs, errors.New("rate limits must be at least 1"))
	}
	if cfg.ContactsMasterKey == "" {
		errs = append(errs, errors.New("CONTACTS_MASTER_KEY is required (generate with: openssl rand -base64 32)"))
	}
	if cfg.ContactsRetentionDays < 1 || cfg.ContactsRetentionDays > 365 {
		errs = append(errs, errors.New("CONTACTS_RETENTION_DAYS must be between 1 and 365"))
	}
	if cfg.OTPTTL < time.Minute || cfg.OTPTTL > 15*time.Minute {
		errs = append(errs, errors.New("OTP_TTL must be between 1m and 15m"))
	}
	if cfg.OTPProvider != "console" {
		errs = append(errs, fmt.Errorf("OTP_PROVIDER %q is not supported yet (only \"console\")", cfg.OTPProvider))
	}
	if cfg.IsProduction() && cfg.OTPProvider == "console" {
		errs = append(errs, errors.New("OTP_PROVIDER=console is not allowed in production because it writes login codes to the logs; implement an SMS provider first"))
	}
	for _, o := range cfg.CORSOrigins {
		if o == "*" && cfg.Env == EnvProduction {
			errs = append(errs, errors.New("CORS_ALLOWED_ORIGINS must not contain * in production"))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) (int, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return n, nil
}

func getBool(key string, def bool) (bool, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false: %w", key, err)
	}
	return b, nil
}

func getDuration(key string, def time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration like 15m: %w", key, err)
	}
	return d, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
