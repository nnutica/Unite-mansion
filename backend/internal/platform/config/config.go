// Package config loads application configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppEnv             string // local | uat
	HTTPPort           string
	DatabaseURL        string
	S3                 S3Config
	AuthSecret         string
	CORSAllowedOrigins []string
}

// S3Config points at any S3-compatible store: RustFS in dev, Cloudflare R2 in UAT.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
}

// Load reads the environment and validates required values.
func Load() (Config, error) {
	var errs []error
	forcePathStyle, err := strconv.ParseBool(getenv("S3_FORCE_PATH_STYLE", "false"))
	if err != nil {
		errs = append(errs, fmt.Errorf("S3_FORCE_PATH_STYLE must be true or false: %w", err))
	}

	cfg := Config{
		AppEnv:      getenv("APP_ENV", "local"),
		HTTPPort:    getenv("HTTP_PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		S3: S3Config{
			Endpoint:        os.Getenv("S3_ENDPOINT"),
			Region:          getenv("S3_REGION", "auto"),
			Bucket:          os.Getenv("S3_BUCKET"),
			AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
			ForcePathStyle:  forcePathStyle,
		},
		AuthSecret:         os.Getenv("AUTH_SECRET"),
		CORSAllowedOrigins: splitList(getenv("CORS_ALLOWED_ORIGINS", "http://localhost:5173")),
	}
	return cfg, errors.Join(append(errs, cfg.validate())...)
}

func (c Config) validate() error {
	var errs []error
	if c.AppEnv != "local" && c.AppEnv != "uat" {
		errs = append(errs, fmt.Errorf("APP_ENV must be local or uat, got %q", c.AppEnv))
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.AuthSecret == "" {
		errs = append(errs, errors.New("AUTH_SECRET is required"))
	}
	required := []struct{ name, value string }{
		{"S3_ENDPOINT", c.S3.Endpoint},
		{"S3_BUCKET", c.S3.Bucket},
		{"S3_ACCESS_KEY_ID", c.S3.AccessKeyID},
		{"S3_SECRET_ACCESS_KEY", c.S3.SecretAccessKey},
	}
	for _, r := range required {
		if r.value == "" {
			errs = append(errs, fmt.Errorf("%s is required", r.name))
		}
	}
	return errors.Join(errs...)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
