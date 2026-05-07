package rules

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// PersonaConfig is the on-disk shape of §5.1 / §5.2 YAML configs.
type PersonaConfig struct {
	Persona      string         `yaml:"persona" json:"persona"`
	SlackChannel string         `yaml:"slack_channel,omitempty" json:"slack_channel,omitempty"`
	Realtors     []RealtorEntry `yaml:"realtors,omitempty" json:"realtors,omitempty"`
	Rules        []RuleSpec     `yaml:"rules" json:"rules"`
}

// RealtorEntry is the persona-level realtor roster (real-estate persona only).
// Captured here for round-trip but unused by the engine — the dispatcher
// owns realtor selection.
type RealtorEntry struct {
	Name    string   `yaml:"name" json:"name"`
	Phone   string   `yaml:"phone,omitempty" json:"phone,omitempty"`
	Suburbs []string `yaml:"suburbs" json:"suburbs"`
	Hours   string   `yaml:"hours,omitempty" json:"hours,omitempty"`
}

// RuleSpec is the on-disk rule shape: name + when-tree + fire spec.
type RuleSpec struct {
	Name string         `yaml:"name" json:"name"`
	When map[string]any `yaml:"when" json:"when"`
	Fire FireSpecYAML   `yaml:"fire" json:"fire"`
}

// FireSpecYAML mirrors FireSpec but with YAML/JSON tags. We translate to the
// stronger-typed FireSpec at compile time.
type FireSpecYAML struct {
	ActionTemplate  string `yaml:"action_template" json:"action_template"`
	Destination     string `yaml:"destination" json:"destination"`
	CooldownSeconds int    `yaml:"cooldown_seconds" json:"cooldown_seconds"`
}

// LoadPersonaConfigYAML parses a §5 persona config from raw YAML bytes and
// compiles each rule into a runnable form. Rules with malformed When-trees
// are returned as an error rather than silently skipped — the seed CLI
// surfaces these to the operator.
func LoadPersonaConfigYAML(raw []byte) (PersonaConfig, []Rule, error) {
	var cfg PersonaConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return PersonaConfig{}, nil, fmt.Errorf("yaml: %w", err)
	}
	rules, err := compilePersona(cfg)
	if err != nil {
		return cfg, nil, err
	}
	return cfg, rules, nil
}

// LoadPersonaConfigJSON parses a JSON-encoded persona config (used when the
// spec is fetched from Postgres JSONB columns).
func LoadPersonaConfigJSON(raw []byte) (PersonaConfig, []Rule, error) {
	var cfg PersonaConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return PersonaConfig{}, nil, fmt.Errorf("json: %w", err)
	}
	rules, err := compilePersona(cfg)
	if err != nil {
		return cfg, nil, err
	}
	return cfg, rules, nil
}

func compilePersona(cfg PersonaConfig) ([]Rule, error) {
	out := make([]Rule, 0, len(cfg.Rules))
	for i, r := range cfg.Rules {
		expr, err := buildExpr(r.When)
		if err != nil {
			return nil, fmt.Errorf("rule[%d] %q: %w", i, r.Name, err)
		}
		out = append(out, Rule{
			ID:      uuid.New(),
			Name:    r.Name,
			Persona: cfg.Persona,
			When:    expr,
			Fire: FireSpec{
				ActionTemplate: r.Fire.ActionTemplate,
				Destination:    r.Fire.Destination,
				CooldownSecs:   r.Fire.CooldownSeconds,
			},
			Cooldown: time.Duration(r.Fire.CooldownSeconds) * time.Second,
			Enabled:  true,
		})
	}
	return out, nil
}

// CompileRule compiles a single (already-parsed) RuleSpec for a persona.
// Used by the engine when hot-reloading individual rules from Postgres.
func CompileRule(persona string, r RuleSpec) (Rule, error) {
	expr, err := buildExpr(r.When)
	if err != nil {
		return Rule{}, fmt.Errorf("rule %q: %w", r.Name, err)
	}
	return Rule{
		ID:      uuid.New(),
		Name:    r.Name,
		Persona: persona,
		When:    expr,
		Fire: FireSpec{
			ActionTemplate: r.Fire.ActionTemplate,
			Destination:    r.Fire.Destination,
			CooldownSecs:   r.Fire.CooldownSeconds,
		},
		Cooldown: time.Duration(r.Fire.CooldownSeconds) * time.Second,
		Enabled:  true,
	}, nil
}

// --- Expr building ---------------------------------------------------------

const (
	keyAll = "all"
	keyAny = "any"
	keyNot = "not"
)

// buildExpr compiles a parsed when-spec into an Expr tree. The spec is one
// of:
//
//	{"all": [<spec>, <spec>, ...]}
//	{"any": [<spec>, <spec>, ...]}
//	{"not": <spec>}
//	{"window.event_count": {">=": 3}}     # predicate with explicit op
//	{"window.has_event_type": "page"}     # predicate with shorthand string
//	{"window.distinct_paths_at_least": 3} # predicate with shorthand number
//
// A spec map with multiple top-level keys at the same level is treated as
// implicit AND between predicates. Unknown predicate names return an error.
func buildExpr(spec map[string]any) (Expr, error) {
	if len(spec) == 0 {
		return AllOf{}, nil // vacuous truth
	}

	// Logical wrapper at top level: handle exclusively when the only key is
	// all/any/not.
	if len(spec) == 1 {
		for k, v := range spec {
			switch k {
			case keyAll:
				return buildAll(v)
			case keyAny:
				return buildAny(v)
			case keyNot:
				return buildNot(v)
			default:
				return buildPredicate(k, v)
			}
		}
	}

	// Multi-key spec: implicit AND across all keys.
	conj := AllOf{}
	for k, v := range spec {
		switch k {
		case keyAll, keyAny, keyNot:
			return nil, fmt.Errorf("logical key %q must appear alone, not alongside other predicates", k)
		}
		expr, err := buildPredicate(k, v)
		if err != nil {
			return nil, err
		}
		conj.Children = append(conj.Children, expr)
	}
	return conj, nil
}

func buildAll(v any) (Expr, error) {
	items, err := toSpecSlice(v, keyAll)
	if err != nil {
		return nil, err
	}
	out := AllOf{Children: make([]Expr, 0, len(items))}
	for i, item := range items {
		child, err := buildExpr(item)
		if err != nil {
			return nil, fmt.Errorf("all[%d]: %w", i, err)
		}
		out.Children = append(out.Children, child)
	}
	return out, nil
}

func buildAny(v any) (Expr, error) {
	items, err := toSpecSlice(v, keyAny)
	if err != nil {
		return nil, err
	}
	out := AnyOf{Children: make([]Expr, 0, len(items))}
	for i, item := range items {
		child, err := buildExpr(item)
		if err != nil {
			return nil, fmt.Errorf("any[%d]: %w", i, err)
		}
		out.Children = append(out.Children, child)
	}
	return out, nil
}

func buildNot(v any) (Expr, error) {
	m, ok := toSpecMap(v)
	if !ok {
		return nil, fmt.Errorf("not: must be a map, got %T", v)
	}
	child, err := buildExpr(m)
	if err != nil {
		return nil, fmt.Errorf("not: %w", err)
	}
	return Not{Child: child}, nil
}

// buildPredicate compiles a single named predicate. The value `v` is either:
//
//	a map of args                 → passed as-is
//	a scalar (string/number/etc.) → wrapped as {"value": v}
//	a slice                       → wrapped as {"value": v}
func buildPredicate(name string, v any) (Expr, error) {
	spec, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown predicate %q", name)
	}
	args, err := normalizeArgs(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	fn, err := spec.build(args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	p := &Predicate{
		Name:     name,
		Args:     args,
		fn:       fn,
		usesTime: spec.usesTime,
	}
	return p, nil
}

// normalizeArgs turns scalars and slices into a uniform map[string]any with
// {"value": ...} so predicate builders see a consistent shape.
func normalizeArgs(v any) (map[string]any, error) {
	switch x := v.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return x, nil
	case map[any]any:
		// yaml.v3 strict mode usually returns map[string]any, but
		// configurations sometimes round-trip through map[any]any.
		out := make(map[string]any, len(x))
		for k, val := range x {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("non-string key in args: %v", k)
			}
			out[ks] = val
		}
		return out, nil
	default:
		return map[string]any{"value": v}, nil
	}
}

// toSpecSlice expects v to be []any of map[string]any (one per child).
func toSpecSlice(v any, label string) ([]map[string]any, error) {
	arr, ok := v.([]any)
	if !ok {
		// yaml may produce []map[string]any directly.
		if direct, ok := v.([]map[string]any); ok {
			return direct, nil
		}
		return nil, fmt.Errorf("%s: must be a list, got %T", label, v)
	}
	out := make([]map[string]any, 0, len(arr))
	for i, item := range arr {
		m, ok := toSpecMap(item)
		if !ok {
			return nil, fmt.Errorf("%s[%d]: must be a map, got %T", label, i, item)
		}
		out = append(out, m)
	}
	return out, nil
}

func toSpecMap(v any) (map[string]any, bool) {
	switch x := v.(type) {
	case map[string]any:
		return x, true
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = val
		}
		return out, true
	}
	return nil, false
}

// --- JSONB round-trip ------------------------------------------------------

// MarshalSpec renders an Expr back to a generic spec map, suitable for
// writing to Postgres JSONB.
func MarshalSpec(e Expr) (map[string]any, error) {
	switch x := e.(type) {
	case AllOf:
		items := make([]map[string]any, 0, len(x.Children))
		for _, c := range x.Children {
			m, err := MarshalSpec(c)
			if err != nil {
				return nil, err
			}
			items = append(items, m)
		}
		return map[string]any{keyAll: items}, nil
	case AnyOf:
		items := make([]map[string]any, 0, len(x.Children))
		for _, c := range x.Children {
			m, err := MarshalSpec(c)
			if err != nil {
				return nil, err
			}
			items = append(items, m)
		}
		return map[string]any{keyAny: items}, nil
	case Not:
		m, err := MarshalSpec(x.Child)
		if err != nil {
			return nil, err
		}
		return map[string]any{keyNot: m}, nil
	case *Predicate:
		// Round-trip: always emit the args as a map. Callers get back the
		// same shape predicate builders accept.
		args := map[string]any{}
		for k, v := range x.Args {
			args[k] = v
		}
		return map[string]any{x.Name: args}, nil
	}
	return nil, fmt.Errorf("MarshalSpec: unknown Expr type %T", e)
}

// UnmarshalSpec parses a generic spec map (i.e. the same shape MarshalSpec
// emits) back into an Expr tree.
func UnmarshalSpec(spec map[string]any) (Expr, error) {
	return buildExpr(spec)
}
