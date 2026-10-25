package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

type Config struct {
	HTTPAddr             string
	WebDir               string
	DatabaseURL          string
	RedisAddr            string
	OTPStubCode          string
	TrustRefreshInterval time.Duration
	WorkerTimeout        time.Duration
	AssistantURL         string
	DocumentsDir         string // каталог (Docker-том) с файлами заявок
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:             getenv("HTTP_ADDR", ":8080"),
		WebDir:               getenv("WEB_DIR", "web"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		RedisAddr:            getenv("REDIS_ADDR", "localhost:6379"),
		OTPStubCode:          getenv("OTP_STUB_CODE", "000000"),
		AssistantURL:         getenv("ASSISTANT_URL", "http://assistant:8081"),
		DocumentsDir:         getenv("DOCUMENTS_DIR", "data/documents"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	var err error
	if cfg.TrustRefreshInterval, err = duration("TRUST_REFRESH_INTERVAL", 12*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.WorkerTimeout, err = duration("WORKER_TIMEOUT", 8*time.Second); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s: expected a positive duration like 12h, got %q", key, v)
	}
	return d, nil
}
