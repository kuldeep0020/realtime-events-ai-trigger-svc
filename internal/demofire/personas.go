package demofire

import (
	"github.com/rudderlabs/realtime-events-ai-trigger-svc/internal/event"
)

// profileSpec pairs an anonymousId (and optionally userId for rs-self) with
// the visible identify traits the SDK would have already loaded. The firer
// reads the rotation table at script-build time so that count=N is
// deterministic and rehearsable.
type profileSpec struct {
	AnonID        string
	UserID        string         // empty for realestate; equals AnonID for rs-self
	IdentifyTraits map[string]any // what the SDK pre-loaded; see spec §5.3
}

// realestateVariation holds the per-profile variation in suburb, listings,
// filter, and final dwell listing, so each concurrent card looks distinct.
type realestateVariation struct {
	Suburb            string
	Listings          []string // listing IDs viewed (2 or 3 before final dwell)
	FilterBedsMin     int
	FinalDwellListing string
}

// realestateProfileSpecs is the fixed rotation order — do not change the
// ordering; count=N is deterministic and rehearsable.
var realestateProfileSpecs = []profileSpec{
	{
		AnonID: "anon_demo-re-001",
		IdentifyTraits: map[string]any{
			"first_name":      "Sarah",
			"last_name":       "Chen",
			"email":           "sarah.chen@stripe.com",
			"membership_tier": "browse",
		},
	},
	{
		AnonID: "anon_demo-re-002",
		IdentifyTraits: map[string]any{
			"first_name":      "Marcus",
			"last_name":       "Lee",
			"email":           "marcus.lee@figma.com",
			"membership_tier": "browse",
		},
	},
	{
		// ANONYMOUS — no email, no name. traits.known: email predicate fails;
		// realtor_anonymous_high_intent fires instead of realtor_known_high_intent.
		AnonID: "anon_demo-re-003",
		IdentifyTraits: map[string]any{
			"membership_tier": "browse",
		},
	},
	{
		AnonID: "anon_demo-re-004",
		IdentifyTraits: map[string]any{
			"first_name":      "Priya",
			"last_name":       "Sharma",
			"email":           "priya.sharma@plaid.com",
			"membership_tier": "browse",
		},
	},
	{
		AnonID: "anon_demo-re-005",
		IdentifyTraits: map[string]any{
			"first_name":      "David",
			"last_name":       "Martinez",
			"email":           "david.m@anthropic.com",
			"membership_tier": "browse",
		},
	},
	{
		AnonID: "anon_demo-re-006",
		IdentifyTraits: map[string]any{
			"first_name":      "Jordan",
			"last_name":       "Patel",
			"email":           "jordan.p@example.com",
			"membership_tier": "browse",
		},
	},
	{
		AnonID: "anon_demo-re-007",
		IdentifyTraits: map[string]any{
			"first_name":      "Emma",
			"last_name":       "Wilson",
			"email":           "emma.wilson@notion.so",
			"membership_tier": "browse",
		},
	},
	{
		// ANONYMOUS — second anonymous slot for variety in count=4 if ever needed.
		AnonID: "anon_demo-re-008",
		IdentifyTraits: map[string]any{
			"membership_tier": "browse",
		},
	},
}

// realestateVariations mirrors the profile order 1:1 so that
// realestateProfileSpecs[i] is fired with realestateVariations[i].
var realestateVariations = []realestateVariation{
	{Suburb: "suburb-1", Listings: []string{"L101", "L107", "L112"}, FilterBedsMin: 3, FinalDwellListing: "L112"},
	{Suburb: "suburb-1", Listings: []string{"L107", "L112", "L115"}, FilterBedsMin: 4, FinalDwellListing: "L115"},
	{Suburb: "suburb-1", Listings: []string{"L101", "L112", "L118"}, FilterBedsMin: 3, FinalDwellListing: "L118"},
	{Suburb: "suburb-1", Listings: []string{"L107", "L112", "L120"}, FilterBedsMin: 3, FinalDwellListing: "L120"},
	{Suburb: "suburb-3", Listings: []string{"L301", "L307", "L312"}, FilterBedsMin: 4, FinalDwellListing: "L312"},
	{Suburb: "suburb-2", Listings: []string{"L201", "L207"}, FilterBedsMin: 3, FinalDwellListing: "L207"},
	{Suburb: "suburb-2", Listings: []string{"L207", "L209"}, FilterBedsMin: 2, FinalDwellListing: "L209"},
	{Suburb: "suburb-3", Listings: []string{"L301", "L307"}, FilterBedsMin: 4, FinalDwellListing: "L307"},
}

// rsSelfProfileSpecs is the fixed rotation order for the rs-self demo.
var rsSelfProfileSpecs = []profileSpec{
	{
		AnonID: "demo-rs-001",
		UserID: "demo-rs-001",
		IdentifyTraits: map[string]any{
			"first_name": "Alex",
			"last_name":  "Rivera",
			"email":      "alex@acme.io",
			"company":    "Acme",
			"role":       "engineer",
			"plan":       "free",
		},
	},
	{
		AnonID: "demo-rs-002",
		UserID: "demo-rs-002",
		IdentifyTraits: map[string]any{
			"first_name": "Jamie",
			"last_name":  "Kim",
			"email":      "jamie@beacon.dev",
			"company":    "Beacon",
			"role":       "DevOps Lead",
			"plan":       "growth",
		},
	},
	{
		AnonID: "demo-rs-003",
		UserID: "demo-rs-003",
		IdentifyTraits: map[string]any{
			"first_name": "Sam",
			"last_name":  "Patel",
			"email":      "sam@cobaltdata.com",
			"company":    "Cobalt Data",
			"role":       "CTO",
			"plan":       "enterprise",
		},
	},
}

// RealestateScriptForProfile constructs the 8-step (or 7-step for 2-listing
// variations) real-estate script using the provided profileSpec and variation.
// The identify event uses p.IdentifyTraits.
func RealestateScriptForProfile(p profileSpec, v realestateVariation) []ScriptStep {
	return realestateScriptForProfileWithOpts(p, v, defaultOpts())
}

func realestateScriptForProfileWithOpts(p profileSpec, v realestateVariation, opts builderOpts) []ScriptStep {
	anonID := p.AnonID
	userID := p.UserID

	listingsPage := event.Page{
		URL:                    "https://realestate-demo.example/listings",
		Path:                   "/listings",
		Referrer:               "https://google.com",
		Search:                 "",
		Title:                  "All listings",
		InitialReferrer:        "https://google.com",
		InitialReferringDomain: "google.com",
	}
	finalListingPath := "/listings/" + v.FinalDwellListing
	finalPage := event.Page{
		URL:                    "https://realestate-demo.example" + finalListingPath,
		Path:                   finalListingPath,
		Referrer:               "https://realestate-demo.example/listings",
		Search:                 "",
		Title:                  v.FinalDwellListing + " - " + v.Suburb,
		InitialReferrer:        "https://google.com",
		InitialReferringDomain: "google.com",
	}

	ctx := reBaseContextForProfile(p, listingsPage)

	steps := []ScriptStep{
		// t=0: identify with profile traits
		{
			DelayMs: 0,
			Event: newIdentifyEvent(anonID, userID,
				p.IdentifyTraits,
				ctx, opts,
			),
		},
		// t=2s: page /listings
		{
			DelayMs: 2000,
			Event:   newPageEvent(anonID, userID, ctx, opts),
		},
	}

	// Add listing view events for each listing in the variation
	for i, listingID := range v.Listings {
		delay := 3000
		if i > 0 {
			delay = 4000
		}
		steps = append(steps, ScriptStep{
			DelayMs: delay,
			Event: newTrackEvent(anonID, userID, "Listing Viewed",
				map[string]any{
					"listing_id":      listingID,
					"suburb":          v.Suburb,
					"price":           listingPrice(listingID),
					"bedrooms":        v.FilterBedsMin,
					"sq_ft":           2100,
					"listed_days_ago": 8,
				},
				ctx, opts,
			),
		})
	}

	// Filter Applied step
	steps = append(steps, ScriptStep{
		DelayMs: 4000,
		Event: newTrackEvent(anonID, userID, "Filter Applied",
			map[string]any{
				"filter_type":   "search",
				"beds_min":      v.FilterBedsMin,
				"suburb":        v.Suburb,
				"price_min":     1000000,
				"price_max":     1800000,
				"results_count": 24,
			},
			ctx, opts,
		),
	})

	// Dwell page — navigate to the final listing detail page
	steps = append(steps, ScriptStep{
		DelayMs: 3000,
		Event:   newPageEvent(anonID, userID, reBaseContextForProfile(p, finalPage), opts),
	})

	// Final filter tweak on the detail page (idle trigger bait)
	steps = append(steps, ScriptStep{
		DelayMs: 2000,
		Event: newTrackEvent(anonID, userID, "Filter Applied",
			map[string]any{
				"filter_type":   "search",
				"beds_min":      v.FilterBedsMin,
				"suburb":        v.Suburb,
				"price_min":     1200000,
				"price_max":     1500000,
				"results_count": 12,
			},
			reBaseContextForProfile(p, finalPage), opts,
		),
	})

	return steps
}

// listingPrice returns a deterministic listing price for demo purposes.
func listingPrice(listingID string) int {
	// Use a simple hash of the listing ID suffix for variety
	if len(listingID) == 0 {
		return 1200000
	}
	// prices vary by listing range
	switch listingID[0] {
	case 'L':
		if len(listingID) >= 4 {
			c := listingID[1]
			switch c {
			case '1':
				return 1200000 + int(listingID[len(listingID)-1]-'0')*50000
			case '2':
				return 950000 + int(listingID[len(listingID)-1]-'0')*30000
			case '3':
				return 1500000 + int(listingID[len(listingID)-1]-'0')*60000
			}
		}
	}
	return 1350000
}

// reBaseContextForProfile returns the on-the-wire SDK context for a real-estate
// browser event using the given profile's traits.
func reBaseContextForProfile(p profileSpec, page event.Page) event.Context {
	return event.Context{
		Library: event.Library{Name: "analytics-js", Version: "3.5.0"},
		Page:    page,
		Campaign: map[string]string{
			"utm_source":   "google",
			"utm_medium":   "cpc",
			"utm_campaign": "spring-2026-suburb1",
		},
		OS:           event.OS{Name: "macOS", Version: "13.0"},
		Screen:       event.Screen{Width: 1440, Height: 900, Density: 2, InnerWidth: 1440, InnerHeight: 874},
		UserAgent:    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		Locale:       "en-IN",
		Timezone:     "Asia/Kolkata",
		SessionID:    1714978800000,
		SessionStart: false,
		Traits:       p.IdentifyTraits,
	}
}

// RSSelfScriptForProfile constructs the 5-step rs-self script using the
// provided profileSpec. The identify event uses p.IdentifyTraits.
func RSSelfScriptForProfile(p profileSpec) []ScriptStep {
	return rsSelfScriptForProfileWithOpts(p, defaultOpts())
}

func rsSelfScriptForProfileWithOpts(p profileSpec, opts builderOpts) []ScriptStep {
	anonID := p.AnonID
	userID := p.UserID
	if userID == "" {
		userID = anonID // rs-self: identical values per §6.3
	}

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
				p.IdentifyTraits,
				rsBaseContext(dashPage), opts,
			),
		},
		// t=3s: Account Created
		{
			DelayMs: 3000,
			Event: newTrackEvent(anonID, userID, "Account Created",
				map[string]any{
					"plan":   p.IdentifyTraits["plan"],
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
					"destination_type":        "Amplitude",
					"step":                    "credentials_validation",
					"error_code":              "AMP_INVALID_API_KEY",
					"error_message":           "Provided API key was rejected by Amplitude (HTTP 401)",
					"elapsed_seconds_in_step": 134,
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

// ScriptForPersonaIndex returns the canonical script for the persona at the
// given index into the rotation table. Use idx=0 for the primary demo path.
// For "realestate": rotates through realestateProfileSpecs and realestateVariations.
// For "rs-self": rotates through rsSelfProfileSpecs.
func ScriptForPersonaIndex(persona string, idx int) []ScriptStep {
	switch persona {
	case "realestate":
		n := len(realestateProfileSpecs)
		p := realestateProfileSpecs[idx%n]
		v := realestateVariations[idx%len(realestateVariations)]
		return RealestateScriptForProfile(p, v)
	case "rs-self":
		n := len(rsSelfProfileSpecs)
		p := rsSelfProfileSpecs[idx%n]
		return RSSelfScriptForProfile(p)
	default:
		return nil
	}
}

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
		// fresh property snapshot when idle expires.
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
// the persona is unknown. This is a backwards-compatible wrapper over
// ScriptForPersonaIndex(persona, 0).
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
