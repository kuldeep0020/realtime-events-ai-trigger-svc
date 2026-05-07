package main

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/db"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/llm"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/rules"
)

// loadRulesFromPG fetches active rules from PG for both personas and
// compiles them. Used both at boot and by the rule reloader.
func (rt *runtime) loadRulesFromPG(ctx context.Context) ([]rules.Rule, error) {
	out := make([]rules.Rule, 0, 8)
	for _, persona := range []string{llm.PersonaRealestate, llm.PersonaRSSelf} {
		rows, err := db.LoadActiveRules(ctx, rt.pool, persona)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			var spec map[string]any
			if err := json.Unmarshal(r.Spec, &spec); err != nil {
				rt.log.Warn("serve: unparseable rule spec — skipping",
					"rule", r.Name, "err", err)
				continue
			}
			rs, err := compileRuleFromSpec(persona, r.ID, r.Name, spec)
			if err != nil {
				rt.log.Warn("serve: compile rule failed — skipping",
					"rule", r.Name, "err", err)
				continue
			}
			out = append(out, rs)
		}
	}
	return out, nil
}

// compileRuleFromSpec compiles a single PG-stored rule spec via the rules
// package's exported builder. The spec map's shape mirrors the YAML rule
// body: {name, when, fire}.
func compileRuleFromSpec(persona string, id uuid.UUID, name string, spec map[string]any) (rules.Rule, error) {
	when, _ := spec["when"].(map[string]any)
	fire, _ := spec["fire"].(map[string]any)
	rspec := rules.RuleSpec{Name: name, When: when}
	if fire != nil {
		rspec.Fire.ActionTemplate = strFromMap(fire, "action_template")
		rspec.Fire.Destination = strFromMap(fire, "destination")
		// cooldown_seconds may be float64 (json) or int.
		if v, ok := fire["cooldown_seconds"]; ok {
			rspec.Fire.CooldownSeconds = toInt(v)
		}
	}
	r, err := rules.CompileRule(persona, rspec)
	if err != nil {
		return rules.Rule{}, err
	}
	r.ID = id
	return r, nil
}

func strFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float32:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}
