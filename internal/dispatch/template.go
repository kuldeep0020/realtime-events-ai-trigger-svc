package dispatch

import (
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
)

// placeholderRe matches {{section.path.to.value}} with an optional :hint suffix.
// Groups: (1) full dotted path including section, (2) hint (may be empty).
// Underscores are permitted in every segment (e.g. {{unknown_section.foo}}).
var placeholderRe = regexp.MustCompile(`\{\{([a-z][a-z_]*(?:\.[a-z_]+)*)(?::([a-z]+))?\}\}`)

// RenderContext holds the four data sources available to template placeholders.
// Each map key corresponds to the section name used in {{section.path}} syntax.
type RenderContext struct {
	Trait   map[string]any // "trait"   — activation.ProfileResponse.Data
	Window  map[string]any // "window"  — derived from window.Snapshot
	Realtor map[string]any // "realtor" — selected realtor (name, phone, hours, suburbs)
	Outcome map[string]any // "outcome" — template-specific synthetic values
}

// sectionMap maps the first path segment to the corresponding map in ctx.
func (ctx RenderContext) sectionMap(section string) map[string]any {
	switch section {
	case "trait":
		return ctx.Trait
	case "window":
		return ctx.Window
	case "realtor":
		return ctx.Realtor
	case "outcome":
		return ctx.Outcome
	default:
		return nil
	}
}

// Render walks all string-typed values in parsed and substitutes
// {{section.path}} placeholders with resolved values from ctx.
//
// Path resolution: dot-separated. "window.last_listing.id" looks up
// ctx.Window["last_listing"].(map[string]any)["id"]. If any segment is
// missing or not a map when expected, the whole placeholder is replaced
// with "n/a" and the path is appended to missingPaths with one slog.Warn
// emitted per (path, call).
//
// Format hints: {{trait.propensity_score:pct}} → "87%" for 0.87.
// Supported: :pct (float * 100, 0 decimals), :money ($1,500,000 with commas).
//
// Recursive: walks nested maps and slices. Non-string leaves pass through
// unchanged.
func Render(parsed map[string]any, ctx RenderContext) (rendered map[string]any, missingPaths []string) {
	missing := map[string]bool{}
	out := renderMap(parsed, ctx, missing)
	for path := range missing {
		missingPaths = append(missingPaths, path)
	}
	return out, missingPaths
}

func renderMap(m map[string]any, ctx RenderContext, missing map[string]bool) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = renderValue(v, ctx, missing)
	}
	return out
}

func renderValue(v any, ctx RenderContext, missing map[string]bool) any {
	switch x := v.(type) {
	case string:
		return renderString(x, ctx, missing)
	case map[string]any:
		return renderMap(x, ctx, missing)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = renderValue(item, ctx, missing)
		}
		return out
	case []map[string]any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = renderMap(item, ctx, missing)
		}
		return out
	default:
		return v
	}
}

// renderString replaces all placeholders in s using ctx. Each placeholder
// is fully resolved or replaced with "n/a".
func renderString(s string, ctx RenderContext, missing map[string]bool) string {
	return placeholderRe.ReplaceAllStringFunc(s, func(match string) string {
		groups := placeholderRe.FindStringSubmatch(match)
		// groups[1] = full path (e.g. "window.last_listing.id")
		// groups[2] = hint (may be empty)
		fullPath := groups[1]
		hint := groups[2]

		val, found := resolvePath(fullPath, ctx)
		if !found {
			if !missing[fullPath] {
				missing[fullPath] = true
				slog.Warn("dispatch/template: missing placeholder path", "path", fullPath)
			}
			return "n/a"
		}
		return formatValue(val, hint)
	})
}

// resolvePath resolves a dot-separated path like "window.last_listing.id"
// against ctx. The first segment selects the section map; subsequent segments
// traverse nested map[string]any values.
func resolvePath(fullPath string, ctx RenderContext) (any, bool) {
	parts := strings.SplitN(fullPath, ".", 2)
	if len(parts) < 2 {
		// No dot — just a section name with no key; not valid.
		return nil, false
	}
	section := parts[0]
	rest := parts[1]

	m := ctx.sectionMap(section)
	if m == nil {
		return nil, false
	}

	// Walk remaining segments.
	segments := strings.Split(rest, ".")
	var cur any = (map[string]any)(m)
	for _, seg := range segments {
		cm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		val, exists := cm[seg]
		if !exists {
			return nil, false
		}
		cur = val
	}
	if cur == nil {
		return nil, false
	}
	return cur, true
}

// formatValue stringifies val applying the optional hint.
//
// Supported hints:
//   - "pct"   — treat as float64, multiply by 100, format with 0 decimals + "%"
//   - "money" — treat as numeric, format with "$" prefix and thousands commas
//
// No hint: fmt.Sprintf("%v", val).
func formatValue(val any, hint string) string {
	switch hint {
	case "pct":
		f := toFloat64(val)
		return fmt.Sprintf("%.0f%%", f*100)
	case "money":
		f := toFloat64(val)
		return formatMoney(f)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// toFloat64 coerces common numeric types to float64. Returns 0 on failure.
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// formatMoney formats a number as "$1,500,000" (integer with $ prefix and
// thousands commas). Fractional cents are dropped.
func formatMoney(f float64) string {
	n := int64(math.Round(f))
	negative := n < 0
	if negative {
		n = -n
	}
	// Build digits with commas.
	s := fmt.Sprintf("%d", n)
	// Insert commas every 3 digits from right.
	var buf strings.Builder
	rem := len(s) % 3
	for i, ch := range s {
		if i > 0 && (i-rem)%3 == 0 {
			buf.WriteByte(',')
		}
		buf.WriteRune(ch)
	}
	result := "$" + buf.String()
	if negative {
		result = "-" + result
	}
	return result
}
