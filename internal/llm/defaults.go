package llm

import "encoding/json"

// hardcodedDefault returns a baked-in ActionResult for a known template name.
// Each demo persona has at least one default so the dispatcher always has
// something renderable, even with a completely empty Postgres.
//
// The default registry is intentionally conservative — it embeds neutral
// copy that is safe to ship to Slack / mock-email even without enrichment.
// Per-call vars are NOT interpolated into the default Raw/Parsed payload to
// avoid leaking PII into a generic fallback. Callers may render real values
// later if they choose.
//
// reason is recorded on DegradedReason for observability.
func hardcodedDefault(templateName string, vars TemplateVars, reason string) ActionResult {
	if def, ok := defaultRegistry[templateName]; ok {
		raw, _ := json.Marshal(def)
		return ActionResult{
			Template: templateName,
			Raw:      string(raw),
			Parsed:   def,
			Source:   "fallback",
			DegradedReason: nonEmpty(reason,
				"hardcoded default used (no canned row matched)"),
		}
	}

	// Unknown template — surface a minimal, safe envelope rather than
	// erroring. The dispatcher logs the DegradedReason; the UI may render
	// the headline plus a "we couldn't generate a tailored action" notice.
	generic := map[string]any{
		"headline":  "An action was triggered, but no template was wired",
		"persona":   vars.Persona,
		"template":  templateName,
		"is_uncertain": true,
	}
	raw, _ := json.Marshal(generic)
	return ActionResult{
		Template:       templateName,
		Raw:            string(raw),
		Parsed:         generic,
		Source:         "fallback",
		DegradedReason: nonEmpty(reason, "unknown template name; generic envelope used"),
	}
}

// defaultRegistry holds the baked-in payload for every shipped template. The
// shapes mirror the canned-responses-hand.yaml content so downstream UI
// rendering paths work identically whether the data comes from PG or here.
var defaultRegistry = map[string]map[string]any{
	TemplateRealestateRealtorPitch: {
		"headline": "High-intent visitor abandoned a session",
		"talking_points": []string{
			"Visitor browsed multiple listings in a short span",
			"Filters indicate a clear intent (bedrooms, budget, suburb)",
			"Idle for >10s on a detail page is the strongest engagement signal",
		},
		"best_cta":         "Call within 30 minutes; lead with the listing they spent the most time on",
		"urgency":          "high",
		"assigned_realtor": "On-call realtor",
	},
	TemplateRealestateRealtorAnonymous: {
		"headline": "Anonymous high-intent visitor abandoned a session",
		"talking_points": []string{
			"Visitor browsed multiple listings without identifying themselves",
			"Filter activity shows clear purchase intent",
			"Standby realtor can engage via in-app chat or follow-up ad",
		},
		"best_cta":                    "Trigger an in-app prompt or retargeting sequence within 15 minutes",
		"urgency":                     "medium",
		"assigned_realtor_on_standby": "On-call realtor",
	},
	TemplateRSOnboardingStuck: {
		"subject": "Stuck during onboarding? Here's a quick path forward",
		"body_markdown": "Hi there,\n\nWe noticed you ran into an issue while setting up " +
			"your destination. The two most common causes are:\n\n" +
			"1. The credentials are correct but pointing to a different project.\n" +
			"2. The credentials lack ingestion permissions.\n\n" +
			"Re-paste the ingestion credentials from the same project and re-test " +
			"the destination. If the error persists, reply with the request ID " +
			"from the destination error log and we'll get you unstuck same-day.\n\n" +
			"— RudderStack",
		"doc_links": []map[string]string{
			{
				"title": "RudderStack docs",
				"url":   "https://www.rudderstack.com/docs/",
			},
		},
		"next_step_cta": "Reply with your error log request ID for fast-track help",
	},
	TemplateRSDestinationError: {
		"subject": "Destination setup error — here's how to resolve it",
		"body_markdown": "Hi there,\n\nWe detected an error on one of your destinations. " +
			"Common causes include invalid API credentials or missing permissions.\n\n" +
			"Steps to fix:\n\n" +
			"1. Open the destination settings and re-enter your credentials.\n" +
			"2. Confirm the credentials have the required ingestion permissions.\n" +
			"3. Click \"Test connection\" — if it passes, you're all set.\n\n" +
			"If the error persists, share the error code from the destination log " +
			"and we'll help you resolve it same-day.\n\n" +
			"— RudderStack",
		"doc_links": []map[string]string{
			{
				"title": "Destination setup guide",
				"url":   "https://www.rudderstack.com/docs/destinations/",
			},
		},
		"next_step_cta": "Reply with your destination error code for fast-track support",
	},
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
