package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
	"gopkg.in/yaml.v3"
)

// generateConfigRequest is the body shape for POST /api/onboarding/generate-config.
// We deliberately accept arbitrary `answers` (map[string]any) so the wizard can
// evolve its question set without a backend round-trip; only `persona` is
// required.
type generateConfigRequest struct {
	Persona string         `json:"persona"`
	Answers map[string]any `json:"answers"`
}

// generateConfigResponse mirrors what the frontend expects: a YAML config
// string the user can preview and edit, plus the seed source path it came
// from for traceability.
type generateConfigResponse struct {
	Persona     string `json:"persona"`
	Source      string `json:"source"`       // "preset:<name>"
	ConfigYAML  string `json:"config_yaml"`  // the YAML body
	Description string `json:"description"`
}

// handleGenerateConfig returns a PRESET-driven config based on persona and
// applies wizard answers to the YAML before returning it.
//
// If req.Answers is nil or empty the seed YAML is returned verbatim (preserves
// backward compatibility with legacy clients and tests).
//
// NOTE: The wizard's question defaults differ intentionally from the seed
// defaults. For example, the wizard defaults idle_seconds=30 while the seed
// uses 10. On the first Generate click, the GENERATED YAML will reflect the
// wizard's defaults (e.g. idle_seconds: 30). This is intentional — it
// demonstrates that the wizard's answers are being applied.
func (s *Server) handleGenerateConfig(w http.ResponseWriter, r *http.Request) {
	var req generateConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	persona := strings.TrimSpace(strings.ToLower(req.Persona))
	if persona == "" {
		writeError(w, http.StatusBadRequest, "persona is required")
		return
	}

	var (
		seedPath    string
		description string
	)
	switch persona {
	case "realestate":
		seedPath = "persona-configs/realestate.yaml"
		description = "Real-estate listing portal — fires Slack pings to assigned realtors when high-intent visitors abandon a session."
	case "rs-self", "rsself", "rs_self":
		seedPath = "persona-configs/rs-self.yaml"
		description = "RudderStack self-serve onboarding — fires support emails when users get stuck on errors."
		persona = "rs-self"
	default:
		writeError(w, http.StatusBadRequest, "unsupported persona: "+req.Persona)
		return
	}

	if s.seed == nil {
		writeError(w, http.StatusServiceUnavailable, "seed filesystem not configured")
		return
	}
	body, err := s.seed.ReadFile(seedPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read preset config: "+err.Error())
		return
	}

	// Apply wizard answers if present. Empty/nil answers → return seed verbatim.
	if len(req.Answers) > 0 {
		modified, err := applyAnswers(persona, body, req.Answers)
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to apply answers to config: "+err.Error())
			return
		}
		body = modified
	}

	writeJSON(w, http.StatusOK, generateConfigResponse{
		Persona:     persona,
		Source:      "preset:" + seedPath,
		ConfigYAML:  string(body),
		Description: description,
	})
}

// applyAnswers parses the seed YAML, mutates it according to the wizard answers,
// and returns the re-marshalled YAML. The seed YAML is not modified in place.
//
// For realestate persona:
//   - answers.realtors (string) → replaces the realtors array. Each non-empty
//     line: split on "→" (or "->" as fallback), LHS=name, RHS=comma-separated
//     suburbs. Original hours are preserved when the name matches a seed entry.
//   - answers.idle_seconds (number) → replaces the idle_seconds threshold in
//     the realtor_session_abandoned rule.
//   - answers.price_range (string) → display-only; ignored in YAML.
//
// For rs-self persona:
//   - answers.error_events ([]string or []interface{}) → rebuilds the
//     onboarding_errored rule's `any` block. Empty list → rule unchanged.
//   - answers.idle_seconds (number) → replaces the idle_seconds threshold in
//     the onboarding_stuck rule.
//   - answers.help_channel (string) → only "Email" is implemented. "In-app
//     banner" adds a TODO comment near the rule but does not break anything.
func applyAnswers(persona string, seedYAML []byte, answers map[string]any) ([]byte, error) {
	// We parse into a generic map to avoid losing unknown fields during
	// round-trip. yaml.v3's node API would give better formatting preservation
	// but generic map is sufficient for the demo.
	var doc map[string]any
	if err := yaml.Unmarshal(seedYAML, &doc); err != nil {
		return nil, fmt.Errorf("parse seed YAML: %w", err)
	}

	switch persona {
	case "realestate":
		if err := applyRealestateAnswers(doc, answers); err != nil {
			return nil, err
		}
	case "rs-self":
		if err := applyRSSelfAnswers(doc, answers); err != nil {
			return nil, err
		}
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal modified YAML: %w", err)
	}
	return out, nil
}

// applyRealestateAnswers mutates the realestate persona config map in-place.
func applyRealestateAnswers(doc map[string]any, answers map[string]any) error {
	// --- realtors ---
	if rawRealtors, ok := answers["realtors"]; ok {
		if realtorStr, ok := rawRealtors.(string); ok && strings.TrimSpace(realtorStr) != "" {
			// Build a lookup of existing realtor hours by name so we can
			// preserve custom hours for returning entries.
			originalHours := map[string]string{}
			if existing, ok := doc["realtors"].([]any); ok {
				for _, e := range existing {
					if m, ok := e.(map[string]any); ok {
						name, _ := m["name"].(string)
						hours, _ := m["hours"].(string)
						if name != "" && hours != "" {
							originalHours[name] = hours
						}
					}
				}
			}

			var newRealtors []any
			for _, line := range strings.Split(realtorStr, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				// Accept both "→" (U+2192) and "->" as separators.
				var parts []string
				if strings.Contains(line, "→") {
					parts = strings.SplitN(line, "→", 2)
				} else {
					parts = strings.SplitN(line, "->", 2)
				}
				if len(parts) != 2 {
					continue // skip malformed lines
				}
				name := strings.TrimSpace(parts[0])
				suburbStr := strings.TrimSpace(parts[1])
				if name == "" {
					continue
				}

				var suburbs []string
				for _, s := range strings.Split(suburbStr, ",") {
					s = strings.TrimSpace(s)
					if s != "" {
						suburbs = append(suburbs, s)
					}
				}

				hours := "09:00-18:00 IST"
				if h, ok := originalHours[name]; ok {
					hours = h
				}

				entry := map[string]any{
					"name":    name,
					"suburbs": suburbs,
					"hours":   hours,
				}
				newRealtors = append(newRealtors, entry)
			}

			if len(newRealtors) > 0 {
				doc["realtors"] = newRealtors
			}
		}
	}

	// --- idle_seconds ---
	if rawIdle, ok := answers["idle_seconds"]; ok {
		idleSec := toFloat64(rawIdle)
		if idleSec > 0 {
			if err := setRuleIdleSeconds(doc, "realtor_session_abandoned", idleSec); err != nil {
				return fmt.Errorf("set idle_seconds on realtor_session_abandoned: %w", err)
			}
		}
	}

	// price_range is display-only — intentionally not written to YAML.
	return nil
}

// applyRSSelfAnswers mutates the rs-self persona config map in-place.
func applyRSSelfAnswers(doc map[string]any, answers map[string]any) error {
	// --- error_events ---
	if rawEvents, ok := answers["error_events"]; ok {
		events := toStringSlice(rawEvents)
		if len(events) > 0 {
			if err := setOnboardingErroredEvents(doc, events); err != nil {
				return fmt.Errorf("set error_events on onboarding_errored: %w", err)
			}
		}
	}

	// --- idle_seconds ---
	if rawIdle, ok := answers["idle_seconds"]; ok {
		idleSec := toFloat64(rawIdle)
		if idleSec > 0 {
			if err := setRuleIdleSeconds(doc, "onboarding_stuck", idleSec); err != nil {
				return fmt.Errorf("set idle_seconds on onboarding_stuck: %w", err)
			}
		}
	}

	// --- help_channel ---
	if rawChannel, ok := answers["help_channel"]; ok {
		channel, _ := rawChannel.(string)
		if strings.EqualFold(channel, "in-app banner") {
			// In-app banner is not yet implemented in the engine. We mark the
			// rule with a TODO comment via a dedicated key that the engine
			// ignores, so the YAML reflects the user's intent without breaking.
			markInAppBannerTODO(doc)
		}
		// "Email" is the default destination already in the seed; nothing to do.
	}

	return nil
}

// setRuleIdleSeconds finds the named rule in doc["rules"] and replaces the
// window.idle_seconds ">=" threshold with newValue.
func setRuleIdleSeconds(doc map[string]any, ruleName string, newValue float64) error {
	rules, ok := doc["rules"].([]any)
	if !ok {
		return fmt.Errorf("rules is not a list")
	}

	for _, rAny := range rules {
		rule, ok := rAny.(map[string]any)
		if !ok {
			continue
		}
		name, _ := rule["name"].(string)
		if name != ruleName {
			continue
		}

		when, ok := rule["when"].(map[string]any)
		if !ok {
			return fmt.Errorf("rule %q has no when block", ruleName)
		}

		// The idle_seconds predicate lives inside an "all" list.
		if err := setIdleSecondsInAll(when, newValue); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("rule %q not found", ruleName)
}

// setIdleSecondsInAll recurses into "all" lists to find and replace the
// window.idle_seconds ">=" predicate value.
func setIdleSecondsInAll(spec map[string]any, newValue float64) error {
	// Direct predicate on this level.
	if idleSpec, ok := spec["window.idle_seconds"]; ok {
		switch v := idleSpec.(type) {
		case map[string]any:
			v[">="] = newValue
		case map[any]any:
			v[">="] = newValue
		default:
			// Scalar value; replace with operator map.
			spec["window.idle_seconds"] = map[string]any{">=": newValue}
		}
		return nil
	}

	// Recurse into "all" list.
	if allAny, ok := spec["all"]; ok {
		switch list := allAny.(type) {
		case []any:
			for _, item := range list {
				if m, ok := item.(map[string]any); ok {
					if err := setIdleSecondsInAll(m, newValue); err == nil {
						return nil
					}
				}
			}
		case []map[string]any:
			for _, m := range list {
				if err := setIdleSecondsInAll(m, newValue); err == nil {
					return nil
				}
			}
		}
	}

	return fmt.Errorf("window.idle_seconds predicate not found")
}

// setOnboardingErroredEvents rebuilds the onboarding_errored rule's `any` block
// from the given event name list.
func setOnboardingErroredEvents(doc map[string]any, eventNames []string) error {
	rules, ok := doc["rules"].([]any)
	if !ok {
		return fmt.Errorf("rules is not a list")
	}
	for _, rAny := range rules {
		rule, ok := rAny.(map[string]any)
		if !ok {
			continue
		}
		name, _ := rule["name"].(string)
		if name != "onboarding_errored" {
			continue
		}

		when, ok := rule["when"].(map[string]any)
		if !ok {
			return fmt.Errorf("onboarding_errored rule has no when block")
		}

		// Rebuild the `any` list with the selected event names.
		anyList := make([]any, 0, len(eventNames))
		for _, ev := range eventNames {
			anyList = append(anyList, map[string]any{
				"window.has_event_name": ev,
			})
		}
		when["any"] = anyList
		return nil
	}
	return fmt.Errorf("onboarding_errored rule not found")
}

// markInAppBannerTODO adds a _todo marker to the onboarding_errored rule so
// operators can see the pending work. The engine ignores unknown keys.
func markInAppBannerTODO(doc map[string]any) {
	rules, ok := doc["rules"].([]any)
	if !ok {
		return
	}
	for _, rAny := range rules {
		rule, ok := rAny.(map[string]any)
		if !ok {
			continue
		}
		name, _ := rule["name"].(string)
		if name == "onboarding_errored" {
			// TODO: in-app banner not yet implemented
			rule["_todo"] = "in-app banner not yet implemented"
			return
		}
	}
}

// --- helpers ---

// toFloat64 converts numeric answer values from JSON (float64), Go integers,
// or digit-string values (e.g. "6" from a text input).
func toFloat64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return f
		}
		return 0
	}
	return 0
}

// toStringSlice converts a []any or []string to []string.
func toStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// activateConfigRequest accepts either id (existing row) OR persona+config_yaml
// (insert + activate). Mutually exclusive — exactly one path must be specified.
type activateConfigRequest struct {
	ID         string `json:"id,omitempty"`
	Persona    string `json:"persona,omitempty"`
	TenantID   string `json:"tenant_id,omitempty"`
	ConfigYAML string `json:"config_yaml,omitempty"`
}

type activateConfigResponse struct {
	ID           string `json:"id"`
	Active       bool   `json:"active"`
	Persona      string `json:"persona"`
	RulesReplaced int   `json:"rules_replaced,omitempty"`
}

// handleActivateConfig promotes the seeded config for a persona to active=true,
// deactivating all other configs for that persona. No new rows are ever
// inserted — the operation is idempotent.
//
// Accepted request shapes:
//   - {"persona":"realestate"} — promotes the oldest config that has rules.
//   - {"persona":"realestate","config_yaml":"..."} — parses the YAML, replaces
//     the seeded config's rules in-place with the parsed rules, updates
//     config_yaml, then activates. This is the wizard customization path.
//     The engine is notified to reload immediately so the new rules are live
//     within the request lifetime rather than waiting for the 30-second tick.
//   - {"id":"<uuid>"} — legacy path: promotes a specific config by UUID.
func (s *Server) handleActivateConfig(w http.ResponseWriter, r *http.Request) {
	var req activateConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	switch {
	case req.ID != "":
		// Legacy path: promote a specific config by UUID. Used when the caller
		// already knows the exact config row to activate (e.g. admin tooling).
		parsed, err := uuid.Parse(req.ID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id (not a UUID)")
			return
		}

		if s.pool == nil {
			writeError(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}

		var persona string
		if err := s.pool.QueryRow(ctx, `SELECT persona FROM configs WHERE id = $1`, parsed).Scan(&persona); err != nil {
			writeError(w, http.StatusNotFound, "config not found")
			return
		}

		tx, err := s.pool.Begin(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to begin tx: "+err.Error())
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx, `UPDATE configs SET active = FALSE WHERE persona = $1 AND id <> $2`, persona, parsed); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to deactivate prior configs: "+err.Error())
			return
		}
		if _, err := tx.Exec(ctx, `UPDATE configs SET active = TRUE WHERE id = $1`, parsed); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to activate config: "+err.Error())
			return
		}
		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, activateConfigResponse{
			ID:      parsed.String(),
			Active:  true,
			Persona: persona,
		})

	case req.Persona != "":
		// Wizard path. If config_yaml is provided, parse it and replace the
		// seeded config's rules in-place before activating. This is how the
		// wizard's customizations go live in the rules engine.
		//
		// If config_yaml is NOT supplied, fall back to the existing "promote
		// seeded as-is" behavior to preserve backward compatibility.
		if req.ConfigYAML != "" {
			// Validate YAML and rule count before touching the DB, so
			// user-input errors return 400 even when pool is nil.
			ruleSpecs, parseErr := parseRuleSpecsFromYAML(req.ConfigYAML)
			if parseErr != nil {
				writeError(w, http.StatusBadRequest, (&badYAMLError{cause: parseErr}).Error())
				return
			}
			if len(ruleSpecs) == 0 {
				writeError(w, http.StatusBadRequest, "config_yaml must contain at least one rule")
				return
			}

			if s.pool == nil {
				writeError(w, http.StatusServiceUnavailable, "database unavailable")
				return
			}

			seededID, rulesReplaced, err := s.activateWithCustomYAML(ctx, req.Persona, req.ConfigYAML)
			if err != nil {
				var badYAML *badYAMLError
				switch {
				case errors.As(err, &badYAML):
					writeError(w, http.StatusBadRequest, badYAML.Error())
				case errors.Is(err, errNoRules):
					writeError(w, http.StatusBadRequest, "config_yaml must contain at least one rule")
				case errors.Is(err, pgx.ErrNoRows):
					writeError(w, http.StatusNotFound, "no config with rules found for persona: "+req.Persona)
				default:
					writeError(w, http.StatusInternalServerError, "failed to activate config: "+err.Error())
				}
				return
			}

			// Trigger an immediate engine reload so the new rules are live
			// for the demo without waiting for the 30-second periodic tick.
			if s.engineReloader != nil {
				reloadCtx, reloadCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer reloadCancel()
				if err := s.engineReloader(reloadCtx); err != nil {
					// Non-fatal: log and continue. The 30-second periodic reload
					// will pick up the committed rules regardless.
					s.logger.Warn("activate: engine reload failed",
						"err", err,
						"persona", req.Persona,
						"config_id", seededID.String())
				}
			}

			writeJSON(w, http.StatusOK, activateConfigResponse{
				ID:            seededID.String(),
				Active:        true,
				Persona:       req.Persona,
				RulesReplaced: rulesReplaced,
			})
			return
		}

		// No config_yaml: promote the seeded config as-is.
		if s.pool == nil {
			writeError(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		seededID, err := db.ActivatePersonaSeededConfig(ctx, s.pool, req.Persona)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "no config with rules found for persona: "+req.Persona)
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to activate config: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, activateConfigResponse{
			ID:      seededID.String(),
			Active:  true,
			Persona: req.Persona,
		})

	default:
		writeError(w, http.StatusBadRequest, "either id or persona is required")
		return
	}
}

// badYAMLError wraps a YAML parse error so the handler can distinguish it from
// internal server errors and return HTTP 400 with the underlying message.
type badYAMLError struct{ cause error }

func (e *badYAMLError) Error() string { return "invalid config_yaml: " + e.cause.Error() }
func (e *badYAMLError) Unwrap() error { return e.cause }

// errNoRules is returned by activateWithCustomYAML when the parsed YAML
// contains no rules. The handler translates this to HTTP 400.
var errNoRules = errors.New("no rules")

// activateWithCustomYAML parses configYAML, finds the seeded config row for
// persona, replaces its rules in-place with the parsed rules, updates
// config_yaml, then activates it. Returns (configID, rulesCount, error).
//
// The seeded config row is rewritten in-place (no new row created) so the
// engine's foreign-key references to rule IDs are replaced atomically. The
// engine reload that follows this call picks up the new rules via
// loadRulesFromPG which reads from the active config's rules rows.
func (s *Server) activateWithCustomYAML(ctx context.Context, persona, configYAML string) (uuid.UUID, int, error) {
	// Parse the YAML to extract rule specs.
	ruleSpecs, err := parseRuleSpecsFromYAML(configYAML)
	if err != nil {
		return uuid.UUID{}, 0, &badYAMLError{cause: err}
	}

	// Guard: a YAML with no rules would wipe out the existing rule set and
	// leave the engine with nothing to evaluate.
	if len(ruleSpecs) == 0 {
		return uuid.UUID{}, 0, errNoRules
	}

	// Find the seeded config row (oldest config with rules).
	const findQ = `
		SELECT c.id
		FROM configs c
		JOIN rules r ON r.config_id = c.id
		WHERE c.persona = $1 AND r.enabled = TRUE
		ORDER BY c.created_at ASC
		LIMIT 1`

	var seededID uuid.UUID
	if err := s.pool.QueryRow(ctx, findQ, persona).Scan(&seededID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.UUID{}, 0, pgx.ErrNoRows
		}
		return uuid.UUID{}, 0, fmt.Errorf("find seeded config: %w", err)
	}

	// Replace rules + update config_yaml + activate — all in one transaction.
	if err := db.ReplaceConfigRules(ctx, s.pool, seededID, persona, configYAML, ruleSpecs); err != nil {
		return uuid.UUID{}, 0, fmt.Errorf("replace config rules: %w", err)
	}

	return seededID, len(ruleSpecs), nil
}

// parseRuleSpecsFromYAML parses a persona config YAML and returns the rule
// specs as db.RuleSpec values ready for ReplaceConfigRules.
func parseRuleSpecsFromYAML(configYAML string) ([]db.RuleSpec, error) {
	var doc struct {
		Rules []map[string]any `yaml:"rules"`
	}
	if err := yaml.Unmarshal([]byte(configYAML), &doc); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	if len(doc.Rules) == 0 {
		return nil, nil
	}

	out := make([]db.RuleSpec, 0, len(doc.Rules))
	for i, r := range doc.Rules {
		name, _ := r["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("rule[%d] missing name", i)
		}
		out = append(out, db.RuleSpec{
			Name:    name,
			RuleMap: r,
			Enabled: true,
		})
	}
	return out, nil
}
