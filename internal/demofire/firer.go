package demofire

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/event"
)

// Firer POSTs persona-specific browser-channel event sequences to the
// RudderStack ingestion service.
//
// Configuration:
//
//   - IngestionURL: base URL like https://rudderstacvilo.dev-rudder.rudderlabs.com
//   - WriteKey:     workspace write key, used as Basic-auth username with empty password
//   - HTTPClient:   optional override for tests; defaults to a 10s-timeout client
//   - Logger:       optional; defaults to slog.Default()
//   - Sleep:        optional clock-injection hook for tests; defaults to time.Sleep
//
// Concurrency: a Firer is safe to use sequentially; running two Fire calls
// against the same Firer concurrently is permitted but the underlying HTTP
// client is shared so high concurrency is not the design intent.
type Firer struct {
	IngestionURL string
	WriteKey     string
	HTTPClient   *http.Client
	Logger       *slog.Logger
	// Sleep is invoked between steps with the relative delay. Defaults to
	// time.Sleep. Test code injects a no-op or a deterministic mock.
	Sleep func(time.Duration)
}

// NewFirer constructs a Firer with sensible defaults. Both IngestionURL
// and WriteKey are required — a clear error is returned at Fire time when
// either is empty.
func NewFirer(ingestionURL, writeKey string) *Firer {
	return &Firer{
		IngestionURL: strings.TrimRight(ingestionURL, "/"),
		WriteKey:     writeKey,
		HTTPClient:   &http.Client{Timeout: 10 * time.Second},
		Logger:       slog.Default(),
		Sleep:        time.Sleep,
	}
}

// Fire walks the provided script, sleeping for each step's DelayMs before
// POSTing the event in a `{"batch":[…]}` body. Returns the count of events
// that were sent and the first error encountered (subsequent steps short-
// circuit on error).
//
// Honours ctx cancellation during sleep AND between steps so a SIGINT
// terminates promptly without waiting out the full schedule.
func (f *Firer) Fire(ctx context.Context, script []ScriptStep) (int, error) {
	if f == nil {
		return 0, oops.Errorf("Fire: nil Firer")
	}
	if f.IngestionURL == "" {
		return 0, oops.Errorf("Fire: IngestionURL is empty")
	}
	if f.WriteKey == "" {
		return 0, oops.Errorf("Fire: WriteKey is empty")
	}
	if f.HTTPClient == nil {
		f.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if f.Logger == nil {
		f.Logger = slog.Default()
	}
	if f.Sleep == nil {
		f.Sleep = time.Sleep
	}

	endpoint := f.IngestionURL + "/v1/batch"
	authHeader := basicAuth(f.WriteKey, "")

	var sent int
	for i, step := range script {
		if err := ctx.Err(); err != nil {
			return sent, oops.Wrapf(err, "Fire: cancelled before step %d", i)
		}
		if step.DelayMs > 0 {
			if err := sleepWithCtx(ctx, f.Sleep, time.Duration(step.DelayMs)*time.Millisecond); err != nil {
				return sent, oops.Wrapf(err, "Fire: cancelled during delay before step %d", i)
			}
		}

		body, err := buildBatchBody(step.Event)
		if err != nil {
			return sent, oops.With("step", i).Wrapf(err, "Fire: build body")
		}
		if err := f.postOne(ctx, endpoint, authHeader, body); err != nil {
			return sent, oops.With("step", i).Wrap(err)
		}
		sent++
		f.Logger.Info("demo-fire: step sent",
			"step", i,
			"type", step.Event.Type,
			"event", step.Event.Event,
			"path", step.Event.PagePath(),
		)
	}
	return sent, nil
}

// postOne sends a single batch body and returns nil on 2xx, error otherwise.
func (f *Firer) postOne(ctx context.Context, endpoint, authHeader string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return oops.Wrapf(err, "build request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)

	resp, err := f.HTTPClient.Do(req)
	if err != nil {
		return oops.Wrapf(err, "http do")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return oops.
			With("status_code", resp.StatusCode).
			With("body_preview", string(preview)).
			Errorf("non-2xx response: %d", resp.StatusCode)
	}
	// Drain body so HTTP keep-alive can reuse the connection.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<14))
	return nil
}

// buildBatchBody marshals a single Event into a {"batch":[<event>]} body.
// We marshal each event individually rather than relying on the Batch
// struct so we can stamp `sentAt` at request time without mutating the
// shared Event value.
func buildBatchBody(e event.Event) ([]byte, error) {
	body := map[string]any{
		"batch": []event.Event{e},
		// sentAt is a string in ingestion-svc parlance — we emit ISO 8601.
		"sentAt": time.Now().UTC().Format(time.RFC3339Nano),
	}
	return json.Marshal(body)
}

// basicAuth returns the Authorization header value for HTTP Basic auth.
// Format: "Basic base64(<user>:<pass>)". RudderStack uses writeKey as
// the username with an empty password.
func basicAuth(user, pass string) string {
	creds := user + ":" + pass
	enc := base64.StdEncoding.EncodeToString([]byte(creds))
	return "Basic " + enc
}

// sleepWithCtx splits a single Sleep into a context-aware wait loop. We
// chunk into small slices so SIGINT during a long delay terminates within
// ~50ms regardless of total step duration.
func sleepWithCtx(ctx context.Context, sleepFn func(time.Duration), total time.Duration) error {
	if total <= 0 {
		return nil
	}
	const slice = 50 * time.Millisecond
	deadline := time.Now().Add(total)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil
		}
		if remaining > slice {
			sleepFn(slice)
			continue
		}
		sleepFn(remaining)
	}
}

// describePersona returns a one-line summary string for log messages.
func describePersona(persona string, scriptLen int) string {
	return fmt.Sprintf("persona=%s steps=%d", persona, scriptLen)
}
