package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/demofire"
)

// runDemoFire dispatches the persona-specific event sequence either to the
// ingestion service over HTTP or directly to a Pulsar topic.
//
// Flags:
//
//	--persona              "realestate" | "rs-self" (required)
//	--target               "http" (default) | "pulsar"
//	--write-key            override the persona's default writeKey
//	--ingestion-url        override INGESTION_URL env (http target only)
//	--pulsar-url           override PULSAR_URL env (pulsar target only)
//	--pulsar-topic         override PULSAR_TOPIC env (pulsar target only)
//	--pulsar-token         override PULSAR_JWT_TOKEN env (pulsar target only)
//	--pulsar-tls-certs     override PULSAR_TLS_TRUST_CERTS env (pulsar target only)
//	--pulsar-source-id     set sourceId property (defaults to writeKey)
func runDemoFire(args []string) {
	fs := flag.NewFlagSet("demo-fire", flag.ExitOnError)

	persona := fs.String("persona", "", "demo persona: 'realestate' or 'rs-self'")
	target := fs.String("target", "http", "publish target: 'http' or 'pulsar'")
	writeKey := fs.String("write-key", "", "override the persona's default writeKey")

	// HTTP-target flags.
	ingestionURL := fs.String("ingestion-url", os.Getenv("INGESTION_URL"), "override INGESTION_URL env (http target)")

	// Pulsar-target flags.
	pulsarURL := fs.String("pulsar-url", os.Getenv("PULSAR_URL"), "Pulsar broker URL (pulsar target)")
	pulsarTopic := fs.String("pulsar-topic", os.Getenv("PULSAR_TOPIC"), "Pulsar topic (pulsar target)")
	pulsarToken := fs.String("pulsar-token", os.Getenv("PULSAR_JWT_TOKEN"), "JWT token (pulsar target)")
	pulsarTLSCerts := fs.String("pulsar-tls-certs", os.Getenv("PULSAR_TLS_TRUST_CERTS"), "path to CA cert (pulsar target)")
	pulsarSourceID := fs.String("pulsar-source-id", "", "sourceId property (defaults to writeKey)")
	pulsarValidateHostname := fs.Bool("pulsar-validate-hostname", envBoolDefault("PULSAR_TLS_VALIDATE_HOSTNAME", true), "validate TLS hostname (pulsar target)")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "demo-fire: parse flags: %v\n", err)
		os.Exit(2)
	}

	if *persona == "" {
		fmt.Fprintln(os.Stderr, "demo-fire: --persona is required ('realestate' or 'rs-self')")
		os.Exit(2)
	}

	script := demofire.ScriptForPersona(*persona)
	if script == nil {
		fmt.Fprintf(os.Stderr, "demo-fire: unknown persona %q\n", *persona)
		os.Exit(2)
	}

	wk := *writeKey
	if wk == "" {
		wk = pickDefaultWriteKey(*persona)
	}
	if wk == "" {
		fmt.Fprintln(os.Stderr, "demo-fire: --write-key is required (no default for persona)")
		os.Exit(2)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var count int
	var err error

	switch *target {
	case "http":
		if *ingestionURL == "" {
			fmt.Fprintln(os.Stderr, "demo-fire: --ingestion-url or INGESTION_URL env is required for --target http")
			os.Exit(2)
		}
		log.Info("demo-fire: starting",
			"persona", *persona,
			"target", "http",
			"steps", len(script),
			"ingestion_url", *ingestionURL,
		)
		firer := demofire.NewFirer(*ingestionURL, wk)
		firer.Logger = log
		count, err = firer.Fire(ctx, script)

	case "pulsar":
		if *pulsarURL == "" {
			fmt.Fprintln(os.Stderr, "demo-fire: --pulsar-url or PULSAR_URL env is required for --target pulsar")
			os.Exit(2)
		}
		if *pulsarTopic == "" {
			fmt.Fprintln(os.Stderr, "demo-fire: --pulsar-topic or PULSAR_TOPIC env is required for --target pulsar")
			os.Exit(2)
		}
		log.Info("demo-fire: starting",
			"persona", *persona,
			"target", "pulsar",
			"steps", len(script),
			"pulsar_url", *pulsarURL,
			"pulsar_topic", *pulsarTopic,
		)
		cfg := demofire.PulsarFirerConfig{
			URL:                 *pulsarURL,
			Topic:               *pulsarTopic,
			Token:               *pulsarToken,
			TLSTrustCertsFile:   *pulsarTLSCerts,
			TLSValidateHostname: *pulsarValidateHostname,
			WriteKey:            wk,
			SourceID:            *pulsarSourceID,
		}
		pf := demofire.NewPulsarFirer(cfg)
		pf.Logger = log
		count, err = pf.Fire(ctx, script)

	default:
		fmt.Fprintf(os.Stderr, "demo-fire: unknown --target %q (want 'http' or 'pulsar')\n", *target)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "demo-fire: failed after %d steps: %v\n", count, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "demo-fire: sent %d events\n", count)
}

// pickDefaultWriteKey resolves the persona-default writeKey, preferring an
// override from ALLOWED_WRITE_KEYS (first comma-separated entry) when the
// persona library doesn't have a hardcoded one. The hardcoded values are
// the production demo writeKeys from §0; they're safe to ship since the
// demo workspace is dedicated to the hackathon.
func pickDefaultWriteKey(persona string) string {
	if k := demofire.PersonaWriteKey(persona); k != "" {
		return k
	}
	if list := strings.TrimSpace(os.Getenv("ALLOWED_WRITE_KEYS")); list != "" {
		first, _, _ := strings.Cut(list, ",")
		return strings.TrimSpace(first)
	}
	return ""
}

// envBoolDefault parses env var name as a boolean, returning dflt when absent
// or unparseable. Accepts "0", "false", "no" as false; anything else true.
func envBoolDefault(name string, dflt bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if v == "" {
		return dflt
	}
	return v != "0" && v != "false" && v != "no"
}
