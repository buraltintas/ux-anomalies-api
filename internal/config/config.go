package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv            string
	AppAddr           string
	LogLevel          slog.Level
	DatabaseURL       string
	MigrationsDBURL   string
	BFFSecret         string
	JWTIssuer         string
	JWTAudience       string
	JWTSigningKey     string
	JWTAccessTTL      time.Duration
	MaxBodyBytes      int64
	MaxMultipartBytes int64
	MaxImageBytes     int64
	RateLimitBackend  string
	StorageBackend    string
	StorageLocalDir   string
	GCPProjectID      string
	GCSBucket         string
	GCSUploadPrefix   string
	GCSSignedURLTTL   time.Duration
	CORSAllowedOrigin string
}

func Load() (Config, error) {
	// Local convenience only: load .env if present and process env is not already fully set.
	if _, err := os.Stat(".env"); err == nil {
		_ = godotenv.Load(".env")
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	migrationsURL := strings.TrimSpace(os.Getenv("MIGRATIONS_DATABASE_URL"))
	if migrationsURL == "" {
		migrationsURL = databaseURL
	}

	cfg := Config{
		AppEnv:            getDefault("APP_ENV", "development"),
		AppAddr:           getDefault("APP_ADDR", ":8080"),
		LogLevel:          parseLogLevel(getDefault("LOG_LEVEL", "debug")),
		DatabaseURL:       databaseURL,
		MigrationsDBURL:   migrationsURL,
		BFFSecret:         strings.TrimSpace(os.Getenv("BFF_SECRET")),
		JWTIssuer:         getDefault("JWT_ISSUER", "uxanomalies-api"),
		JWTAudience:       getDefault("JWT_AUDIENCE", "uxanomalies-bff"),
		JWTSigningKey:     strings.TrimSpace(os.Getenv("JWT_SIGNING_KEY")),
		JWTAccessTTL:      parseDurationDefault("JWT_ACCESS_TTL", 24*time.Hour),
		MaxBodyBytes:      parseInt64Default("MAX_BODY_BYTES", 1<<20),
		MaxMultipartBytes: parseInt64Default("MAX_MULTIPART_BYTES", 20<<20),
		MaxImageBytes:     parseInt64Default("MAX_IMAGE_BYTES", 5<<20),
		RateLimitBackend:  getDefault("RATE_LIMIT_BACKEND", "memory"),
		StorageBackend:    getDefault("STORAGE_BACKEND", "gcs"),
		StorageLocalDir:   getDefault("STORAGE_LOCAL_DIR", "./var/storage"),
		GCPProjectID:      getDefault("GCP_PROJECT_ID", ""),
		GCSBucket:         getDefault("GCS_BUCKET", ""),
		GCSUploadPrefix:   getDefault("GCS_UPLOAD_PREFIX", "anomalies"),
		GCSSignedURLTTL:   parseDurationDefault("GCS_SIGNED_URL_TTL", 24*time.Hour),
		CORSAllowedOrigin: getDefault("CORS_ALLOWED_ORIGINS", "http://localhost:3000"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.AppEnv != "development" && cfg.AppEnv != "production" {
		return Config{}, fmt.Errorf("APP_ENV must be one of: development, production")
	}
	if cfg.MigrationsDBURL == "" {
		return Config{}, fmt.Errorf("MIGRATIONS_DATABASE_URL fallback resolved empty; set DATABASE_URL or MIGRATIONS_DATABASE_URL")
	}
	if cfg.BFFSecret == "" {
		return Config{}, fmt.Errorf("BFF_SECRET is required")
	}
	if cfg.JWTSigningKey == "" {
		return Config{}, fmt.Errorf("JWT_SIGNING_KEY is required")
	}
	if cfg.StorageBackend == "gcs" {
		if cfg.GCSBucket == "" {
			return Config{}, fmt.Errorf("GCS_BUCKET is required when STORAGE_BACKEND=gcs")
		}
		if cfg.GCPProjectID == "" {
			return Config{}, fmt.Errorf("GCP_PROJECT_ID is required when STORAGE_BACKEND=gcs")
		}
		if strings.TrimSpace(cfg.GCSUploadPrefix) == "" {
			return Config{}, fmt.Errorf("GCS_UPLOAD_PREFIX is required when STORAGE_BACKEND=gcs")
		}
		if cfg.GCSSignedURLTTL <= 0 {
			return Config{}, fmt.Errorf("GCS_SIGNED_URL_TTL must be > 0")
		}
	} else if cfg.StorageBackend != "local" {
		return Config{}, fmt.Errorf("STORAGE_BACKEND must be one of: gcs, local")
	}
	return cfg, nil
}

func getDefault(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func parseLogLevel(v string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func parseInt64Default(key string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	p, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return p
}

func parseDurationDefault(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
