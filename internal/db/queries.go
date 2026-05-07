package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
)

// InsertEvent inserts a single event row into the events table and returns the
// generated BIGSERIAL id.
func InsertEvent(
	ctx context.Context,
	pool *pgxpool.Pool,
	anonID, userID, writeKey, eventType, eventName, pagePath string,
	payload []byte,
) (int64, error) {
	const q = `
		INSERT INTO events
		  (anonymous_id, user_id, write_key, event_type, event_name, page_path, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`

	var id int64
	err := pool.QueryRow(ctx, q,
		anonID, nullableText(userID), writeKey, eventType,
		nullableText(eventName), nullableText(pagePath), payload,
	).Scan(&id)
	if err != nil {
		return 0, oops.Wrapf(err, "InsertEvent")
	}
	return id, nil
}

// FetchEventsForAnon returns all events for a given anonymousId received after
// `since`, ordered by received_at ASC.
func FetchEventsForAnon(ctx context.Context, pool *pgxpool.Pool, anonID string, since time.Time) ([]EventRow, error) {
	const q = `
		SELECT id, COALESCE(pulsar_message_id,''), anonymous_id, COALESCE(user_id,''),
		       write_key, event_type, COALESCE(event_name,''), COALESCE(page_path,''),
		       payload, received_at
		FROM events
		WHERE anonymous_id = $1 AND received_at >= $2
		ORDER BY received_at ASC`

	rows, err := pool.Query(ctx, q, anonID, since)
	if err != nil {
		return nil, oops.Wrapf(err, "FetchEventsForAnon query")
	}
	defer rows.Close()

	var out []EventRow
	for rows.Next() {
		var r EventRow
		if err := rows.Scan(
			&r.ID, &r.PulsarMessageID, &r.AnonymousID, &r.UserID,
			&r.WriteKey, &r.EventType, &r.EventName, &r.PagePath,
			&r.Payload, &r.ReceivedAt,
		); err != nil {
			return nil, oops.Wrapf(err, "FetchEventsForAnon scan")
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Wrapf(err, "FetchEventsForAnon rows")
	}
	return out, nil
}

// LoadActiveRules returns all enabled rules for configs matching `persona`
// where the config is active.
func LoadActiveRules(ctx context.Context, pool *pgxpool.Pool, persona string) ([]RuleRow, error) {
	const q = `
		SELECT r.id, r.name, r.spec, c.persona, r.config_id
		FROM rules r
		JOIN configs c ON c.id = r.config_id
		WHERE c.persona = $1 AND c.active AND r.enabled`

	rows, err := pool.Query(ctx, q, persona)
	if err != nil {
		return nil, oops.Wrapf(err, "LoadActiveRules query")
	}
	defer rows.Close()

	var out []RuleRow
	for rows.Next() {
		var r RuleRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Spec, &r.Persona, &r.ConfigID); err != nil {
			return nil, oops.Wrapf(err, "LoadActiveRules scan")
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Wrapf(err, "LoadActiveRules rows")
	}
	return out, nil
}

// UpsertCooldown inserts or updates a cooldown record for (ruleID, anonID).
func UpsertCooldown(ctx context.Context, pool *pgxpool.Pool, ruleID uuid.UUID, anonID string, until time.Time) error {
	const q = `
		INSERT INTO cooldowns (rule_id, anonymous_id, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (rule_id, anonymous_id)
		DO UPDATE SET expires_at = EXCLUDED.expires_at`

	if _, err := pool.Exec(ctx, q, ruleID, anonID, until); err != nil {
		return oops.Wrapf(err, "UpsertCooldown")
	}
	return nil
}

// IsCooledDown returns true if an active (non-expired) cooldown exists for
// (ruleID, anonID).
func IsCooledDown(ctx context.Context, pool *pgxpool.Pool, ruleID uuid.UUID, anonID string) (bool, error) {
	const q = `
		SELECT EXISTS(
		  SELECT 1 FROM cooldowns
		  WHERE rule_id = $1 AND anonymous_id = $2 AND expires_at > NOW()
		)`

	var cooled bool
	if err := pool.QueryRow(ctx, q, ruleID, anonID).Scan(&cooled); err != nil {
		return false, oops.Wrapf(err, "IsCooledDown")
	}
	return cooled, nil
}

// InsertTrigger inserts a trigger row and returns the generated UUID.
// If t.ID is zero, the database generates a UUID via gen_random_uuid().
func InsertTrigger(ctx context.Context, pool *pgxpool.Pool, t TriggerRow) (uuid.UUID, error) {
	const q = `
		INSERT INTO triggers (
		  rule_id, rule_name, persona, anonymous_id, fired_at,
		  window_snapshot, full_events, enriched_traits, kapa_result,
		  llm_raw, llm_parsed, llm_source, destination, dispatch_status,
		  dispatched_at, error
		) VALUES (
		  $1, $2, $3, $4, $5,
		  $6, $7, $8, $9,
		  $10, $11, $12, $13, $14,
		  $15, $16
		)
		RETURNING id`

	firedAt := t.FiredAt
	if firedAt.IsZero() {
		firedAt = time.Now().UTC()
	}
	dispatchStatus := t.DispatchStatus
	if dispatchStatus == "" {
		dispatchStatus = "pending"
	}

	var id uuid.UUID
	err := pool.QueryRow(ctx, q,
		t.RuleID,
		t.RuleName,
		t.Persona,
		t.AnonymousID,
		firedAt,
		t.WindowSnapshot,
		t.FullEvents,
		nullableBytes(t.EnrichedTraits),
		nullableBytes(t.KapaResult),
		nullableText(t.LLMRaw),
		nullableBytes(t.LLMParsed),
		nullableText(t.LLMSource),
		t.Destination,
		dispatchStatus,
		t.DispatchedAt,
		nullableText(t.Error),
	).Scan(&id)
	if err != nil {
		return uuid.UUID{}, oops.Wrapf(err, "InsertTrigger")
	}
	return id, nil
}

// LoadCannedResponse returns the raw_json for the highest-priority canned
// response matching (templateName, persona). Returns pgx.ErrNoRows when absent.
func LoadCannedResponse(ctx context.Context, pool *pgxpool.Pool, templateName, persona string) ([]byte, error) {
	const q = `
		SELECT raw_json FROM canned_responses
		WHERE template_name = $1 AND persona = $2
		ORDER BY priority DESC
		LIMIT 1`

	var raw []byte
	err := pool.QueryRow(ctx, q, templateName, persona).Scan(&raw)
	if err != nil {
		if isNoRows(err) {
			return nil, pgx.ErrNoRows
		}
		return nil, oops.Wrapf(err, "LoadCannedResponse")
	}
	return raw, nil
}

// LoadCannedKapa returns the response_json for a matching canned Kapa entry.
// Matches exact query_pattern first, then LIKE pattern (longest match wins).
func LoadCannedKapa(ctx context.Context, pool *pgxpool.Pool, query string) ([]byte, error) {
	const q = `
		SELECT response_json FROM canned_kapa_responses
		WHERE query_pattern = $1 OR $1 LIKE query_pattern
		ORDER BY length(query_pattern) DESC
		LIMIT 1`

	var raw []byte
	err := pool.QueryRow(ctx, q, query).Scan(&raw)
	if err != nil {
		if isNoRows(err) {
			return nil, pgx.ErrNoRows
		}
		return nil, oops.Wrapf(err, "LoadCannedKapa")
	}
	return raw, nil
}

// LoadProfile returns traits JSONB from mock_profiles for a given (entity,
// idType, idValue). Returns pgx.ErrNoRows when absent.
func LoadProfile(ctx context.Context, pool *pgxpool.Pool, entity, idType, idValue string) ([]byte, error) {
	const q = `
		SELECT traits FROM mock_profiles
		WHERE entity = $1 AND id_type = $2 AND id_value = $3
		LIMIT 1`

	var traits []byte
	err := pool.QueryRow(ctx, q, entity, idType, idValue).Scan(&traits)
	if err != nil {
		if isNoRows(err) {
			return nil, pgx.ErrNoRows
		}
		return nil, oops.Wrapf(err, "LoadProfile")
	}
	return traits, nil
}

// InsertMockEmail inserts a row into mock_emails.
func InsertMockEmail(ctx context.Context, pool *pgxpool.Pool, m MockEmailRow) error {
	const q = `
		INSERT INTO mock_emails (trigger_id, to_email, subject, body_markdown, links)
		VALUES ($1, $2, $3, $4, $5)`

	if _, err := pool.Exec(ctx, q,
		m.TriggerID,
		m.ToEmail,
		m.Subject,
		m.BodyMarkdown,
		nullableBytes(m.Links),
	); err != nil {
		return oops.Wrapf(err, "InsertMockEmail")
	}
	return nil
}

// LoadActionTemplate returns the system prompt, user prompt template, and
// output format for the named action template.
// Returns pgx.ErrNoRows when absent.
func LoadActionTemplate(ctx context.Context, pool *pgxpool.Pool, name string) (system, user, format string, err error) {
	const q = `
		SELECT system_prompt, user_prompt_tmpl, output_format
		FROM action_templates
		WHERE name = $1
		LIMIT 1`

	err = pool.QueryRow(ctx, q, name).Scan(&system, &user, &format)
	if err != nil {
		if isNoRows(err) {
			return "", "", "", pgx.ErrNoRows
		}
		return "", "", "", oops.Wrapf(err, "LoadActionTemplate")
	}
	return system, user, format, nil
}

// --- helpers ---

// nullableText converts an empty string to nil so pgx stores NULL rather than "".
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullableBytes converts a nil or zero-length slice to nil so pgx stores NULL.
func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// isNoRows returns true for both pgx.ErrNoRows and any wrapped variant.
func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
