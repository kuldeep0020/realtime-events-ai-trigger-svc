package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/samber/oops"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/db"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/kapa"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/llm"
)

// LLMRefresher is the subset of llm.Client used by RefreshLiveLLM. The
// LocalAgentClient implements this interface; tests pass a fake.
type LLMRefresher interface {
	Generate(ctx context.Context, templateName string, vars llm.TemplateVars) (llm.ActionResult, error)
}

// KapaRefresher is the subset of kapa.Client used by RefreshLiveKapa.
type KapaRefresher interface {
	Retrieve(ctx context.Context, query string) (kapa.Result, error)
}

// RefreshTarget describes a single (template, persona) pair that should be
// re-rendered via the live LLM. The seed CLI ships a small fixed list per
// persona — keep targets small (~3-5) to bound runtime.
type RefreshTarget struct {
	TemplateName string
	Persona      string
	Variant      string
	Priority     int
}

// DefaultRefreshTargets is the canonical (template, persona) list refreshed
// when `seed --from live` runs without a custom target list. Extended by
// callers if more templates ship.
var DefaultRefreshTargets = []RefreshTarget{
	{TemplateName: llm.TemplateRealestateRealtorPitch, Persona: llm.PersonaRealestate, Variant: "default", Priority: 100},
	{TemplateName: llm.TemplateRSOnboardingStuck, Persona: llm.PersonaRSSelf, Variant: "default", Priority: 100},
}

// RefreshLiveLLM iterates `targets`, loads each template's prompts from
// action_templates, renders them via llm.RenderPrompt against the supplied
// vars, calls the live LLM client, and upserts the result into
// canned_responses. Returns the count of targets refreshed.
//
// The function tolerates per-target failures: a single render or HTTP error
// is logged and skipped so other targets still seed. If you want fail-fast,
// pre-validate targets before calling.
func (s *Seeder) RefreshLiveLLM(
	ctx context.Context,
	client LLMRefresher,
	varsByPersona map[string]llm.TemplateVars,
	targets []RefreshTarget,
	log *slog.Logger,
) (int, error) {
	if client == nil {
		return 0, oops.Errorf("RefreshLiveLLM: client is nil")
	}
	if log == nil {
		log = slog.Default()
	}
	if len(targets) == 0 {
		targets = DefaultRefreshTargets
	}

	var refreshed int
	for _, t := range targets {
		systemTmpl, userTmpl, _, err := db.LoadActionTemplate(ctx, s.pool, t.TemplateName)
		if err != nil {
			log.Warn("RefreshLiveLLM: action_template missing — skipping",
				"template", t.TemplateName, "err", err)
			continue
		}

		// Pull persona-scoped vars; default to a minimal envelope when the
		// caller hasn't pre-rendered them. We require persona at minimum.
		vars := varsByPersona[t.Persona]
		if vars.Persona == "" {
			vars.Persona = t.Persona
		}

		systemRendered, userRendered, err := llm.RenderPrompt(systemTmpl, userTmpl, vars)
		if err != nil {
			log.Warn("RefreshLiveLLM: render failed — skipping",
				"template", t.TemplateName, "err", err)
			continue
		}

		// LocalAgentClient.Generate uses the convention that
		// vars.WindowSnapshotJSON carries the system prompt and
		// vars.FullEventsJSON carries the user prompt. Build a fresh vars
		// envelope so we don't mutate the caller's map entry.
		callVars := vars
		callVars.WindowSnapshotJSON = systemRendered
		callVars.FullEventsJSON = userRendered

		result, err := client.Generate(ctx, t.TemplateName, callVars)
		if err != nil {
			log.Warn("RefreshLiveLLM: LLM call failed — skipping",
				"template", t.TemplateName, "err", err)
			continue
		}

		// Persist the parsed map as the canned row. If parsed is empty
		// (raw text), wrap it in a simple envelope so the canned client
		// can still range over it.
		raw := []byte(result.Raw)
		if len(result.Parsed) > 0 {
			b, err := json.Marshal(result.Parsed)
			if err == nil {
				raw = b
			}
		}
		if len(raw) == 0 {
			log.Warn("RefreshLiveLLM: empty result — skipping",
				"template", t.TemplateName)
			continue
		}

		if err := s.UpsertCannedLLMRow(ctx, t.TemplateName, t.Persona, t.Variant, t.Priority, raw); err != nil {
			return refreshed, oops.With("template", t.TemplateName).Wrap(err)
		}
		refreshed++
		log.Info("RefreshLiveLLM: row updated",
			"template", t.TemplateName, "persona", t.Persona)
	}
	return refreshed, nil
}

// KapaRefreshTarget describes a single canned-kapa entry to refresh by
// hitting the live API for `Query` and storing the response under
// `QueryPattern`. The pattern is what the canned client matches at
// trigger time; the query is what the seed sends to Kapa once.
type KapaRefreshTarget struct {
	QueryPattern string
	Query        string
}

// DefaultKapaRefreshTargets matches the canned_kapa entries shipped in
// seed/canned-responses-hand.yaml.
var DefaultKapaRefreshTargets = []KapaRefreshTarget{
	{
		QueryPattern: "Amplitude API key error %",
		Query:        "Amplitude API key error AMP_INVALID_API_KEY when setting up a destination",
	},
}

// RefreshLiveKapa iterates targets, calls the live Kapa client for each,
// and upserts the response JSON keyed by QueryPattern.
func (s *Seeder) RefreshLiveKapa(
	ctx context.Context,
	client KapaRefresher,
	targets []KapaRefreshTarget,
	log *slog.Logger,
) (int, error) {
	if client == nil {
		return 0, oops.Errorf("RefreshLiveKapa: client is nil")
	}
	if log == nil {
		log = slog.Default()
	}
	if len(targets) == 0 {
		targets = DefaultKapaRefreshTargets
	}

	var refreshed int
	for _, t := range targets {
		res, err := client.Retrieve(ctx, t.Query)
		if err != nil {
			log.Warn("RefreshLiveKapa: lookup failed — skipping",
				"pattern", t.QueryPattern, "err", err)
			continue
		}
		raw, err := json.Marshal(res)
		if err != nil {
			log.Warn("RefreshLiveKapa: marshal failed — skipping",
				"pattern", t.QueryPattern, "err", err)
			continue
		}
		if err := s.UpsertCannedKapaRow(ctx, t.QueryPattern, raw); err != nil {
			return refreshed, fmt.Errorf("upsert kapa pattern %q: %w", t.QueryPattern, err)
		}
		refreshed++
		log.Info("RefreshLiveKapa: row updated", "pattern", t.QueryPattern)
	}
	return refreshed, nil
}
