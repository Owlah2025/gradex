// Package config loads Gradex's video-service configuration from environment
// variables. Values mirror .env.example; see that file for the authoritative list.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL string
	RedisAddr   string

	S3Endpoint     string
	S3AccessKey    string
	S3SecretKey    string
	S3Bucket       string
	S3Region       string
	S3UsePathStyle bool

	UploadURLExpiryMinutes  int
	PlaybackURLExpiryMinutes int
	MaxUploadSizeBytes      int64

	AuthFakeMode bool

	FFmpegBinaryPath  string
	FFprobeBinaryPath string

	// PlaybackTokenSecret signs the short-lived manifest tokens embedded in
	// playback URLs (see internal/video/token.go). The HLS player fetches
	// master/child playlists from Gradex's own API using this token instead
	// of a per-object presigned URL, because plain S3 presigned URLs sign
	// exactly one object each and can't cover an HLS manifest's child
	// playlists/segments the way a real CDN's wildcard/cookie signing would
	// (see docs/superpowers/specs/2026-07-17-video-streaming-design.md §5).
	PlaybackTokenSecret string

	Port string
}

func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:              os.Getenv("DATABASE_URL"),
		RedisAddr:                os.Getenv("REDIS_ADDR"),
		S3Endpoint:               os.Getenv("S3_ENDPOINT"),
		S3AccessKey:              os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:              os.Getenv("S3_SECRET_KEY"),
		S3Bucket:                 os.Getenv("S3_BUCKET"),
		S3Region:                 envOr("S3_REGION", "us-east-1"),
		FFmpegBinaryPath:         envOr("FFMPEG_BINARY_PATH", "ffmpeg"),
		FFprobeBinaryPath:        envOr("FFPROBE_BINARY_PATH", "ffprobe"),
		Port:                     envOr("PORT", "8080"),
		PlaybackTokenSecret:      os.Getenv("PLAYBACK_TOKEN_SECRET"),
	}

	var err error
	if cfg.S3UsePathStyle, err = boolEnv("S3_USE_PATH_STYLE", true); err != nil {
		return nil, err
	}
	if cfg.AuthFakeMode, err = boolEnv("AUTH_FAKE_MODE", true); err != nil {
		return nil, err
	}
	if cfg.UploadURLExpiryMinutes, err = intEnv("UPLOAD_URL_EXPIRY_MINUTES", 15); err != nil {
		return nil, err
	}
	if cfg.PlaybackURLExpiryMinutes, err = intEnv("PLAYBACK_URL_EXPIRY_MINUTES", 5); err != nil {
		return nil, err
	}
	if cfg.MaxUploadSizeBytes, err = int64Env("MAX_UPLOAD_SIZE_BYTES", 5*1024*1024*1024); err != nil {
		return nil, err
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.RedisAddr == "" {
		return nil, fmt.Errorf("REDIS_ADDR is required")
	}
	if cfg.PlaybackTokenSecret == "" {
		return nil, fmt.Errorf("PLAYBACK_TOKEN_SECRET is required")
	}
	if cfg.S3Endpoint == "" || cfg.S3Bucket == "" {
		return nil, fmt.Errorf("S3_ENDPOINT and S3_BUCKET are required")
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func boolEnv(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid bool for %s: %w", key, err)
	}
	return b, nil
}

func intEnv(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid int for %s: %w", key, err)
	}
	return i, nil
}

func int64Env(key string, fallback int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid int64 for %s: %w", key, err)
	}
	return i, nil
}
