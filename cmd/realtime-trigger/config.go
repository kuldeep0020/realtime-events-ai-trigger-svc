package main

import (
	"fmt"
	"os"
	"strings"
)

// runtimeConfig captures every env var read by `realtime-trigger serve`. We
// keep this struct as the single source of truth — main.go does not read
// os.Getenv directly. Validation happens once at startup; downstream code
// receives a fully-populated struct with sensible defaults applied.
type runtimeConfig struct {
	// Postgres
	PostgresDSN string

	// Pulsar
	PulsarURL          string
	PulsarTopic        string
	PulsarSubscription string
	PulsarToken        string

	// Pulsar TLS — required for self-signed local broker; optional for
	// public-CA brokers like StreamNative production.
	PulsarTLSTrustCerts       string
	PulsarTLSValidateHostname bool
	PulsarTLSAllowInsecure    bool

	// Tenant filter & write keys
	AllowedWriteKeys []string

	// Browser CORS: comma-separated full origins (e.g. https://app.example.com).
	// Empty → only localhost dev origins are allowed by the API middleware.
	CorsAllowedOrigins []string

	// Demo wiring
	IngestionURL   string
	DemoFireTarget string // "pulsar" | "http"

	// Modes (mock | live | canned)
	LLMMode        string
	KapaMode       string
	ActivationMode string

	// Live activation (only when ActivationMode=live)
	ActivationBaseURL string
	ActivationDestID  string
	ActivationSAT     string

	// Slack dispatch
	SlackWebhookURL string

	// Logging
	LogLevel string

	// HTTP server
	HTTPAddr string

	// Migrations dir for the embedded migrate command
	MigrationsDir string
}

// loadRuntimeConfig reads env, applies defaults, and returns a validated
// runtimeConfig. The error is non-nil iff a required field is missing.
func loadRuntimeConfig() (runtimeConfig, error) {
	cfg := runtimeConfig{
		PostgresDSN:        envOrDefault("POSTGRES_DSN", ""),
		PulsarURL:          envOrDefault("PULSAR_URL", ""),
		PulsarTopic:        envOrDefault("PULSAR_TOPIC", ""),
		PulsarSubscription: envOrDefault("PULSAR_SUBSCRIPTION", "realtime-ai-trigger-svc-v1"),
		PulsarToken:        os.Getenv("PULSAR_JWT_TOKEN"),

		PulsarTLSTrustCerts:       os.Getenv("PULSAR_TLS_TRUST_CERTS"),
		PulsarTLSValidateHostname: envBoolDefault("PULSAR_TLS_VALIDATE_HOSTNAME", true),
		PulsarTLSAllowInsecure:    envBoolDefault("PULSAR_TLS_ALLOW_INSECURE", false),
		IngestionURL:       envOrDefault("INGESTION_URL", ""),
		DemoFireTarget:     envOrDefault("DEMO_FIRE_TARGET", "pulsar"),
		LLMMode:            envOrDefault("LLM_MODE", "canned"),
		KapaMode:           envOrDefault("KAPA_MODE", "canned"),
		ActivationMode:     envOrDefault("ACTIVATION_MODE", "mock"),
		ActivationBaseURL:  os.Getenv("ACTIVATION_BASE_URL"),
		ActivationDestID:   os.Getenv("ACTIVATION_DEST_ID"),
		ActivationSAT:      os.Getenv("ACTIVATION_SAT"),
		SlackWebhookURL:    os.Getenv("SLACK_WEBHOOK_URL"),
		LogLevel:           envOrDefault("LOG_LEVEL", "info"),
		HTTPAddr:           envOrDefault("HTTP_ADDR", ":8080"),
		MigrationsDir:      envOrDefault("MIGRATIONS_DIR", defaultMigrationsDir()),
	}

	allowed := strings.TrimSpace(os.Getenv("ALLOWED_WRITE_KEYS"))
	if allowed != "" {
		for _, k := range strings.Split(allowed, ",") {
			if k = strings.TrimSpace(k); k != "" {
				cfg.AllowedWriteKeys = append(cfg.AllowedWriteKeys, k)
			}
		}
	}

	corsOrigins := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if corsOrigins != "" {
		for _, o := range strings.Split(corsOrigins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				cfg.CorsAllowedOrigins = append(cfg.CorsAllowedOrigins, o)
			}
		}
	}

	// Required-field validation. Each missing field returns a clear error.
	var missing []string
	if cfg.PostgresDSN == "" {
		missing = append(missing, "POSTGRES_DSN")
	}
	if cfg.PulsarURL == "" {
		missing = append(missing, "PULSAR_URL")
	}
	if cfg.PulsarTopic == "" {
		missing = append(missing, "PULSAR_TOPIC")
	}
	if len(missing) > 0 {
		return cfg, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}

	// Mode-specific validation.
	if cfg.ActivationMode == "live" {
		if cfg.ActivationBaseURL == "" || cfg.ActivationSAT == "" || cfg.ActivationDestID == "" {
			return cfg, fmt.Errorf("ACTIVATION_MODE=live requires ACTIVATION_BASE_URL + ACTIVATION_SAT + ACTIVATION_DEST_ID")
		}
	}

	switch cfg.DemoFireTarget {
	case "pulsar", "http":
		// valid
	default:
		return cfg, fmt.Errorf("DEMO_FIRE_TARGET=%q is invalid; must be pulsar or http", cfg.DemoFireTarget)
	}

	return cfg, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
