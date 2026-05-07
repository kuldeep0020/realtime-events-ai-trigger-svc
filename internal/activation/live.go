package activation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/samber/oops"
)

// liveDefaultTimeout is the per-request timeout for the live Activation API.
// Kept short because the enricher fans out to LLM/Kapa calls afterwards and
// the user-facing budget is tight.
const liveDefaultTimeout = 5 * time.Second

// LiveClient calls the real RudderStack Activation API.
//
// Wire shape (per §3.5 / official v1 spec):
//
//	POST {baseURL}/activation
//	Authorization: Bearer {sat}
//	Content-Type: application/json
//	Body: {entity, destinationId, id:{type, value}}
//
// Response 200: {entity, id:{type, value}, data: {...traits}}.
//
// This client is wired but NOT exercised by the demo path
// (ACTIVATION_MODE=mock is the default). Production swap is a one-line
// substitution in the wiring layer.
type LiveClient struct {
	baseURL string
	sat     string
	http    *http.Client
}

// LiveConfig holds the few env-derived values the LiveClient needs.
type LiveConfig struct {
	BaseURL string        // e.g. https://profiles.rudderstack.com/v1
	SAT     string        // workspace Service Access Token (Bearer)
	Timeout time.Duration // 0 → defaults to 5s
}

// NewLiveClient validates config and returns a LiveClient ready for use.
//
// Returns an error when BaseURL or SAT is empty — the live mode contract
// requires both. The error path is intentionally explicit so a misconfigured
// production deploy fails fast at wiring time rather than at the first
// trigger.
func NewLiveClient(cfg LiveConfig) (*LiveClient, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, oops.Errorf("activation.LiveClient: ACTIVATION_BASE_URL required")
	}
	if strings.TrimSpace(cfg.SAT) == "" {
		return nil, oops.Errorf("activation.LiveClient: ACTIVATION_SAT required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = liveDefaultTimeout
	}

	return &LiveClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		sat:     cfg.SAT,
		http:    &http.Client{Timeout: timeout},
	}, nil
}

// GetProfile POSTs to {baseURL}/activation with Bearer auth and parses the
// response into a ProfileResponse. A 200 with empty `data` is a normal hit
// (profile present, traits empty); we surface it as Data: map[string]any{}.
func (c *LiveClient) GetProfile(ctx context.Context, req ProfileRequest) (ProfileResponse, error) {
	if c == nil || c.http == nil {
		return ProfileResponse{}, oops.Errorf("activation.LiveClient not initialized")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return ProfileResponse{}, oops.Wrapf(err, "marshal request")
	}

	url := c.baseURL + "/activation"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ProfileResponse{}, oops.Wrapf(err, "build http request")
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.sat)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return ProfileResponse{}, oops.Wrapf(err, "activation http call")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read up to 4KiB of body for diagnostics; activation returns small JSON errors.
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ProfileResponse{}, oops.
			With("status_code", resp.StatusCode).
			With("body", string(errBody)).
			Errorf("activation: non-2xx response")
	}

	var out ProfileResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ProfileResponse{}, oops.Wrapf(err, "decode activation response")
	}
	if out.Data == nil {
		out.Data = map[string]any{}
	}
	// Best-effort defaults: the API echoes entity/id, but if the server omits
	// either we fall back to the request values for downstream stability.
	if out.Entity == "" {
		out.Entity = req.Entity
	}
	if out.ID == (ID{}) {
		out.ID = req.ID
	}
	return out, nil
}

// String returns a redacted summary suitable for logs.
func (c *LiveClient) String() string {
	if c == nil {
		return "activation.LiveClient(nil)"
	}
	return fmt.Sprintf("activation.LiveClient{baseURL=%s, sat=***}", c.baseURL)
}
