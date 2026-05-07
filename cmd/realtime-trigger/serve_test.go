package main

import (
	"os"
	"strings"
	"testing"
)

// TestLoadRuntimeConfig_RequiresMustHaves verifies the loader surfaces
// missing-required errors clearly rather than silently defaulting.
func TestLoadRuntimeConfig_RequiresMustHaves(t *testing.T) {
	// Save original env so we can restore it.
	saved := map[string]string{
		"POSTGRES_DSN":        os.Getenv("POSTGRES_DSN"),
		"PULSAR_URL":          os.Getenv("PULSAR_URL"),
		"PULSAR_TOPIC":        os.Getenv("PULSAR_TOPIC"),
		"ALLOWED_WRITE_KEYS":  os.Getenv("ALLOWED_WRITE_KEYS"),
		"INGESTION_URL":       os.Getenv("INGESTION_URL"),
		"ACTIVATION_MODE":     os.Getenv("ACTIVATION_MODE"),
		"ACTIVATION_BASE_URL": os.Getenv("ACTIVATION_BASE_URL"),
		"ACTIVATION_DEST_ID":  os.Getenv("ACTIVATION_DEST_ID"),
		"ACTIVATION_SAT":      os.Getenv("ACTIVATION_SAT"),
	}
	t.Cleanup(func() {
		for k, v := range saved {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	})

	for k := range saved {
		_ = os.Unsetenv(k)
	}

	_, err := loadRuntimeConfig()
	if err == nil {
		t.Fatal("expected error for missing required env vars")
	}
	for _, mustMention := range []string{"POSTGRES_DSN", "PULSAR_URL", "PULSAR_TOPIC"} {
		if !strings.Contains(err.Error(), mustMention) {
			t.Errorf("expected error to mention %s; got %v", mustMention, err)
		}
	}
}

// TestLoadRuntimeConfig_DefaultsApplied verifies non-required fields fall
// back to their documented defaults when env is unset.
func TestLoadRuntimeConfig_DefaultsApplied(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://localhost/x")
	t.Setenv("PULSAR_URL", "pulsar://localhost:6650")
	t.Setenv("PULSAR_TOPIC", "topic")
	// All optional ones unset.
	_ = os.Unsetenv("LLM_MODE")
	_ = os.Unsetenv("KAPA_MODE")
	_ = os.Unsetenv("ACTIVATION_MODE")
	_ = os.Unsetenv("LOG_LEVEL")
	_ = os.Unsetenv("HTTP_ADDR")

	cfg, err := loadRuntimeConfig()
	if err != nil {
		t.Fatalf("loadRuntimeConfig: %v", err)
	}
	if cfg.LLMMode != "canned" {
		t.Errorf("LLMMode=%q (want canned)", cfg.LLMMode)
	}
	if cfg.KapaMode != "canned" {
		t.Errorf("KapaMode=%q (want canned)", cfg.KapaMode)
	}
	if cfg.ActivationMode != "mock" {
		t.Errorf("ActivationMode=%q (want mock)", cfg.ActivationMode)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr=%q (want :8080)", cfg.HTTPAddr)
	}
	if cfg.PulsarSubscription != "realtime-ai-trigger-svc-v1" {
		t.Errorf("PulsarSubscription=%q", cfg.PulsarSubscription)
	}
}

// TestLoadRuntimeConfig_LiveActivationRequiresAllFields verifies the
// live-mode validation surface.
func TestLoadRuntimeConfig_LiveActivationRequiresAllFields(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://localhost/x")
	t.Setenv("PULSAR_URL", "pulsar://localhost:6650")
	t.Setenv("PULSAR_TOPIC", "topic")
	t.Setenv("ACTIVATION_MODE", "live")
	// Intentionally omit BASE_URL / SAT / DEST_ID.

	_, err := loadRuntimeConfig()
	if err == nil {
		t.Fatal("expected error for live activation without required fields")
	}
	if !strings.Contains(err.Error(), "ACTIVATION_MODE=live") {
		t.Errorf("expected error to mention live mode; got %v", err)
	}
}

// TestLoadRuntimeConfig_AllowedWriteKeysSplit verifies the comma-splitter.
func TestLoadRuntimeConfig_AllowedWriteKeysSplit(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://localhost/x")
	t.Setenv("PULSAR_URL", "pulsar://localhost:6650")
	t.Setenv("PULSAR_TOPIC", "topic")
	t.Setenv("ALLOWED_WRITE_KEYS", "key-1, key-2 ,key-3")

	cfg, err := loadRuntimeConfig()
	if err != nil {
		t.Fatalf("loadRuntimeConfig: %v", err)
	}
	if got, want := len(cfg.AllowedWriteKeys), 3; got != want {
		t.Errorf("AllowedWriteKeys count=%d, want %d", got, want)
	}
	for i, expected := range []string{"key-1", "key-2", "key-3"} {
		if cfg.AllowedWriteKeys[i] != expected {
			t.Errorf("AllowedWriteKeys[%d]=%q, want %q", i, cfg.AllowedWriteKeys[i], expected)
		}
	}
}

// TestPickDefaultWriteKey_KnownPersona checks the canonical write-key
// resolution.
func TestPickDefaultWriteKey_KnownPersona(t *testing.T) {
	if k := pickDefaultWriteKey("realestate"); k == "" {
		t.Error("expected non-empty key for realestate persona")
	}
	if k := pickDefaultWriteKey("rs-self"); k == "" {
		t.Error("expected non-empty key for rs-self persona")
	}
	if k := pickDefaultWriteKey("unknown"); k != "" {
		t.Errorf("expected empty for unknown, got %q", k)
	}
}
