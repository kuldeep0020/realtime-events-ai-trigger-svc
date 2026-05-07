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
	"sync"
	"sync/atomic"
	"time"

	"github.com/samber/oops"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/event"
)

// NamedScript pairs a persona label with a pre-built script and the
// anonymousId that script fires under. Used by RunConcurrent.
type NamedScript struct {
	Persona string
	Script  []ScriptStep
	AnonID  string
}

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
//   - Speed:        playback multiplier; 1.0 = real-time, 2.0 = double speed, 0.5 = half speed.
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
	// Speed is a playback multiplier applied to all DelayMs values.
	// 1.0 = real-time (default), 2.0 = halves delays, 0.5 = doubles delays.
	// Values <= 0 are treated as 1.0.
	Speed float64
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
		Speed:        1.0,
	}
}

// speedOrOne returns f.Speed if it is positive, otherwise 1.0.
func (f *Firer) speedOrOne() float64 {
	if f.Speed <= 0 {
		return 1.0
	}
	return f.Speed
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

	speed := f.speedOrOne()
	endpoint := f.IngestionURL + "/v1/batch"
	authHeader := basicAuth(f.WriteKey, "")

	var sent int
	for i, step := range script {
		if err := ctx.Err(); err != nil {
			return sent, oops.Wrapf(err, "Fire: cancelled before step %d", i)
		}
		if step.DelayMs > 0 {
			actualDelay := time.Duration(float64(step.DelayMs)*(1.0/speed)) * time.Millisecond
			if err := sleepWithCtx(ctx, f.Sleep, actualDelay); err != nil {
				return sent, oops.Wrapf(err, "Fire: cancelled during delay before step %d", i)
			}
		}

		// Re-stamp the event at actual send time so timestamps are spread
		// across the real wall-clock duration of the script, not all identical
		// to the moment the script slice was constructed.
		ev := step.Event
		now := time.Now().UTC()
		ev.OriginalTimestamp = now
		ev.SentAt = now

		body, err := buildBatchBody(ev)
		if err != nil {
			return sent, oops.With("step", i).Wrapf(err, "Fire: build body")
		}
		if err := f.postOne(ctx, endpoint, authHeader, body); err != nil {
			return sent, oops.With("step", i).Wrap(err)
		}
		sent++
		f.Logger.Info("demo-fire: step sent",
			"step", i,
			"type", ev.Type,
			"event", ev.Event,
			"path", ev.PagePath(),
		)
	}
	return sent, nil
}

// RunConcurrent fires multiple named scripts in parallel goroutines.
// Each goroutine is staggered by 500ms × its index offset so the dashboard
// SSE feed shows scripts spinning up in sequence rather than racing in
// lockstep.
//
// Speed applies to all scripts. The first error encountered is returned after
// all goroutines have drained (to avoid leaks). Total sent is the sum across
// all goroutines.
func (f *Firer) RunConcurrent(ctx context.Context, scripts []NamedScript, speed float64) (int, error) {
	if len(scripts) == 0 {
		return 0, nil
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

	// Build a per-goroutine firer that inherits all settings but uses the
	// supplied speed. We share the HTTP client (goroutine-safe).
	effectiveSpeed := speed
	if effectiveSpeed <= 0 {
		effectiveSpeed = 1.0
	}

	var (
		totalSent atomic.Int64
		firstErr  error
		mu        sync.Mutex
		wg        sync.WaitGroup
	)

	for i, ns := range scripts {
		i, ns := i, ns // capture loop vars
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Stagger start: 500ms × index (scaled by speed)
			staggerDelay := time.Duration(float64(500*i)*(1.0/effectiveSpeed)) * time.Millisecond
			if staggerDelay > 0 {
				if err := sleepWithCtx(ctx, f.Sleep, staggerDelay); err != nil {
					// ctx cancelled during stagger — record error but don't abort yet
					mu.Lock()
					if firstErr == nil {
						firstErr = oops.Wrapf(err, "RunConcurrent: stagger cancelled for script %d (%s)", i, ns.Persona)
					}
					mu.Unlock()
					return
				}
			}

			// Create a per-goroutine Firer that shares the HTTP client but has
			// independent Speed so we don't race on f.Speed.
			gf := &Firer{
				IngestionURL: f.IngestionURL,
				WriteKey:     f.WriteKey,
				HTTPClient:   f.HTTPClient,
				Logger:       f.Logger.With("concurrent_script", i, "persona", ns.Persona, "anon_id", ns.AnonID),
				Sleep:        f.Sleep,
				Speed:        effectiveSpeed,
			}

			sent, err := gf.Fire(ctx, ns.Script)
			totalSent.Add(int64(sent))

			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return int(totalSent.Load()), firstErr
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
