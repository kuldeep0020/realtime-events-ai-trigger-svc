package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samber/oops"
)

// localAgentDefaultTimeout is generous because SSE streams from the local
// agent can take tens of seconds for "Deep" model tier. Used by the seed
// CLI only; never on the hot path.
const localAgentDefaultTimeout = 60 * time.Second

// LocalAgentClient calls the local agent SSE endpoint to materialise canned
// responses at seed time. Streaming response chunks are concatenated and
// then JSON-decoded once the [DONE] sentinel is seen.
//
// Wire shape (per §3.7):
//
//	POST {url}
//	Authorization: Bearer {LOCAL_AGENT_TOKEN}
//	Body: {"message": "<rendered user>", "instructions": "<system>", "model": "Fast"}
//
// Response: text/event-stream lines of the form `data: {chunk}\n` (or
// `data: [DONE]\n` to signal completion). Some implementations emit JSON
// chunks like `{"text":"..."}`; we accumulate the raw text payload of each
// `data: ...` line, then attempt to decode the whole accumulator as JSON
// for the final ActionResult.Parsed map. If the agent returns plain text we
// surface it as Raw with Parsed set to a single-key envelope.
type LocalAgentClient struct {
	url    string
	bearer string
	model  string
	http   *http.Client
}

// LocalAgentConfig holds env-derived values for the local agent client.
type LocalAgentConfig struct {
	URL     string        // required
	Bearer  string        // required
	Model   string        // "Fast" | "Balanced" | "Deep"; defaults to Fast
	Timeout time.Duration // 0 → defaults to 60s
}

// NewLocalAgentClient validates config and returns a LocalAgentClient.
func NewLocalAgentClient(cfg LocalAgentConfig) (*LocalAgentClient, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, oops.Errorf("LocalAgentClient: LOCAL_AGENT_URL required")
	}
	if strings.TrimSpace(cfg.Bearer) == "" {
		return nil, oops.Errorf("LocalAgentClient: LOCAL_AGENT_TOKEN required")
	}
	model := cfg.Model
	if model == "" {
		model = "Fast"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = localAgentDefaultTimeout
	}
	return &LocalAgentClient{
		url:    cfg.URL,
		bearer: cfg.Bearer,
		model:  model,
		http:   &http.Client{Timeout: timeout},
	}, nil
}

// Generate renders the action-template prompts (loaded from PG by the
// caller via db.LoadActionTemplate) into (system, user), POSTs them to the
// local agent, accumulates the SSE stream, and packages the result as
// ActionResult.
//
// Note: this implementation expects the caller to inject the rendered
// prompt strings via TemplateVars in a pre-prepared JSON envelope. Because
// the seed CLI is the sole consumer, we keep the surface simple: the
// caller renders prompts (with RenderPrompt) and embeds them into vars
// using the convention that vars.WindowSnapshotJSON carries the system
// prompt and vars.FullEventsJSON carries the user prompt. This keeps
// Generate's signature identical to CannedClient, allowing one-line swap.
//
// The convention is documented here rather than enforced via a separate
// type because the seed CLI is the only live-mode caller and contracting
// it via the existing TemplateVars avoids a parallel type hierarchy.
func (c *LocalAgentClient) Generate(ctx context.Context, templateName string, vars TemplateVars) (ActionResult, error) {
	if c == nil || c.http == nil {
		return ActionResult{}, oops.Errorf("LocalAgentClient not initialized")
	}

	body, err := json.Marshal(map[string]string{
		"message":      vars.FullEventsJSON,     // user prompt
		"instructions": vars.WindowSnapshotJSON, // system prompt
		"model":        c.model,
	})
	if err != nil {
		return ActionResult{}, oops.Wrapf(err, "marshal local-agent body")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return ActionResult{}, oops.Wrapf(err, "build local-agent request")
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.bearer)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return ActionResult{}, oops.Wrapf(err, "local-agent http call")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ActionResult{}, oops.
			With("status_code", resp.StatusCode).
			With("body", string(errBody)).
			Errorf("local-agent: non-2xx response")
	}

	accumulated, err := accumulateSSE(resp.Body)
	if err != nil {
		return ActionResult{}, oops.Wrapf(err, "local-agent SSE accumulate")
	}

	parsed := map[string]any{}
	if err := json.Unmarshal([]byte(accumulated), &parsed); err != nil {
		// Plain-text streaming response — wrap it.
		parsed = map[string]any{"text": accumulated}
	}

	return ActionResult{
		Template: templateName,
		Raw:      accumulated,
		Parsed:   parsed,
		Source:   "live",
	}, nil
}

// accumulateSSE reads an SSE stream from r and returns the concatenated
// data payload. It honours the standard SSE conventions:
//
//   - Lines starting with `data: ` carry the payload chunk.
//   - A line of `data: [DONE]` is the terminal sentinel; we stop reading.
//   - Lines that don't start with `data:` (event types, comments, blank
//     keep-alives) are ignored.
//
// Notes on chunk concatenation: Some agents emit JSON-per-chunk (e.g.
// `{"text":"hello"}` then `{"text":" world"}`); others emit one final
// JSON. We concatenate the raw post-`data: ` bytes verbatim. The caller
// decides how to parse the accumulator — if it parses as JSON we return
// the parsed map; otherwise we wrap as `{"text": "..."}`.
func accumulateSSE(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	// SSE chunks can be larger than the default 64KiB; bump to 1MiB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var b strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			// `event:`, comment lines, etc — ignore.
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		// Try to parse a JSON-shaped chunk and concatenate any `text` key
		// or the whole payload if it's just a string. Otherwise append the
		// raw payload as-is.
		if extracted, ok := extractTextChunk(payload); ok {
			b.WriteString(extracted)
			continue
		}
		b.WriteString(payload)
	}
	if err := scanner.Err(); err != nil {
		return "", oops.Wrapf(err, "scan SSE stream")
	}
	return b.String(), nil
}

// extractTextChunk returns the inner string when the payload is
// `{"text": "..."}` or a quoted JSON string. The boolean indicates whether
// extraction was applied; callers fall back to the raw payload otherwise.
func extractTextChunk(payload string) (string, bool) {
	// Quoted-string-only chunk: `"hello"` → hello.
	if strings.HasPrefix(payload, `"`) {
		var s string
		if err := json.Unmarshal([]byte(payload), &s); err == nil {
			return s, true
		}
		return "", false
	}
	if strings.HasPrefix(payload, "{") {
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err == nil {
			if t, ok := m["text"].(string); ok {
				return t, true
			}
		}
	}
	return "", false
}
