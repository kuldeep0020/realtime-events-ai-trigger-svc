package kapa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/samber/oops"
)

// liveDefaultTimeout is the per-request timeout for the live Kapa API.
const liveDefaultTimeout = 5 * time.Second

// liveDefaultBaseURL is the production Kapa endpoint host. Tests inject
// their own via LiveConfig.BaseURL.
const liveDefaultBaseURL = "https://api.kapa.ai"

// LiveClient calls the real Kapa.ai retrieval endpoint. Used by the seed CLI
// to materialize `canned_kapa_responses` from real API output.
type LiveClient struct {
	baseURL   string
	projectID string
	apiKey    string
	http      *http.Client
}

// LiveConfig holds the env-derived values the LiveClient needs.
type LiveConfig struct {
	BaseURL   string        // optional; defaults to https://api.kapa.ai
	ProjectID string        // required (KAPA_PROJECT_ID)
	APIKey    string        // required (KAPA_API_KEY)
	Timeout   time.Duration // 0 → defaults to 5s
}

// NewLiveClient validates config and returns a LiveClient ready for use.
func NewLiveClient(cfg LiveConfig) (*LiveClient, error) {
	if strings.TrimSpace(cfg.ProjectID) == "" {
		return nil, oops.Errorf("kapa.LiveClient: KAPA_PROJECT_ID required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, oops.Errorf("kapa.LiveClient: KAPA_API_KEY required")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = liveDefaultBaseURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = liveDefaultTimeout
	}
	return &LiveClient{
		baseURL:   base,
		projectID: url.PathEscape(cfg.ProjectID),
		apiKey:    cfg.APIKey,
		http:      &http.Client{Timeout: timeout},
	}, nil
}

// Retrieve POSTs to /query/v1/projects/{project_id}/chat/ with X-API-KEY
// auth and parses the canonical Kapa JSON response.
func (c *LiveClient) Retrieve(ctx context.Context, query string) (Result, error) {
	if c == nil || c.http == nil {
		return Result{}, oops.Errorf("kapa.LiveClient not initialized")
	}

	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return Result{}, oops.Wrapf(err, "marshal kapa query")
	}

	endpoint := fmt.Sprintf("%s/query/v1/projects/%s/chat/", c.baseURL, c.projectID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, oops.Wrapf(err, "build kapa request")
	}
	httpReq.Header.Set("X-API-KEY", c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Result{}, oops.Wrapf(err, "kapa http call")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Result{}, oops.
			With("status_code", resp.StatusCode).
			With("body", string(errBody)).
			Errorf("kapa: non-2xx response")
	}

	var out Result
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, oops.Wrapf(err, "decode kapa response")
	}
	if out.RelevantSources == nil {
		out.RelevantSources = []Source{}
	}
	out.Source = "live"
	return out, nil
}
