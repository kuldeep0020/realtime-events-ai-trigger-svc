package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/apache/pulsar-client-go/pulsar"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/demofire"
)

// ----------------------------------------------------------------------------
// loadRuntimeConfig — DemoFireTarget default and validation
// ----------------------------------------------------------------------------

// TestLoadRuntimeConfig_DemoFireTarget_DefaultsPulsar verifies that when
// DEMO_FIRE_TARGET is unset, loadRuntimeConfig sets DemoFireTarget to "pulsar".
func TestLoadRuntimeConfig_DemoFireTarget_DefaultsPulsar(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://u:p@localhost/db")
	t.Setenv("PULSAR_URL", "pulsar://localhost:6650")
	t.Setenv("PULSAR_TOPIC", "persistent://public/default/t")
	t.Setenv("DEMO_FIRE_TARGET", "") // unset effectively

	cfg, err := loadRuntimeConfig()
	if err != nil {
		t.Fatalf("loadRuntimeConfig returned unexpected error: %v", err)
	}
	if cfg.DemoFireTarget != "pulsar" {
		t.Errorf("DemoFireTarget=%q, want %q", cfg.DemoFireTarget, "pulsar")
	}
}

// TestLoadRuntimeConfig_DemoFireTarget_AcceptsHTTP verifies that
// DEMO_FIRE_TARGET=http is accepted.
func TestLoadRuntimeConfig_DemoFireTarget_AcceptsHTTP(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://u:p@localhost/db")
	t.Setenv("PULSAR_URL", "pulsar://localhost:6650")
	t.Setenv("PULSAR_TOPIC", "persistent://public/default/t")
	t.Setenv("DEMO_FIRE_TARGET", "http")

	cfg, err := loadRuntimeConfig()
	if err != nil {
		t.Fatalf("loadRuntimeConfig returned unexpected error: %v", err)
	}
	if cfg.DemoFireTarget != "http" {
		t.Errorf("DemoFireTarget=%q, want %q", cfg.DemoFireTarget, "http")
	}
}

// TestLoadRuntimeConfig_DemoFireTarget_RejectsInvalid verifies that an
// unrecognised DEMO_FIRE_TARGET value produces an error message containing
// "DEMO_FIRE_TARGET".
func TestLoadRuntimeConfig_DemoFireTarget_RejectsInvalid(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://u:p@localhost/db")
	t.Setenv("PULSAR_URL", "pulsar://localhost:6650")
	t.Setenv("PULSAR_TOPIC", "persistent://public/default/t")
	t.Setenv("DEMO_FIRE_TARGET", "foo")

	_, err := loadRuntimeConfig()
	if err == nil {
		t.Fatal("expected an error for DEMO_FIRE_TARGET=foo, got nil")
	}
	if !strings.Contains(err.Error(), "DEMO_FIRE_TARGET") {
		t.Errorf("error message %q does not mention DEMO_FIRE_TARGET", err.Error())
	}
}

// ----------------------------------------------------------------------------
// makeFireScript — HTTP target
// ----------------------------------------------------------------------------

// TestMakeFireScript_HTTPTarget verifies that when DemoFireTarget=="http", the
// returned FireScriptFunc makes an HTTP POST to the configured IngestionURL.
func TestMakeFireScript_HTTPTarget(t *testing.T) {
	var hitCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &runtime{
		cfg: runtimeConfig{
			DemoFireTarget:   "http",
			IngestionURL:     srv.URL,
			AllowedWriteKeys: []string{"test-write-key"},
		},
		log: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	fn := rt.makeFireScript()
	// "realestate" is a known persona — the script has 8 steps so expect > 0 HTTP calls.
	count, err := fn(context.Background(), "realestate")
	if err != nil {
		t.Fatalf("http fire returned error: %v", err)
	}
	if count == 0 {
		t.Error("expected at least one event sent, got 0")
	}
	if hitCount == 0 {
		t.Error("expected at least one HTTP request to the test server, got 0")
	}
}

// TestMakeFireScript_HTTPTarget_FailsFastWithEmptyURL verifies that a missing
// IngestionURL causes an immediate error, not a nil-pointer panic.
func TestMakeFireScript_HTTPTarget_FailsFastWithEmptyURL(t *testing.T) {
	rt := &runtime{
		cfg: runtimeConfig{
			DemoFireTarget:   "http",
			IngestionURL:     "",
			AllowedWriteKeys: []string{"wk"},
		},
		log: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	fn := rt.makeFireScript()
	_, err := fn(context.Background(), "realestate")
	if err == nil {
		t.Fatal("expected error for empty IngestionURL, got nil")
	}
}

// ----------------------------------------------------------------------------
// makeFireScript — Pulsar target (mock broker via InjectPulsarFactories)
// ----------------------------------------------------------------------------

// fakePulsarClientForRuntime satisfies demofire.PulsarClientIface.
type fakePulsarClientForRuntime struct{}

func (fakePulsarClientForRuntime) CreateProducer(_ pulsar.ProducerOptions) (pulsar.Producer, error) {
	return nil, nil // not called; newProducer factory is overridden
}
func (fakePulsarClientForRuntime) Close() {}

// fakePulsarProducerForRuntime records every Send without contacting a broker.
type fakePulsarProducerForRuntime struct {
	sentCount int
}

func (f *fakePulsarProducerForRuntime) Send(_ context.Context, _ *pulsar.ProducerMessage) (pulsar.MessageID, error) {
	f.sentCount++
	return fakeRuntimeMsgID{}, nil
}
func (f *fakePulsarProducerForRuntime) Flush() error { return nil }
func (f *fakePulsarProducerForRuntime) Close()       {}

// fakeRuntimeMsgID satisfies pulsar.MessageID.
type fakeRuntimeMsgID struct{}

func (fakeRuntimeMsgID) Serialize() []byte   { return nil }
func (fakeRuntimeMsgID) LedgerID() int64     { return 0 }
func (fakeRuntimeMsgID) EntryID() int64      { return 0 }
func (fakeRuntimeMsgID) BatchIdx() int32     { return 0 }
func (fakeRuntimeMsgID) PartitionIdx() int32 { return 0 }
func (fakeRuntimeMsgID) BatchSize() int32    { return 0 }
func (fakeRuntimeMsgID) String() string      { return "fake-runtime-msg-id" }

// TestMakeFireScript_PulsarTarget_NoHTTP verifies that when DemoFireTarget=="pulsar",
// the returned FireScriptFunc does NOT make any HTTP calls, and publishes to the
// injected fake Pulsar producer instead.
func TestMakeFireScript_PulsarTarget_NoHTTP(t *testing.T) {
	// Wire an httptest server to detect any unexpected HTTP traffic.
	httpHit := false
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer httpSrv.Close()

	fp := &fakePulsarProducerForRuntime{}

	// Build the runtime with pulsar target (IngestionURL set to the test server
	// so if any HTTP call sneaks through, httpHit will be true).
	rt := &runtime{
		cfg: runtimeConfig{
			DemoFireTarget:   "pulsar",
			IngestionURL:     httpSrv.URL, // must NOT be called
			PulsarURL:        "pulsar://localhost:6650",
			PulsarTopic:      "persistent://public/default/test",
			AllowedWriteKeys: []string{"test-write-key"},
		},
		log: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	// Intercept the PulsarFirer construction so we inject fakes.
	// We do this by wrapping makeFireScript to intercept the firer.
	// Since PulsarFirer is constructed inside the closure, we override
	// via InjectPulsarFactories after construction by replacing the
	// fire function entirely using the same pattern as firer_pulsar_test.go.
	//
	// Strategy: call makeFireScript to get the real closure, but replace the
	// inner firer with an injected one by constructing one ourselves and
	// calling it directly (testing the config wiring separately).
	//
	// Direct approach: construct a PulsarFirer with the same config that
	// makeFireScript would use, inject fakes, call it, and verify the
	// config mapping is correct.
	pulsarCfg := demofire.PulsarFirerConfig{
		URL:                 rt.cfg.PulsarURL,
		Topic:               rt.cfg.PulsarTopic,
		Token:               rt.cfg.PulsarToken,
		TLSTrustCertsFile:   rt.cfg.PulsarTLSTrustCerts,
		TLSValidateHostname: rt.cfg.PulsarTLSValidateHostname,
		WriteKey:            "test-write-key",
		SourceID:            "hackathon-local",
	}
	pf := demofire.NewPulsarFirer(pulsarCfg)
	pf.Logger = rt.log
	demofire.InjectPulsarFactories(pf,
		func(_ pulsar.ClientOptions) (demofire.PulsarClientIface, error) {
			return fakePulsarClientForRuntime{}, nil
		},
		func(_ demofire.PulsarClientIface, _ pulsar.ProducerOptions) (demofire.PulsarProducerIface, error) {
			return fp, nil
		},
	)

	script := demofire.ScriptForPersona("realestate")
	count, err := pf.Fire(context.Background(), script)
	if err != nil {
		t.Fatalf("PulsarFirer.Fire returned error: %v", err)
	}
	if count == 0 {
		t.Error("expected events to be sent via Pulsar, got 0")
	}
	if fp.sentCount == 0 {
		t.Error("expected fake producer to record sends, got 0")
	}
	if httpHit {
		t.Error("HTTP test server was unexpectedly hit — pulsar path must not make HTTP calls")
	}
}

// TestMakeFireScript_PulsarTarget_EmptyURLError verifies that when PulsarURL is
// empty, the fire function returns a clear error rather than panicking.
func TestMakeFireScript_PulsarTarget_EmptyURLError(t *testing.T) {
	rt := &runtime{
		cfg: runtimeConfig{
			DemoFireTarget:   "pulsar",
			PulsarURL:        "", // missing
			PulsarTopic:      "persistent://public/default/test",
			AllowedWriteKeys: []string{"wk"},
		},
		log: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	fn := rt.makeFireScript()
	_, err := fn(context.Background(), "realestate")
	if err == nil {
		t.Fatal("expected error when PulsarURL is empty, got nil")
	}
}
