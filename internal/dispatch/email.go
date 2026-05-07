package dispatch

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/db"
)

// MockEmailBackend persists "outbound" emails as rows in the mock_emails
// table — there is no SMTP integration. The mock-email viewer panel in the
// UI reads from this table to render a faux Gmail card.
//
// finalURL points to the API route the UI uses to fetch a single mock email
// by ID: "/api/mock-emails/{uuid}". The UUID is generated client-side here so
// we can return the deep-link without a second round-trip.
type MockEmailBackend struct {
	pool *pgxpool.Pool
	// triggerIDFn is an optional hook the dispatcher can use to associate
	// outbound emails with their triggering Trigger row. Default: no FK.
	triggerIDFn func(ctx context.Context, persona string, payload ActionPayload) *uuid.UUID
	// defaultTo is the fallback recipient when payload omits to_email.
	defaultTo string
}

// NewMockEmailBackend builds a backend that writes to mock_emails via the
// shared pgx pool. defaultTo is used when the payload doesn't carry a
// recipient (the rs-self demo doesn't enrich until WP-F wires it up).
func NewMockEmailBackend(pool *pgxpool.Pool, defaultTo string) *MockEmailBackend {
	if defaultTo == "" {
		defaultTo = "demo@rudderstack.com"
	}
	return &MockEmailBackend{pool: pool, defaultTo: defaultTo}
}

// Dispatch writes a row to mock_emails. The payload's parsed JSON is expected
// to contain `subject`, `body_markdown`, and `doc_links` (real-estate /
// rs_onboarding shape). Missing fields are tolerated — we substitute
// reasonable defaults so the UI never renders a blank card.
func (m *MockEmailBackend) Dispatch(ctx context.Context, persona string, payload ActionPayload) (string, string, error) {
	if m.pool == nil {
		return "failed", "", oops.Errorf("email: pool is nil")
	}
	parsed := payload.Parsed()

	subject := stringOrEmpty(parsed, "subject")
	if subject == "" {
		subject = stringOrEmpty(parsed, "headline")
	}
	if subject == "" {
		subject = "RudderStack notification"
	}

	body := stringOrEmpty(parsed, "body_markdown")
	if body == "" {
		// Fallback: if the LLM didn't produce a body, render the parsed JSON
		// as a fenced code block so the UI shows something meaningful.
		raw := payload.Raw()
		if raw == "" {
			body = "_(no body provided)_"
		} else {
			body = "```json\n" + raw + "\n```"
		}
	}

	links := encodeLinks(parsed)

	to := stringOrEmpty(parsed, "to_email")
	if to == "" {
		to = m.defaultTo
	}

	var triggerID *uuid.UUID
	if m.triggerIDFn != nil {
		triggerID = m.triggerIDFn(ctx, persona, payload)
	}

	row := db.MockEmailRow{
		TriggerID:    triggerID,
		ToEmail:      to,
		Subject:      subject,
		BodyMarkdown: body,
		Links:        links,
	}
	id, err := db.InsertMockEmail(ctx, m.pool, row)
	if err != nil {
		return "failed", "", oops.Wrapf(err, "email: insert mock email")
	}

	// finalURL is the deep-link the UI uses to fetch the dispatched email.
	finalURL := fmt.Sprintf("/api/mock-emails/%s", id)
	return "sent", finalURL, nil
}

// encodeLinks marshals payload["doc_links"] to JSONB bytes if present,
// otherwise returns nil so the column stores NULL.
func encodeLinks(parsed map[string]any) []byte {
	if parsed == nil {
		return nil
	}
	v, ok := parsed["doc_links"]
	if !ok || v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
