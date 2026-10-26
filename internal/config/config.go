package config

import (
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
	DemoMode             bool   // DEMO_MODE=true: интерфейс на данных в памяти, без Postgres и Redis
	DocumentsDir         string // каталог (Docker-том) с файлами заявок
}

func Load() (Config, error) {
	httpAddr := os.Getenv("HTTP_ADDR")
	if httpAddr == "" {
		if port := os.Getenv("PORT"); port != "" {
			httpAddr = ":" + port
		} else {
			httpAddr = ":8080"
		}
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("POSTGRES_URL")
	}

	webDir := os.Getenv("WEB_DIR")
	if webDir == "" {
		for _, c := range []string{"web", "./web", "../web", "../../web"} {
			if fi, err := os.Stat(c); err == nil && fi.IsDir() {
				webDir = c
				break
			}
		}
		if webDir == "" {
			webDir = "web"
		}
	}

	cfg := Config{
		HTTPAddr:     httpAddr,
		WebDir:       webDir,
		DatabaseURL:  dbURL,
		RedisAddr:    getenv("REDIS_ADDR", "localhost:6379"),
		OTPStubCode:  getenv("OTP_STUB_CODE", "000000"),
		AssistantURL: getenv("ASSISTANT_URL", "http://assistant:8081"),
		DocumentsDir: getenv("DOCUMENTS_DIR", "data/documents"),
		DemoMode:     os.Getenv("DEMO_MODE") == "true",
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
