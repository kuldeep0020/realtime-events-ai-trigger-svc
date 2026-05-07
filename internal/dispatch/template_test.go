package dispatch

import (
	"strings"
	"testing"
)

// makeCtx builds a RenderContext with predictable values for tests.
func makeCtx() RenderContext {
	return RenderContext{
		Trait: map[string]any{
			"first_name":       "Sarah",
			"last_name":        "Chen",
			"email":            "sarah.chen@stripe.com",
			"age":              34,
			"propensity_score": 0.87,
			"income_band":      "$200k-$300k",
		},
		Window: map[string]any{
			"event_count":    7,
			"idle_seconds":   22,
			"dominant_suburb": "suburb-1",
			"last_listing": map[string]any{
				"id":       "L112",
				"price":    1500000.0,
				"bedrooms": 3,
			},
			"last_filter": map[string]any{
				"beds_min": 3,
				"suburb":   "suburb-1",
			},
		},
		Realtor: map[string]any{
			"name":  "Priya N.",
			"phone": "+91-98765-43210",
			"hours": "09:00-18:00 IST",
		},
		Outcome: map[string]any{
			"estimated_deal_value": "$1,500,000",
			"urgency_minutes":      "30",
		},
	}
}

// TestRender_SimpleSubstitution verifies a flat {{trait.first_name}} lookup.
func TestRender_SimpleSubstitution(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{
		"headline": "Hello {{trait.first_name}}!",
	}
	got, missing := Render(parsed, makeCtx())
	if got["headline"] != "Hello Sarah!" {
		t.Errorf("headline = %q, want %q", got["headline"], "Hello Sarah!")
	}
	if len(missing) != 0 {
		t.Errorf("expected no missing paths, got %v", missing)
	}
}

// TestRender_MultiOccurrenceInOneString covers "{{trait.first_name}} {{trait.last_name}}".
func TestRender_MultiOccurrenceInOneString(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{
		"name": "{{trait.first_name}} {{trait.last_name}}",
	}
	got, _ := Render(parsed, makeCtx())
	if got["name"] != "Sarah Chen" {
		t.Errorf("name = %q, want %q", got["name"], "Sarah Chen")
	}
}

// TestRender_NestedMapLookup verifies {{window.last_listing.id}} traversal.
func TestRender_NestedMapLookup(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{
		"listing": "{{window.last_listing.id}}",
	}
	got, missing := Render(parsed, makeCtx())
	if got["listing"] != "L112" {
		t.Errorf("listing = %q, want %q", got["listing"], "L112")
	}
	if len(missing) != 0 {
		t.Errorf("expected no missing, got %v", missing)
	}
}

// TestRender_MissingPath verifies "n/a" substitution and missingPaths return.
func TestRender_MissingPath(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{
		"field": "value is {{trait.unknown_key}}",
	}
	got, missing := Render(parsed, makeCtx())
	if got["field"] != "value is n/a" {
		t.Errorf("field = %q, want %q", got["field"], "value is n/a")
	}
	found := false
	for _, p := range missing {
		if p == "trait.unknown_key" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected trait.unknown_key in missingPaths, got %v", missing)
	}
}

// TestRender_FormatHint_Pct verifies :pct multiplies by 100 and appends %.
func TestRender_FormatHint_Pct(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{
		"score": "{{trait.propensity_score:pct}}",
	}
	got, _ := Render(parsed, makeCtx())
	if got["score"] != "87%" {
		t.Errorf("score = %q, want %q", got["score"], "87%")
	}
}

// TestRender_FormatHint_Money verifies :money formats with $ and commas.
func TestRender_FormatHint_Money(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{
		"price": "{{window.last_listing.price:money}}",
	}
	got, _ := Render(parsed, makeCtx())
	if got["price"] != "$1,500,000" {
		t.Errorf("price = %q, want %q", got["price"], "$1,500,000")
	}
}

// TestRender_RecursiveMap verifies placeholders inside nested map values are
// walked and substituted.
func TestRender_RecursiveMap(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{
		"assigned_realtor": map[string]any{
			"name":  "{{realtor.name}}",
			"phone": "{{realtor.phone}}",
		},
	}
	got, _ := Render(parsed, makeCtx())
	realtor, ok := got["assigned_realtor"].(map[string]any)
	if !ok {
		t.Fatalf("assigned_realtor is not map[string]any, got %T", got["assigned_realtor"])
	}
	if realtor["name"] != "Priya N." {
		t.Errorf("realtor.name = %q, want %q", realtor["name"], "Priya N.")
	}
	if realtor["phone"] != "+91-98765-43210" {
		t.Errorf("realtor.phone = %q, want %q", realtor["phone"], "+91-98765-43210")
	}
}

// TestRender_SliceOfStrings verifies that talking_points ([]any of strings)
// are each walked for placeholder substitution.
func TestRender_SliceOfStrings(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{
		"talking_points": []any{
			"Viewed listing {{window.last_listing.id}} priced at {{window.last_listing.price:money}}",
			"Filter beds_min={{window.last_filter.beds_min}}, suburb={{window.last_filter.suburb}}",
			"Propensity score: {{trait.propensity_score:pct}}",
		},
	}
	got, missing := Render(parsed, makeCtx())
	if len(missing) != 0 {
		t.Errorf("unexpected missing paths: %v", missing)
	}
	tps, ok := got["talking_points"].([]any)
	if !ok || len(tps) != 3 {
		t.Fatalf("talking_points is not []any of len 3: %v", got["talking_points"])
	}
	if !strings.Contains(tps[0].(string), "L112") {
		t.Errorf("tp[0] = %q; expected L112", tps[0])
	}
	if !strings.Contains(tps[0].(string), "$1,500,000") {
		t.Errorf("tp[0] = %q; expected $1,500,000", tps[0])
	}
	if !strings.Contains(tps[1].(string), "beds_min=3") {
		t.Errorf("tp[1] = %q; expected beds_min=3", tps[1])
	}
	if !strings.Contains(tps[2].(string), "87%") {
		t.Errorf("tp[2] = %q; expected 87%%", tps[2])
	}
}

// TestRender_SliceOfMaps verifies recursive walk through []any of map[string]any.
func TestRender_SliceOfMaps(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{
		"realtors": []any{
			map[string]any{
				"name":  "{{realtor.name}}",
				"hours": "{{realtor.hours}}",
			},
		},
	}
	got, _ := Render(parsed, makeCtx())
	realtors, ok := got["realtors"].([]any)
	if !ok || len(realtors) != 1 {
		t.Fatalf("realtors shape unexpected: %v", got["realtors"])
	}
	rm, ok := realtors[0].(map[string]any)
	if !ok {
		t.Fatalf("realtors[0] not map[string]any: %T", realtors[0])
	}
	if rm["name"] != "Priya N." {
		t.Errorf("realtors[0].name = %q, want %q", rm["name"], "Priya N.")
	}
	if rm["hours"] != "09:00-18:00 IST" {
		t.Errorf("realtors[0].hours = %q, want %q", rm["hours"], "09:00-18:00 IST")
	}
}

// TestRender_NonStringLeafPassThrough verifies int/bool leaves are unchanged.
func TestRender_NonStringLeafPassThrough(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{
		"count":  42,
		"active": true,
	}
	got, _ := Render(parsed, makeCtx())
	if got["count"] != 42 {
		t.Errorf("count = %v, want 42", got["count"])
	}
	if got["active"] != true {
		t.Errorf("active = %v, want true", got["active"])
	}
}

// TestFormatMoney covers edge cases for the money formatter.
func TestFormatMoney(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input float64
		want  string
	}{
		{0, "$0"},
		{1000, "$1,000"},
		{1500000, "$1,500,000"},
		{999, "$999"},
		{1000000000, "$1,000,000,000"},
	}
	for _, c := range cases {
		got := formatMoney(c.input)
		if got != c.want {
			t.Errorf("formatMoney(%v) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestRender_MissingSection verifies placeholder with unknown section → "n/a".
func TestRender_MissingSection(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{
		"val": "{{unknown_section.foo}}",
	}
	got, missing := Render(parsed, makeCtx())
	if got["val"] != "n/a" {
		t.Errorf("val = %q, want n/a for unknown section", got["val"])
	}
	if len(missing) == 0 {
		t.Error("expected missing path for unknown_section.foo")
	}
}

// TestRender_EmptyContext verifies graceful handling when ctx maps are nil.
func TestRender_EmptyContext(t *testing.T) {
	t.Parallel()
	parsed := map[string]any{
		"greeting": "Hello {{trait.first_name}}",
	}
	got, missing := Render(parsed, RenderContext{})
	if got["greeting"] != "Hello n/a" {
		t.Errorf("greeting = %q, want %q", got["greeting"], "Hello n/a")
	}
	if len(missing) == 0 {
		t.Error("expected trait.first_name in missing paths")
	}
}
