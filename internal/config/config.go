package config

import (
	"fmt"
	"os"
	"time"
)

type Ingest struct {
	DatabaseURL string
	MediaDir    string
	GRPCAddr    string
}

type Playback struct {
	DatabaseURL   string
	MediaDir      string
	HTTPAddr      string
	RedisAddr     string
	SigningSecret string
	SignatureTTL  time.Duration
}

func LoadIngest() (Ingest, error) {
	c := Ingest{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		MediaDir:    envOr("MEDIA_DIR", "/var/streaming"),
		GRPCAddr:    envOr("GRPC_ADDR", ":9090"),
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL required")
	}
	return c, nil
}

func LoadPlayback() (Playback, error) {
	ttl, err := time.ParseDuration(envOr("SIGNATURE_TTL", "1h"))
	if err != nil {
		return Playback{}, fmt.Errorf("SIGNATURE_TTL: %w", err)
	}
	c := Playback{
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		MediaDir:      envOr("MEDIA_DIR", "/var/streaming"),
		HTTPAddr:      envOr("HTTP_ADDR", ":9005"),
		RedisAddr:     os.Getenv("REDIS_ADDR"),
		SigningSecret: envOr("SIGNING_SECRET", "dev-secret-change-me"),
		SignatureTTL:  ttl,
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL required")
	}
	return c, nil
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
