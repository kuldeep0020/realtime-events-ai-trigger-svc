package demofire

import (
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/event"
)

// RealestateScript returns the 8-event browser-channel sequence for the
// real-estate demo persona (§6.2). Total wall-clock time ~32s. Triggers
// `realtor_session_abandoned` after 10s idle on /listings/L112.
//
// The DelayMs values are RELATIVE to the previous step; the Firer maps
// them onto absolute time at firing time.
func RealestateScript() []ScriptStep {
	return realestateScriptWithOpts(defaultOpts())
}

func realestateScriptWithOpts(opts builderOpts) []ScriptStep {
	const (
		anonID = reAnonID
		userID = ""
	)

	listingsPage := event.Page{
		URL:                    "https://realestate-demo.example/listings",
		Path:                   "/listings",
		Referrer:               "https://google.com",
		Search:                 "",
		Title:                  "All listings",
		InitialReferrer:        "https://google.com",
		InitialReferringDomain: "google.com",
	}
	l112Page := event.Page{
		URL:                    "https://realestate-demo.example/listings/L112",
		Path:                   "/listings/L112",
		Referrer:               "https://realestate-demo.example/listings",
		Search:                 "",
		Title:                  "L112 - Park Avenue, Suburb 1",
		InitialReferrer:        "https://google.com",
		InitialReferringDomain: "google.com",
	}

	return []ScriptStep{
		// t=0: identify with membership_tier="browse"
		{
			DelayMs: 0,
			Event: newIdentifyEvent(anonID, userID,
				map[string]any{"membership_tier": "browse"},
				reBaseContext(listingsPage), opts,
			),
		},
		// t=2s: page /listings
		{
			DelayMs: 2000,
			Event:   newPageEvent(anonID, userID, reBaseContext(listingsPage), opts),
		},
		// t=5s: Listing Viewed L101
		{
			DelayMs: 3000,
			Event: newTrackEvent(anonID, userID, "Listing Viewed",
				map[string]any{
					"listing_id":      "L101",
					"suburb":          "suburb-1",
					"price":           1200000,
					"bedrooms":        3,
					"sq_ft":           2100,
					"year_built":      2015,
					"agent":           "Priya N.",
					"listed_days_ago": 12,
				},
				reBaseContext(listingsPage), opts,
			),
		},
		// t=9s: Filter Applied
		{
			DelayMs: 4000,
			Event: newTrackEvent(anonID, userID, "Filter Applied",
				map[string]any{
					"filter_type":   "search",
					"beds_min":      3,
					"suburb":        "suburb-1",
					"price_min":     1000000,
					"price_max":     1800000,
					"results_count": 24,
				},
				reBaseContext(listingsPage), opts,
			),
		},
		// t=13s: Listing Viewed L107
		{
			DelayMs: 4000,
			Event: newTrackEvent(anonID, userID, "Listing Viewed",
				map[string]any{
					"listing_id": "L107",
					"suburb":     "suburb-1",
					"price":      1500000,
					"bedrooms":   4,
					"sq_ft":      2400,
					"amenities":  []string{"pool", "garage_2", "garden"},
				},
				reBaseContext(listingsPage), opts,
			),
		},
		// t=17s: Listing Viewed L112
		{
			DelayMs: 4000,
			Event: newTrackEvent(anonID, userID, "Listing Viewed",
				map[string]any{
					"listing_id":      "L112",
					"suburb":          "suburb-1",
					"price":           1350000,
					"bedrooms":        3,
					"sq_ft":           2200,
					"listed_days_ago": 4,
					"view_count":      125,
				},
				reBaseContext(listingsPage), opts,
			),
		},
		// t=20s: page /listings/L112 (dwell start)
		{
			DelayMs: 3000,
			Event:   newPageEvent(anonID, userID, reBaseContext(l112Page), opts),
		},
		// t=22s+10s idle: Filter Applied final tweak so the trigger has a
		// fresh property snapshot when idle expires (the 8th event on §6.2's
		// table is the implicit idle expiry; we materialise it as a final
		// short event so the consumer logs hit a meaningful event count
		// before idle detection triggers).
		{
			DelayMs: 2000,
			Event: newTrackEvent(anonID, userID, "Filter Applied",
				map[string]any{
					"filter_type":   "search",
					"beds_min":      3,
					"suburb":        "suburb-1",
					"price_min":     1200000,
					"price_max":     1500000,
					"results_count": 12,
				},
				reBaseContext(l112Page), opts,
			),
		},
	}
}

// RSSelfScript returns the 5-event browser-channel sequence for the rs-self
// demo persona (§6.3). Total wall-clock time ~12s. Triggers
// `onboarding_errored` immediately after the Destination Setup Error.
func RSSelfScript() []ScriptStep {
	return rsSelfScriptWithOpts(defaultOpts())
}

func rsSelfScriptWithOpts(opts builderOpts) []ScriptStep {
	const (
		anonID = rsAnonID
		userID = rsAnonID // §6.3: identical values
	)

	signupPage := event.Page{
		URL:   "https://app.rudderstack.com/signup",
		Path:  "/signup",
		Title: "RudderStack — Sign up",
	}
	setupPage := event.Page{
		URL:   "https://app.rudderstack.com/setup/destinations",
		Path:  "/setup/destinations",
		Title: "Set up your destinations",
	}
	dashPage := event.Page{
		URL:   "https://app.rudderstack.com/dashboard",
		Path:  "/dashboard",
		Title: "Dashboard",
	}

	return []ScriptStep{
		// t=0: identify
		{
			DelayMs: 0,
			Event: newIdentifyEvent(anonID, userID,
				map[string]any{
					"plan":    "free",
					"company": "Acme",
					"role":    "engineer",
				},
				rsBaseContext(dashPage), opts,
			),
		},
		// t=3s: Account Created
		{
			DelayMs: 3000,
			Event: newTrackEvent(anonID, userID, "Account Created",
				map[string]any{
					"plan":   "free",
					"source": "signup_page",
				},
				rsBaseContext(signupPage), opts,
			),
		},
		// t=6s: Source Created
		{
			DelayMs: 3000,
			Event: newTrackEvent(anonID, userID, "Source Created",
				map[string]any{
					"source_type":              "javascript",
					"source_name":              "Production Web",
					"elapsed_seconds_in_setup": 87,
				},
				rsBaseContext(setupPage), opts,
			),
		},
		// t=10s: Destination Setup Error
		{
			DelayMs: 4000,
			Event: newTrackEvent(anonID, userID, "Destination Setup Error",
				map[string]any{
					"destination_type":         "Amplitude",
					"step":                     "credentials_validation",
					"error_code":               "AMP_INVALID_API_KEY",
					"error_message":            "Provided API key was rejected by Amplitude (HTTP 401)",
					"elapsed_seconds_in_step":  134,
				},
				rsBaseContext(setupPage), opts,
			),
		},
		// t=12s: page /setup/destinations (the user lingers on the error screen)
		{
			DelayMs: 2000,
			Event:   newPageEvent(anonID, userID, rsBaseContext(setupPage), opts),
		},
	}
}

// PersonaWriteKey returns the canonical writeKey for `persona`. Sourced from
// §0 of the design doc; if no override is supplied via flag, the firer
// uses these.
func PersonaWriteKey(persona string) string {
	switch persona {
	case "realestate":
		return "3DNyjJW7sRSqftUb1UQuMJdxlFw"
	case "rs-self":
		return "3DNyveG1sfuVHAV598ESyJza3i3"
	default:
		return ""
	}
}

// ScriptForPersona returns the canonical script for `persona`, or nil if
// the persona is unknown.
func ScriptForPersona(persona string) []ScriptStep {
	switch persona {
	case "realestate":
		return RealestateScript()
	case "rs-self":
		return RSSelfScript()
	default:
		return nil
	}
}
