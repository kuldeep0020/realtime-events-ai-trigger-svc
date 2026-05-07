package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samber/oops"
)

// bedrockDefaultTimeout is conservative; Bedrock anthropic-claude calls
// regularly run >30s for long contexts.
const bedrockDefaultTimeout = 60 * time.Second

// BedrockClient is the documented fallback path described in §0 / §3.7
// using the presigned-URL pattern (12h-TTL key). The skeleton below is
// compile-clean and interface-conforming so the wiring layer can swap in
// when local-agent is unavailable; the live behaviour is intentionally
// stubbed since Bedrock is out of hackathon scope.
//
// Wire shape (anthropic-on-bedrock, model invoke endpoint):
//
//	POST {presignedURL}                  // already signed; 12h TTL
//	Content-Type: application/json
//	Body: {
//	  "anthropic_version": "bedrock-2023-05-31",
//	  "max_tokens": 1024,
//	  "system":  "<system prompt>",
//	  "messages": [{"role":"user","content":"<user prompt>"}]
//	}
//	Resp: {"content": [{"type":"text","text":"..."}], ...}
type BedrockClient struct {
	presignedURL string
	http         *http.Client
	maxTokens    int
}

// BedrockConfig collects the env-derived inputs.
type BedrockConfig struct {
	PresignedURL string        // BEDROCK_API_KEY (a presigned invoke URL)
	Timeout      time.Duration // 0 → 60s
	MaxTokens    int           // 0 → 1024
}

// NewBedrockClient validates config.
func NewBedrockClient(cfg BedrockConfig) (*BedrockClient, error) {
	if strings.TrimSpace(cfg.PresignedURL) == "" {
		return nil, oops.Errorf("BedrockClient: BEDROCK_API_KEY (presigned URL) required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = bedrockDefaultTimeout
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	return &BedrockClient{
		presignedURL: cfg.PresignedURL,
		http:         &http.Client{Timeout: timeout},
		maxTokens:    maxTokens,
	}, nil
}

// Generate is a skeleton implementation: it sends a single anthropic-style
// request and returns the raw text content. The same TemplateVars
// convention as LocalAgentClient applies — the caller embeds the rendered
// (system, user) prompts in WindowSnapshotJSON / FullEventsJSON.
//
// We do NOT exercise this path in the demo; the function is here so the
// wiring layer can compile-check the swap.
func (c *BedrockClient) Generate(ctx context.Context, templateName string, vars TemplateVars) (ActionResult, error) {
	if c == nil || c.http == nil {
		return ActionResult{}, oops.Errorf("BedrockClient not initialized")
	}

	body, err := json.Marshal(map[string]any{
		"anthropic_version": "bedrock-2023-05-31",
		"max_tokens":        c.maxTokens,
		"system":            vars.WindowSnapshotJSON,
		"messages": []map[string]string{
			{"role": "user", "content": vars.FullEventsJSON},
		},
	})
	if err != nil {
		return ActionResult{}, oops.Wrapf(err, "marshal bedrock body")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.presignedURL, bytes.NewReader(body))
	if err != nil {
		return ActionResult{}, oops.Wrapf(err, "build bedrock request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return ActionResult{}, oops.Wrapf(err, "bedrock http call")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ActionResult{}, oops.
			With("status_code", resp.StatusCode).
			With("body", string(errBody)).
			Errorf("bedrock: non-2xx response")
	}

	var envelope struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return ActionResult{}, oops.Wrapf(err, "decode bedrock response")
	}

	var combined strings.Builder
	for _, c := range envelope.Content {
		if c.Type == "text" {
			combined.WriteString(c.Text)
		}
	}
	raw := combined.String()
	parsed := map[string]any{"text": raw}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		parsed = map[string]any{"text": raw}
	}

	return ActionResult{
		Template: templateName,
		Raw:      raw,
		Parsed:   parsed,
		Source:   "live",
	}, nil
}
