package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/samber/oops"
)

// Slack delivery tunables. Kept package-private so tests can't accidentally
// depend on them. attemptTimeout governs the per-attempt HTTP timeout; the
// total time spent is bounded by sum(backoff) + maxAttempts*attemptTimeout.
const (
	slackMaxAttempts    = 3
	slackAttemptTimeout = 5 * time.Second
)

// slackBackoffs is the per-attempt backoff schedule. With slackMaxAttempts=3,
// we sleep BEFORE attempts 2 and 3 only — never before attempt 1. The
// schedule is indexed [attemptNumber-1]: attempt 1 → 0 (no sleep), attempt
// 2 → 200ms, attempt 3 → 400ms. The next-doubling value (800ms) is the
// pattern's hint for callers extending slackMaxAttempts.
var slackBackoffs = []time.Duration{
	0,
	200 * time.Millisecond,
	400 * time.Millisecond,
}

// SlackBackend posts a Block Kit message to a Slack incoming webhook. The
// retry policy is "3 attempts, exponential backoff (200ms, 400ms)" on any
// non-2xx response. Per-attempt timeout is 5s.
type SlackBackend struct {
	webhookURL string
	httpClient *http.Client
	// nowFn lets tests inject a deterministic clock; defaults to time.Now.
	nowFn func() time.Time
	// sleepFn lets tests skip real sleeping during retry tests.
	sleepFn func(d time.Duration)
}

// NewSlackBackend builds a SlackBackend. The HTTP client's per-request
// timeout is overridden per-attempt via context, so we use a no-timeout
// client to avoid double-deadline interactions.
func NewSlackBackend(webhookURL string) *SlackBackend {
	return &SlackBackend{
		webhookURL: webhookURL,
		httpClient: &http.Client{}, // per-attempt deadline via ctx
		nowFn:      time.Now,
		sleepFn:    time.Sleep,
	}
}

// Dispatch posts the payload to the Slack webhook. Returns ("sent", url, nil)
// on success; ("failed", "", err) after all retries exhausted.
func (s *SlackBackend) Dispatch(ctx context.Context, persona string, payload ActionPayload) (string, string, error) {
	if s.webhookURL == "" {
		return "failed", "", oops.Errorf("slack: webhook URL is empty")
	}
	body, err := buildBlockKit(persona, payload)
	if err != nil {
		return "failed", "", oops.Wrapf(err, "slack: build block kit")
	}

	var lastErr error
	for attempt := 1; attempt <= slackMaxAttempts; attempt++ {
		// Honour context cancellation BEFORE sleeping; otherwise a cancelled
		// ctx still consumes the backoff wait.
		if err := ctx.Err(); err != nil {
			return "failed", "", oops.Wrapf(err, "slack: context cancelled before attempt %d", attempt)
		}
		if backoff := slackBackoffs[attempt-1]; backoff > 0 {
			s.sleepFn(backoff)
			if err := ctx.Err(); err != nil {
				return "failed", "", oops.Wrapf(err, "slack: context cancelled during backoff before attempt %d", attempt)
			}
		}

		err := s.postOnce(ctx, body)
		if err == nil {
			return "sent", s.webhookURL, nil
		}
		lastErr = err
	}
	return "failed", "", oops.
		With("max_attempts", slackMaxAttempts).
		Wrapf(lastErr, "slack: all %d attempts failed", slackMaxAttempts)
}

// postOnce performs a single HTTP POST under a per-attempt context deadline.
// Returns nil for any 2xx; otherwise an oops error wrapping the stringified
// response body (truncated to 256 bytes).
func (s *SlackBackend) postOnce(ctx context.Context, body []byte) error {
	attemptCtx, cancel := context.WithTimeout(ctx, slackAttemptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return oops.Wrapf(err, "slack: new request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return oops.Wrapf(err, "slack: http do")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Drain body so the connection can be reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}

	// Read up to 256 bytes for diagnostics.
	preview, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	return oops.
		With("status_code", resp.StatusCode).
		With("body_preview", string(preview)).
		Errorf("slack: non-2xx response: %d", resp.StatusCode)
}

// buildBlockKit converts an ActionPayload into the JSON-serialised Slack
// Block Kit message body. Currently supports the real-estate template shape;
// other personas / templates fall back to a minimal section block.
func buildBlockKit(persona string, payload ActionPayload) ([]byte, error) {
	parsed := payload.Parsed()
	template := payload.Template()

	if template == "realestate_realtor_pitch" || persona == "realestate" {
		return buildRealestateBlocks(parsed)
	}
	// Generic fallback.
	return buildGenericBlocks(template, parsed)
}

// buildRealestateBlocks renders the canonical real-estate realtor pitch.
// Required fields: headline, talking_points, best_cta. Optional: assigned_realtor.
func buildRealestateBlocks(p map[string]any) ([]byte, error) {
	headline := stringOrEmpty(p, "headline")
	if headline == "" {
		headline = "Trigger fired"
	}
	bestCTA := stringOrEmpty(p, "best_cta")
	talkingPoints := stringSliceFromAny(p["talking_points"])
	assignedRealtor := stringOrEmpty(p, "assigned_realtor")
	urgency := stringOrEmpty(p, "urgency")

	blocks := make([]map[string]any, 0, 5)

	// Header
	blocks = append(blocks, map[string]any{
		"type": "header",
		"text": map[string]any{
			"type":  "plain_text",
			"text":  truncate(headline, 150),
			"emoji": true,
		},
	})

	// Section: bullet list of talking points + CTA. We compose mrkdwn with
	// bullets — Slack mrkdwn supports • as a literal bullet character.
	var sectionLines []string
	for _, tp := range talkingPoints {
		if tp == "" {
			continue
		}
		sectionLines = append(sectionLines, "• "+tp)
	}
	if bestCTA != "" {
		if len(sectionLines) > 0 {
			sectionLines = append(sectionLines, "")
		}
		sectionLines = append(sectionLines, "*Best CTA:* "+bestCTA)
	}
	if len(sectionLines) > 0 {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": joinLines(sectionLines, 2900), // Slack section text cap is 3000.
			},
		})
	}

	blocks = append(blocks, map[string]any{"type": "divider"})

	// Context: realtor + urgency
	contextElements := make([]map[string]any, 0, 2)
	if assignedRealtor != "" {
		contextElements = append(contextElements, map[string]any{
			"type": "mrkdwn",
			"text": "*Assigned realtor:* " + assignedRealtor,
		})
	}
	if urgency != "" {
		contextElements = append(contextElements, map[string]any{
			"type": "mrkdwn",
			"text": "*Urgency:* " + urgency,
		})
	}
	if len(contextElements) > 0 {
		blocks = append(blocks, map[string]any{
			"type":     "context",
			"elements": contextElements,
		})
	}

	body := map[string]any{
		"text":   truncate(headline, 150), // fallback for clients that can't render blocks
		"blocks": blocks,
	}
	return json.Marshal(body)
}

// buildGenericBlocks is a minimal fallback for unknown templates. The shape
// is still valid Block Kit so Slack always renders something.
func buildGenericBlocks(template string, p map[string]any) ([]byte, error) {
	subject := stringOrEmpty(p, "subject")
	if subject == "" {
		subject = stringOrEmpty(p, "headline")
	}
	if subject == "" {
		subject = fmt.Sprintf("Trigger fired (%s)", template)
	}

	body := map[string]any{
		"text": subject,
		"blocks": []map[string]any{
			{
				"type": "header",
				"text": map[string]any{
					"type":  "plain_text",
					"text":  truncate(subject, 150),
					"emoji": true,
				},
			},
			{
				"type": "section",
				"text": map[string]any{
					"type": "mrkdwn",
					"text": fmt.Sprintf("Action template: `%s`", template),
				},
			},
		},
	}
	return json.Marshal(body)
}

// --- helpers ---

func stringOrEmpty(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// stringSliceFromAny coerces a JSON-decoded []any of strings to []string.
// Returns nil for any non-slice or for slices containing non-string elements
// (we silently skip non-strings).
func stringSliceFromAny(v any) []string {
	switch x := v.(type) {
	case []string:
		out := make([]string, len(x))
		copy(out, x)
		return out
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// truncate trims s to maxLen runes, appending an ellipsis when shortened.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// joinLines joins lines with "\n", capping the total length to maxBytes
// (truncates the tail with an ellipsis if exceeded).
func joinLines(lines []string, maxBytes int) string {
	var buf bytes.Buffer
	for i, line := range lines {
		if i > 0 {
			buf.WriteByte('\n')
		}
		if buf.Len()+len(line) > maxBytes {
			remaining := maxBytes - buf.Len()
			if remaining > 3 {
				buf.WriteString(line[:remaining-3])
				buf.WriteString("...")
			}
			break
		}
		buf.WriteString(line)
	}
	return buf.String()
}
