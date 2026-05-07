package dispatch

import (
	"log/slog"
	"time"

	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/rules"
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/window"
)

// BuildWindowMap derives the "window" section of a RenderContext from a
// Snapshot. The returned map is safe to share — the snap fields are already
// deep-copied by the snapshot mechanism.
//
// Keys:
//
//	event_count, idle_seconds, dominant_suburb, session_minutes,
//	last_listing  (= snap.LastListingProps),
//	last_filter   (= snap.LastFilterProps),
//	last_listing_id (shorthand string; avoids nested lookup in templates),
//	last_error    (= snap.LastErrorEvent.Properties).
func BuildWindowMap(snap window.Snapshot, now time.Time) map[string]any {
	idle := int(snap.IdleFor(now).Seconds())
	sessionMins := 0
	if !snap.FirstSeen.IsZero() && !now.IsZero() {
		sessionMins = int(now.Sub(snap.FirstSeen).Minutes())
	}

	lastListingID := ""
	if snap.LastListingProps != nil {
		if id, ok := snap.LastListingProps["listing_id"].(string); ok {
			lastListingID = id
		}
	}

	m := map[string]any{
		"event_count":     snap.EventCount,
		"idle_seconds":    idle,
		"dominant_suburb": snap.DominantSuburb,
		"session_minutes": sessionMins,
		"last_listing":    snap.LastListingProps,
		"last_filter":     snap.LastFilterProps,
		"last_listing_id": lastListingID,
	}

	// Expose last_error as a sub-map for templates that reference
	// {{window.last_error.error_code}} etc.
	if snap.LastErrorEvent.Properties != nil {
		errMap := make(map[string]any, len(snap.LastErrorEvent.Properties))
		for k, v := range snap.LastErrorEvent.Properties {
			errMap[k] = v
		}
		m["last_error"] = errMap
	}

	return m
}

// SelectRealtor picks the realtor whose suburbs list contains dominantSuburb.
// Falls back to the first realtor in the list when no match is found (logs
// a warning). Returns nil if the list is empty.
func SelectRealtor(realtors []rules.RealtorEntry, dominantSuburb string) *rules.RealtorEntry {
	if len(realtors) == 0 {
		return nil
	}
	for i, r := range realtors {
		for _, s := range r.Suburbs {
			if s == dominantSuburb {
				return &realtors[i]
			}
		}
	}
	slog.Warn("dispatch: no realtor matched dominant suburb; using first realtor",
		"dominant_suburb", dominantSuburb,
		"realtors", len(realtors),
	)
	return &realtors[0]
}

// RealtorToMap converts a RealtorEntry to the map[string]any shape used in
// RenderContext.Realtor and in the SSE enriched_realtor field.
func RealtorToMap(r *rules.RealtorEntry) map[string]any {
	if r == nil {
		return map[string]any{}
	}
	return map[string]any{
		"name":    r.Name,
		"phone":   r.Phone,
		"hours":   r.Hours,
		"suburbs": r.Suburbs,
	}
}

// BuildOutcomeMap synthesises the template-specific outcome values used in
// RenderContext.Outcome. The template name drives which keys are populated.
func BuildOutcomeMap(templateName string, windowMap map[string]any) map[string]any {
	switch templateName {
	case "realestate_realtor_pitch":
		dealValue := "$0"
		if ll, ok := windowMap["last_listing"].(map[string]any); ok {
			if price, ok := ll["price"]; ok {
				dealValue = formatMoney(toFloat64(price))
			}
		}
		return map[string]any{
			"estimated_deal_value": dealValue,
			"urgency_minutes":      "30",
		}
	case "realestate_realtor_anonymous":
		return map[string]any{
			"recommended_action": "Trigger in-app banner offering instant tour booking",
			"urgency_minutes":    "60",
		}
	case "rs_destination_error", "rs_onboarding_stuck":
		return map[string]any{
			"fix_eta_minutes": "5",
		}
	default:
		return map[string]any{}
	}
}
