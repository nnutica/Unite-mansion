// Package config loads application configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	AppEnv             string // local | uat
	HTTPPort           string
	DatabaseURL        string
	StorageDriver      string // r2 | local
	LocalStorageDir    string
	R2                 R2Config
	AuthSecret         string
	CORSAllowedOrigins []string
}

type R2Config struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
}

// Load reads the environment and validates required values.
func Load() (Config, error) {
	cfg := Config{
		AppEnv:          getenv("APP_ENV", "local"),
		HTTPPort:        getenv("HTTP_PORT", "8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		StorageDriver:   getenv("STORAGE_DRIVER", "local"),
		LocalStorageDir: getenv("LOCAL_STORAGE_DIR", "./.storage"),
		R2: R2Config{
			AccountID:       os.Getenv("R2_ACCOUNT_ID"),
			AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
			Bucket:          os.Getenv("R2_BUCKET"),
		},
		AuthSecret:         os.Getenv("AUTH_SECRET"),
		CORSAllowedOrigins: splitList(getenv("CORS_ALLOWED_ORIGINS", "http://localhost:5173")),
	}
	return cfg, cfg.validate()
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
	switch c.StorageDriver {
	case "local":
	case "r2":
		if c.R2.AccountID == "" || c.R2.AccessKeyID == "" || c.R2.SecretAccessKey == "" || c.R2.Bucket == "" {
			errs = append(errs, errors.New("R2_* variables are required when STORAGE_DRIVER=r2"))
		}
	default:
		errs = append(errs, fmt.Errorf("STORAGE_DRIVER must be r2 or local, got %q", c.StorageDriver))
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
