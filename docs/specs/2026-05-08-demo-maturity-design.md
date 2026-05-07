# Demo Maturity — Design Spec

**Date**: 2026-05-08
**Author**: Kuldeep + Claude (orchestrator)
**Status**: pending user approval; targeted for autonomous subagent execution overnight
**Repo**: `/Users/kumar/workspace/realtime-ai-trigger-svc`
**Audience for the demo**: leadership panel of 6 (3 product, 3 technical) plus broader engineering audience

---

## 1. Goals

The current demo works end-to-end but feels thin: a single anonymous visitor, a single trigger, a Slack message that names no one. This spec lifts the demo from "engineer's working prototype" to "product leadership would buy this". The constraint throughout is **stability over novelty** — every change must preserve the existing happy path.

| # | Goal | Measurable outcome |
|---|---|---|
| G1 | Demo feels like multiple real users, not one repeat fire | Fire button supports 1, 2, or 3 concurrent sessions per click; each session has its own `anonymousId`, identify traits, and trigger fire |
| G2 | Audience can read each session card without rushing | Pacing slider (0.5x / 1x / 2x); default 1x stretches realestate to ~32s, 0.5x to ~64s |
| G3 | Slack message reads like an actionable lead, not a debug log | Visitor's name, email, age, occupation, propensity score, recommended action, assigned realtor with contact info — all populated from mock activation data |
| G4 | Wizard reframes around outcomes, not personas | First step shows 4 outcome cards (e.g. "Win back high-intent anonymous visitors"); persona is implied by selection |
| G5 | Anonymous visitors trigger a different action than known visitors | Two distinct realestate rules: `realtor_known_high_intent` (rich Slack with name/contact) and `realtor_anonymous_high_intent` (in-app banner CTA + standby realtor ping); demo shows both flows in one Fire click |
| G6 | Business value is legible on screen during the demo | Outcome banner above dashboard renders the *business* outcome of each trigger (assigned realtor, est. deal value, response window). ROI tile in header shows session totals |
| G7 | Implementation does not introduce regressions | All existing tests pass; existing single-session demo continues to work unchanged |

## 2. Non-goals

Explicitly out of scope so subagents do not drift:

- **No real LLM calls at trigger time.** Canned responses with hand-templated placeholders. The Bedrock key the user provided is available as a fallback if a subagent decides hand-templating is insufficient, but the recommended path is templating.
- **No real Activation API.** Mock-only, backed by the expanded `mock_profiles.yaml`.
- **No new SQL migrations.** The schema in §4.1 of the rev-2 design doc is unchanged. Only seed data + (limited) Go struct fields change.
- **No multi-rule wizard.** One rule per persona at activation time. Adding a second rule requires re-running the wizard (existing limitation).
- **No replay-on-history button** ("test this rule on past data"). Cool but heavy; defer.
- **No cross-window predicates.** Rule eval still operates on a single visitor's window.
- **No new global theme / visual redesign.** Reuse the existing shadcn/ui palette + Tailwind classes already in `frontend/components/ui/`.
- **No collapse-on-trigger card behavior** (was F1 in brainstorming). If 3 concurrent cards look cluttered, the OutcomeBanner draws the eye well enough — we iterate post-demo if needed.
- **No velocity / dwell-seconds predicates.** The new `not` operator + existing predicates cover the four use-case cards.

## 3. Architecture summary

### 3.1 What's already in place (verified)

- **`not` operator is already implemented** in both the rules engine (`internal/rules/expr.go::Not`) and the YAML loader (`internal/rules/loader.go::buildNot`). No engine work is required for C1.
- `traits.known` / `traits.value` predicates exist (`internal/rules/predicates.go`) and read from the window's `Traits` map (populated by `identify` events).
- The rules engine already supports `all`/`any`/`not` nesting recursively via `buildExpr`.
- The firer (`internal/demofire/firer.go`) re-stamps `OriginalTimestamp` and `SentAt` per send, and the window apply uses `receivedAt` as authoritative — so spreading events over wall-clock time will correctly defer trigger fire to the real idle moment (no premature-fire risk regression).
- The activation client (`internal/activation/`) already supports `id_type=anonymous_id` and `user_id` lookups and returns `data: {}` on miss.

### 3.2 What's changing

```text
NEW DATA              NEW BACKEND CODE             NEW FRONTEND CODE
==========            ================             =================
mock_profiles.yaml    rules.window_has_user_id     UseCaseGallery.tsx (wizard step 1)
  (8 RE + 3 RS)       window.LastListingProps      OutcomeBanner.tsx
                      window.LastFilterProps       ROITile.tsx
canned-resp-hand.yaml window.DominantSuburb        Controller.tsx (count + speed picker)
  (templated +        dispatch.template.go         (no NEW dashboard layout —
   anonymous variant)   (placeholder fill)          banner + tile slot in)
                      demofire.profile rotation
realestate.yaml       demofire.RunConcurrent
  (new rule:          api.handlers_demo (count,
   anon high-intent)    speed params)
realtor-roster        runtime_dispatch
  (in realestate.yaml   (realtor selection by
   under realtors)       suburb)
```

### 3.3 Trigger pipeline with template fill

The pipeline today: window match → cooldown → enricher (activation API + profile) → LLM client (canned) → dispatcher (Slack/email).

After this change, between "LLM client" and "dispatcher" we add a **render context** that template-fills placeholders into the canned response's string fields:

```text
canned response (raw_json with {{trait.first_name}}, {{window.last_listing.id}}, {{realtor.name}})
        +
RenderContext {
  Trait:   activation.ProfileResponse.Data,
  Window:  derivedFromSnapshot (DominantSuburb, LastListingProps, LastFilterProps, IdleSeconds, EventCount),
  Realtor: persona-config.realtors[selected by window.DominantSuburb],
}
        ↓ template-fill (regex {{path}} substitution; missing → "n/a", logged)
        ↓
final action JSON → dispatcher
```

This addition lives in `internal/dispatch/template.go` (new file). The dispatcher reads the canned response, runs template-fill, then formats Slack blocks / email body as today.

**Failure mode**: a missing placeholder (e.g., `{{trait.first_name}}` when activation returned empty) renders as `"n/a"` and is logged at WARN level. The trigger still fires — the demo prefers degraded output over a 500.

## 4. Data model changes

### 4.1 SQL — none

No migration. The existing `triggers.enriched_traits` JSONB column already holds the activation API result.

### 4.2 `seed/mock_profiles.yaml` — full replacement

The new file holds **8 realestate profiles** + **3 rs-self profiles**. Realestate IDs follow `anon_demo-re-NNN`. The firer rotates through them in fixed order so that count=N is deterministic and rehearsable.

**Realestate rotation order** (FIXED — do not change):

| # | id_value | Type | first_name | last_name | email | age | occupation | employer | income_band | family_status | propensity_score | preferred_suburbs | preferred_bedrooms | inferred_intent |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | `anon_demo-re-001` | known | Sarah | Chen | sarah.chen@stripe.com | 34 | Senior Product Manager | Stripe | $200k-$300k | married, 1 child | 0.87 | [suburb-1, suburb-3] | 3 | purchase_within_30d |
| 2 | `anon_demo-re-002` | known | Marcus | Lee | marcus.lee@figma.com | 41 | Engineering Director | Figma | $300k-$500k | married, 2 children | 0.79 | [suburb-1, suburb-2] | 4 | purchase_within_60d |
| 3 | `anon_demo-re-003` | **anonymous** | (no entry in YAML — activation returns empty) | | | | | | | | | | | |
| 4 | `anon_demo-re-004` | known | Priya | Sharma | priya.sharma@plaid.com | 38 | VP of Marketing | Plaid | $250k-$400k | married, no kids | 0.82 | [suburb-1] | 3 | purchase_within_30d |
| 5 | `anon_demo-re-005` | known | David | Martinez | david.m@anthropic.com | 47 | Staff Software Engineer | Anthropic | $400k-$600k | married, 3 children | 0.91 | [suburb-3, suburb-2] | 4 | purchase_within_30d |
| 6 | `anon_demo-re-006` | known (partial — has email but no demographics, simulating a CRM with sparse data) | Jordan | Patel | jordan.p@example.com | (omit) | (omit) | (omit) | (omit) | (omit) | 0.65 | (omit) | (omit) | browsing |
| 7 | `anon_demo-re-007` | known | Emma | Wilson | emma.wilson@notion.so | 29 | Product Designer | Notion | $150k-$200k | single | 0.74 | [suburb-2] | 2 | purchase_within_90d |
| 8 | `anon_demo-re-008` | **anonymous** (second anonymous slot for variety in count=4 if ever needed) | | | | | | | | | | | | |

**At count=1**: profile #1 fires (known, Sarah Chen — primary demo path).
**At count=2**: profiles #1 + #2 (two known visitors, both fire `realtor_known_high_intent`).
**At count=3**: profiles #1 + #2 + #3 (two known + one anonymous → demonstrates BOTH rules in one click — this is the headline configuration).

**Common traits (all known realestate profiles)**:
- `first_seen_at`, `last_seen_at`: realistic timestamps (vary slightly per profile)
- `total_sessions`: integer, 5–18
- `total_listing_views`: integer, 30–80
- `is_repeat_visitor`: true
- `membership_tier`: "browse"
- `prior_listings_viewed`: list of 5–10 listing IDs from {L101..L120}, with overlap to current session ones to make the "this matches their pattern" story believable

**Rs-self rotation order**:

| # | id_type | id_value | first_name | last_name | email | company | role | plan | onboarding_progress | tech_stack | support_tier |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | user_id | `demo-rs-001` | Alex | Rivera | alex@acme.io | Acme | Engineer | free | destination_setup | [javascript, next.js, amplitude] | community |
| 2 | user_id | `demo-rs-002` | Jamie | Kim | jamie@beacon.dev | Beacon | DevOps Lead | growth | source_setup_done_destinations_pending | [react, segment-migrate] | priority |
| 3 | user_id | `demo-rs-003` | Sam | Patel | sam@cobaltdata.com | Cobalt Data | CTO | enterprise | webhook_validation_failing | [python, segment-migrate, hightouch] | enterprise |

Each rs-self profile is **also** indexable by `anonymous_id` with the same `id_value` (since the rs-self demo uses identical anonId/userId per §6.3 of the original LLD). Subagents must emit BOTH entries per profile.

### 4.3 `seed/canned-responses-hand.yaml` — rewrite with placeholders

Four templates, all using the placeholder grammar from §5.1:

1. **`realestate_realtor_pitch`** (modified — for KNOWN visitors only): rich Slack message with `{{trait.first_name}}`, `{{trait.last_name}}`, `{{trait.email}}`, `{{trait.age}}`, `{{trait.occupation}}`, `{{trait.employer}}`, `{{trait.income_band}}`, `{{trait.propensity_score}}`, `{{window.last_listing.id}}`, `{{window.last_listing.price}}`, `{{window.last_listing.bedrooms}}`, `{{window.dominant_suburb}}`, `{{window.last_filter.beds_min}}`, `{{realtor.name}}`, `{{realtor.phone}}`, `{{realtor.hours}}`, `{{outcome.estimated_deal_value}}`, `{{outcome.urgency_minutes}}`.
2. **`realestate_realtor_anonymous`** (NEW — for ANONYMOUS visitors): briefer message focused on the in-app-banner action; placeholders for window data only (no trait data); `{{outcome.recommended_action}}`, `{{realtor.name}}` (standby).
3. **`rs_destination_error`** (modified): subject + body now address user by `{{trait.first_name}}` and reference `{{trait.company}}` and `{{window.last_error.error_code}}`; existing 3-step fix structure preserved.
4. **`rs_onboarding_stuck`** (modified): subject + body address user by `{{trait.first_name}}` and reference `{{trait.onboarding_progress}}`.

The exact template text is in §5.2.

### 4.4 `seed/persona-configs/realestate.yaml` — split + add rule

Replace the single `realtor_session_abandoned` rule with two:

```yaml
- name: realtor_known_high_intent
  when:
    all:
      - window.event_count: { ">=": 3 }
      - window.event_path_matches: "^/listings(/|$).*"
      - window.idle_seconds: { ">=": 8 }
      - window.has_event_type: page
      - traits.known: email
  fire:
    action_template: realestate_realtor_pitch
    destination: "slack:realestate-realtor-pings"
    cooldown_seconds: 3600

- name: realtor_anonymous_high_intent
  when:
    all:
      - window.event_count: { ">=": 3 }
      - window.event_path_matches: "^/listings(/|$).*"
      - window.idle_seconds: { ">=": 8 }
      - window.has_event_type: page
      - not:
          traits.known: email
  fire:
    action_template: realestate_realtor_anonymous
    destination: "slack:realestate-realtor-pings"
    cooldown_seconds: 3600
```

**Rationale**: by gating on `traits.known: email` (presence of an email trait set via identify), we differentiate "this visitor identified themselves" from "they are anonymous" without needing an Activation-API-time check. The firer cooperates by emitting the full identify traits for known profiles and a sparse identify (no email) for anonymous profiles — see §5.3.

The existing rule name `realtor_session_abandoned` is **retired**. Cooldown rows keyed by it become orphaned but harmless (the schema's `rule_id` is nullable already and the demo's reset handler clears cooldowns).

`idle_seconds` lowered from 10 to 8 so the trigger fires within the script's natural idle window even at 0.5x speed.

The realtors list in `realestate.yaml` is expanded to include phone numbers (used by `{{realtor.phone}}`) — see §5.4.

### 4.5 In-memory window struct — three new fields

```go
type UserWindow struct {
    // ... existing fields unchanged
    LastListingProps map[string]any  // last "Listing Viewed" event's full property map
    LastFilterProps  map[string]any  // last "Filter Applied" event's full property map
    DominantSuburb   string          // most-frequent value of properties.suburb across all events
    suburbCounts     map[string]int  // unexported: incremental counter behind DominantSuburb
}
```

These are populated incrementally inside `apply()` and copied into `Snapshot` so the dispatcher can read them. They DO NOT participate in rule eval — they are only render-context inputs.

The unexported `suburbCounts` map keeps the dominant-suburb computation O(1) per event. On every event with `properties.suburb` set, we increment and recompute the dominant if a new max emerges.

### 4.6 SSE message contract — `triggers` stream

The existing `triggers` SSE message includes `window_snapshot`, `llm_parsed`, `dispatch_status`. We extend it (additive only) to include:
- `enriched_traits` (the activation API result map) — needed by `OutcomeBanner` to extract names without re-fetching
- `assigned_realtor` (subset: name, phone, hours) — needed by both Slack message text AND OutcomeBanner

These additions live in `runtime_dispatch.go` (or wherever the trigger SSE payload is constructed). The `replay_last_trigger` handler emits the same fields. Existing consumers (the Triggers Fired column) ignore unknown keys — additive change is safe.

## 5. Components

### 5.1 Placeholder grammar — `internal/dispatch/template.go`

**Syntax**: `{{section.path.to.value}}` where `section ∈ {trait, window, realtor, outcome}`.

**Resolver**:
```go
type RenderContext struct {
    Trait   map[string]any    // = activation.ProfileResponse.Data
    Window  map[string]any    // = derived from Snapshot — see below
    Realtor map[string]any    // = selected realtor as map (name, phone, hours, suburbs)
    Outcome map[string]any    // = template-specific synthetic values (deal_value, urgency)
}

// Render walks all string-typed values in `parsed` (a map[string]any from a
// canned response) and substitutes {{section.path}} with the resolved value
// from ctx. Missing paths render as "n/a" and emit a slog.Warn.
//
// Recursive: walks nested maps and slices. Non-string leaves pass through.
func Render(parsed map[string]any, ctx RenderContext) (map[string]any, []string)  // returns (rendered, missingPaths)
```

**Path resolution**: dot-separated. e.g., `window.last_listing.id` looks up `ctx.Window["last_listing"]` (must be a map), then `["id"]`. If any segment is missing or not a map when expected, the whole placeholder is "n/a".

**Numeric formatting**: numbers are stringified via Go's default `%v`, except when followed by a format hint: `{{trait.propensity_score:pct}}` formats `0.87` as `87%`. Supported hints: `:pct` (percent, 0 decimals), `:money` (commas + dollar sign — e.g. `1500000` → `$1,500,000`). No format hint = `%v`.

**Window derivation** (from `window.Snapshot`):
```go
window = map[string]any{
    "event_count":     snap.EventCount,
    "idle_seconds":    int(now.Sub(snap.LastSeen).Seconds()),
    "dominant_suburb": snap.DominantSuburb,
    "last_listing":    snap.LastListingProps,   // {id, price, bedrooms, sq_ft, suburb, ...}
    "last_filter":     snap.LastFilterProps,    // {beds_min, suburb, price_min, ...}
    "last_listing_id": stringFromMap(snap.LastListingProps, "listing_id"),
    "session_minutes": int(now.Sub(snap.FirstSeen).Minutes()),
}
```

(The duplicate `last_listing_id` shorthand is provided so templates can write `{{window.last_listing_id}}` without the nested-map lookup.)

**Outcome synthesis** (per template — derived in dispatcher):
- `realestate_realtor_pitch`: `outcome.estimated_deal_value` = `last_listing.price` formatted as `:money`; `outcome.urgency_minutes` = 30 (constant for the demo)
- `realestate_realtor_anonymous`: `outcome.recommended_action` = "Trigger in-app banner offering instant tour booking"; `outcome.urgency_minutes` = 60 (less urgent than known)
- rs-self templates: `outcome.fix_eta_minutes` = 5 (constant)

### 5.2 Canned response template text (verbatim — drop into YAML)

**`realestate_realtor_pitch.raw_json`**:
```yaml
headline: "🏡 {{trait.first_name}} {{trait.last_name}} is browsing in {{window.dominant_suburb}} — high intent"
visitor_summary:
  name: "{{trait.first_name}} {{trait.last_name}}"
  email: "{{trait.email}}"
  age: "{{trait.age}}"
  occupation: "{{trait.occupation}} at {{trait.employer}}"
  income_band: "{{trait.income_band}}"
  family: "{{trait.family_status}}"
  propensity_score: "{{trait.propensity_score:pct}}"
talking_points:
  - "Viewed {{window.event_count}} events in this session — last on listing {{window.last_listing.id}} ({{window.last_listing.price:money}}, {{window.last_listing.bedrooms}}BR)"
  - "Applied filter beds_min={{window.last_filter.beds_min}}, suburb={{window.last_filter.suburb}} — narrowing intent"
  - "Spent {{window.idle_seconds}}s on detail page before idling — strongest signal"
  - "Income band {{trait.income_band}} aligns with the {{window.last_listing.price:money}} list price"
  - "Repeat visitor — already viewed {{trait.total_listing_views}} listings across {{trait.total_sessions}} sessions"
best_cta: "Call {{trait.first_name}} on {{trait.email}} within {{outcome.urgency_minutes}} minutes. Lead with {{window.last_listing.id}} — matches every filter criterion."
estimated_deal_value: "{{outcome.estimated_deal_value}}"
urgency: high
assigned_realtor:
  name: "{{realtor.name}}"
  phone: "{{realtor.phone}}"
  hours: "{{realtor.hours}}"
```

**`realestate_realtor_anonymous.raw_json`** (NEW):
```yaml
headline: "🕵️ Anonymous high-intent visitor in {{window.dominant_suburb}} — recommend in-app capture"
visitor_summary:
  name: "Anonymous visitor"
  status: "Acting like a buyer (≥3 listings, narrowing filters) but has not identified"
talking_points:
  - "Viewed {{window.event_count}} events; last listing {{window.last_listing.id}} ({{window.last_listing.price:money}}, {{window.last_listing.bedrooms}}BR)"
  - "Filter pattern: beds_min={{window.last_filter.beds_min}}, suburb={{window.last_filter.suburb}}"
  - "Spent {{window.idle_seconds}}s idling on the detail page"
  - "No identify call yet — no name, no email, no phone"
best_cta: "{{outcome.recommended_action}}. If they engage, you'll capture an email within ~60s; reassign to {{realtor.name}}."
recommended_in_app_banner:
  headline: "Want a tour of {{window.last_listing.id}}? Tap to book in 30s — no signup required."
  cta: "Book a tour →"
urgency: medium
assigned_realtor_on_standby:
  name: "{{realtor.name}}"
  phone: "{{realtor.phone}}"
  hours: "{{realtor.hours}}"
```

**`rs_destination_error.raw_json`** (modified — adds salutation; body unchanged):
```yaml
subject: "Quick fix for the {{window.last_error.error_code}} error in your Amplitude setup, {{trait.first_name}}"
body_markdown: |
  Hi {{trait.first_name}},

  We just saw an `{{window.last_error.error_code}}` error from your Amplitude
  destination at {{trait.company}}. This is one of the top-3 destination setup
  errors and is almost always one of these three causes:

  1. **The API key is for a different Amplitude project.** Open Amplitude →
     *Settings → Projects* and copy the key from the project that should
     receive the data.
  2. **The key is admin-only without ingestion permissions.** Amplitude has
     separate keys for admin actions vs. server-side ingestion. RudderStack
     needs the ingestion key.
  3. **A trailing space was copied with the key.** Re-paste with no
     whitespace and click *Test Destination*.

  You can usually unblock this in under 2 minutes. If the *Test Destination*
  check still fails after step 3, reply with the request ID from the error
  log and we'll get you unstuck the same day.

  — RudderStack Onboarding
doc_links:
  - title: "Amplitude destination setup guide"
    url: "https://www.rudderstack.com/docs/destinations/streaming-destinations/amplitude/"
  - title: "Amplitude API keys explained"
    url: "https://www.rudderstack.com/docs/destinations/streaming-destinations/amplitude/#connection-settings"
  - title: "Test Destination button reference"
    url: "https://www.rudderstack.com/docs/dashboard-guides/destinations/#test-destinations"
next_step_cta: "Reply with your request ID if step 1-3 don't unblock you"
```

**`rs_onboarding_stuck.raw_json`** (modified, similar pattern):
```yaml
subject: "{{trait.first_name}}, your Amplitude destination is one step from done"
body_markdown: |
  Hi {{trait.first_name}},

  We saw you hit `{{window.last_error.error_code}}` while finishing your
  Amplitude destination setup at {{trait.company}}. Most teams clear this
  with a single fix — see the three checks below.

  ### Try this in order

  1. **Verify the key matches the right project.** In Amplitude, go to
     *Settings → Projects* and confirm the API key column matches what you
     entered in RudderStack.
  2. **Check the key's permissions.** Amplitude has separate keys for
     ingestion vs admin. RudderStack needs the ingestion key (server-side).
  3. **Test the connection** with the *Test Destination* button after
     re-pasting the key. A green checkmark confirms credentials are accepted.

  If it's still failing after these three steps, reply with the request ID
  from the destination error log and we'll get you unstuck same-day.

  — RudderStack
doc_links:
  - title: "Setting up Amplitude as a destination"
    url: "https://www.rudderstack.com/docs/destinations/streaming-destinations/amplitude/"
  - title: "Authentication & API keys (Amplitude destination)"
    url: "https://www.rudderstack.com/docs/destinations/streaming-destinations/amplitude/#connection-settings"
  - title: "Testing destination connections"
    url: "https://www.rudderstack.com/docs/dashboard-guides/destinations/#test-destinations"
next_step_cta: "Reply with your error log request ID for fast-track help"
```

### 5.3 Firer rotation logic — `internal/demofire/personas.go`

Add a `realestateProfileSpec` and `rsSelfProfileSpec` registry that pairs an `anonymousId` (and optionally `userId` for rs-self) with the *visible* identify traits the SDK would have already loaded. The firer reads the rotation table at script-build time:

```go
type profileSpec struct {
    AnonID        string
    UserID        string         // empty for realestate; equals AnonID for rs-self
    IdentifyTraits map[string]any // what the SDK pre-loaded; see below
}

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
        AnonID: "anon_demo-re-003",
        // ANONYMOUS — no email, no name. traits.known: email predicate fails;
        // realtor_anonymous_high_intent fires.
        IdentifyTraits: map[string]any{
            "membership_tier": "browse",
        },
    },
    // ... 4-8 follow same pattern with the other known visitors
}

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
    // ... 2-3 follow
}
```

**Variation per profile**: rather than 8 identical scripts with different IDs, each known profile fires a script with **slight variations** in suburb / listing IDs / filter values so the dashboard cards look distinct. The variation table:

| Profile | Suburb | Listings viewed | Filter beds_min | Final dwell listing |
|---|---|---|---|---|
| anon_demo-re-001 | suburb-1 | L101, L107, L112 | 3 | L112 |
| anon_demo-re-002 | suburb-1 | L107, L112, L115 | 4 | L115 |
| anon_demo-re-003 (anon) | suburb-1 | L101, L112, L118 | 3 | L118 |
| anon_demo-re-004 | suburb-1 | L107, L112, L120 | 3 | L120 |
| anon_demo-re-005 | suburb-3 | L301, L307, L312 | 4 | L312 |
| anon_demo-re-006 | suburb-2 | L201, L207 | 3 | L207 |
| anon_demo-re-007 | suburb-2 | L207, L209 | 2 | L209 |
| anon_demo-re-008 (anon) | suburb-3 | L301, L307 | 4 | L307 |

The script-build helper accepts a `realestateVariation` struct with these fields and produces the same 8-step script with the variation applied.

### 5.4 Realtor roster expansion

Update `seed/persona-configs/realestate.yaml`:

```yaml
realtors:
  - name: Priya N.
    phone: "+91-98765-43210"
    suburbs: [suburb-1, suburb-2]
    hours: "09:00-18:00 IST"
  - name: Arjun M.
    phone: "+91-98765-43211"
    suburbs: [suburb-3]
    hours: "10:00-19:00 IST"
  - name: Mira K.
    phone: "+91-98765-43212"
    suburbs: [countryside-1, countryside-2]
    hours: "08:00-17:00 IST"
  - name: Rohan B.
    phone: "+91-98765-43213"
    suburbs: [suburb-1, suburb-3]
    hours: "11:00-20:00 IST"
  - name: Tanvi S.
    phone: "+91-98765-43214"
    suburbs: [suburb-2]
    hours: "09:00-17:00 IST"
```

The loader (`rules.RealtorEntry`) gains a `Phone string` field. The dispatcher selects the realtor whose `suburbs` contains `window.dominant_suburb`; on no match, falls back to the first realtor in the list (and logs).

### 5.5 Concurrent firer — `internal/demofire/firer.go`

Add `RunConcurrent(ctx, scripts []NamedScript, speed float64) (totalSent int, err error)`:

- `NamedScript` = `{Persona string, Script []ScriptStep, AnonID string}`. The persona is for logging.
- Speed multiplier: each `ScriptStep.DelayMs` is multiplied by `1.0/speed` before sleeping. `speed=1.0` is the default; `speed=0.5` doubles every delay; `speed=2.0` halves them.
- Implementation: spawn a goroutine per script; use `sync.WaitGroup`; first error is captured and the wait returns it (other goroutines still drain to completion to avoid leaks). All goroutines respect ctx cancellation.
- **Stagger start**: between concurrent scripts insert a small offset (e.g., 500ms × index) so the dashboard SSE feed shows them spinning up in sequence rather than racing in lockstep.

### 5.6 Demo-fire HTTP handler

Extend `handleFireScript` to accept `count int` and `speed float64` from JSON body or query params:

- Default `count=1`, `speed=1.0` (preserves existing behavior).
- Validation: `count ∈ {1,2,3}`, `speed ∈ {0.5, 1.0, 2.0}`. Reject others with 400.
- Firer-callback signature changes from `func(ctx, persona) (count int, err error)` to `func(ctx, persona, count int, speed float64) (eventsSent int, err error)`. Existing callers in `cmd/realtime-trigger/serve.go` adapt.

### 5.7 Use-case gallery wizard — `frontend/components/wizard/UseCaseGallery.tsx`

A new step inserted **before** the existing PersonaPicker (which becomes a hidden middleware step driven by the gallery). The 4 cards:

```ts
type UseCase = {
  id: string;
  title: string;
  subtitle: string;
  persona: 'realestate' | 'rs-self';
  rule_template: string;        // matches a key in BackendRuleTemplates
  preview_action: string;       // human-readable ("Slack to standby realtor", "Personalized email")
  outcome_metric: string;       // ("Avg. response time: 6s", "Avg. recovery rate: 71%")
  icon: 'rescue' | 'alert' | 'wrench' | 'inbox';
};

export const useCases: UseCase[] = [
  {
    id: 'rescue_anonymous_high_intent',
    title: 'Win back high-intent anonymous visitors',
    subtitle:
      'When someone behaves like a buyer but has not signed up, alert your team to capture them with a personalized in-app prompt before they leave.',
    persona: 'realestate',
    rule_template: 'realtor_anonymous_high_intent',
    preview_action: 'In-app banner + Slack to standby realtor',
    outcome_metric: 'Avg. capture rate lift: +38%',
    icon: 'rescue',
  },
  {
    id: 'alert_known_high_value',
    title: 'Alert realtors to known high-value leads',
    subtitle:
      'When a known prospect is actively browsing in their target suburb, page the right realtor with their full context — name, budget, intent, and the listing in their cart.',
    persona: 'realestate',
    rule_template: 'realtor_known_high_intent',
    preview_action: 'Slack with full visitor profile + assigned realtor',
    outcome_metric: 'Avg. response time: 6 seconds',
    icon: 'alert',
  },
  {
    id: 'rescue_destination_error',
    title: 'Rescue stuck destination setup',
    subtitle:
      'When a customer hits an integration error during onboarding, send a tailored fix guide with the exact steps for their specific error before they churn out.',
    persona: 'rs-self',
    rule_template: 'rs_destination_error',
    preview_action: 'Personalized email with 3-step fix',
    outcome_metric: 'Avg. recovery rate: 71%',
    icon: 'wrench',
  },
  {
    id: 'reengage_stuck_onboarding',
    title: 'Re-engage abandoned onboarding',
    subtitle:
      'When a customer creates a source but never connects a destination, nudge them with the next step within 24 hours — with their progress and tech stack as context.',
    persona: 'rs-self',
    rule_template: 'rs_onboarding_stuck',
    preview_action: 'Personalized email with their setup status',
    outcome_metric: 'Avg. completion rate: +52%',
    icon: 'inbox',
  },
];
```

**UI layout**: 2×2 grid of cards. Each card uses the existing shadcn/ui `Card` primitive with a colored left-edge accent (Tailwind: `border-l-4 border-blue-500` for realestate, `border-l-4 border-emerald-500` for rs-self). Hover lifts the card (existing `hover:shadow-md transition-shadow`). On click, the wizard advances to the next step with `persona` and `rule_template` pre-populated.

**Replaces** the existing `PersonaPicker` as the entry-point step. The PersonaPicker component is kept as fallback (linked from a small "Or pick by persona →" link at the bottom of the gallery, in case demo audience asks for it).

The wizard step state machine:
```
[UseCaseGallery] → [QAStep (pre-filled)] → [ConfigPreview] → [Activate]
```

The QAStep's question set is unchanged but its initial values come from the use case (e.g., "rescue_anonymous_high_intent" → idle_seconds=8, persona=realestate; the user can still override).

### 5.8 OutcomeBanner — `frontend/components/dashboard/OutcomeBanner.tsx`

A horizontal banner that mounts above the 3-column dashboard. Subscribes to the `triggers` SSE stream. On a new trigger, animates in (Framer Motion `slide-from-top` + `fade-in` over 250ms) and displays the trigger's outcome.

**Two variants** based on `trigger.persona` + the rule name:

**A. Realestate KNOWN (`rule_name === 'realtor_known_high_intent'`)**:
```text
🏡 → Realtor [realtor.name] alerted for [trait.first_name + last_name]   |   Est. deal value: [outcome.estimated_deal_value]   |   Response window: [outcome.urgency_minutes] min
```
Pulled fields:
- `realtor.name` from `trigger.assigned_realtor.name` (NEW SSE field)
- `trait.first_name/last_name` from `trigger.enriched_traits` (NEW SSE field)
- `outcome.estimated_deal_value` from `trigger.llm_parsed.estimated_deal_value`

**B. Realestate ANONYMOUS (`rule_name === 'realtor_anonymous_high_intent'`)**:
```text
🕵️ → Anonymous high-intent visitor in [window.dominant_suburb]   |   Recommended action: [outcome.recommended_action]   |   Standby realtor: [realtor.name]
```

**C. Rs-self (`rule_name === 'rs_destination_error' | 'rs_onboarding_stuck'`)**:
```text
✉️ → Personalized fix sent to [trait.first_name] at [trait.company]   |   ETA to unblock: [outcome.fix_eta_minutes] min   |   Track in mock-email outbox →
```

The banner stays visible for 12 seconds, then fades out. If multiple triggers fire within that window (concurrent sessions), the banner cycles through them at 4 second intervals (a small dot-indicator at the right shows which-of-N is currently displayed).

### 5.9 ROITile — `frontend/components/dashboard/ROITile.tsx`

A compact card pinned to the top of the dashboard, between the page heading and the column grid. Three numbers, computed client-side from the running `triggers` SSE history:

- **Triggers fired**: count of `triggers` SSE messages since page load
- **Est. revenue protected**: sum of `outcome.estimated_deal_value` across realestate triggers + a fixed $40K per rs-self trigger (representing CLV protected by recovering an at-risk customer)
- **Avg. time-to-action**: average of `(trigger.fired_at - session.started_at)` across triggers — uses the SSE `events` stream's `received_at` of the *first* event for a given anonymousId as session start.

When count is 0, the tile renders a friendly empty state ("Fire a script to see live impact").

### 5.10 Demo controller — `frontend/components/demo/Controller.tsx`

Add two new control sets next to the existing Fire buttons:

- **Sessions selector** (segmented control): 1 / 2 / 3 — defaults to 2
- **Speed selector** (segmented control): 0.5x / 1x / 2x — defaults to 1x

The Fire button label updates dynamically: `Fire 2 realestate sessions @ 1x`. The fire request body becomes `{ persona, count, speed }`.

The auto-reset behavior (clear DB + windows before fire) introduced in the previous polish round is preserved.

## 6. Subagent execution plan

Two rounds, sized so each subagent can finish in a single Sonnet context window. Each round runs in parallel; main agent (Claude/orchestrator) integrates between rounds, runs smoke tests, and commits.

### 6.1 Round 1 — three parallel subagents

| WP | Owner | Files OWNED (write) | Files READ-ONLY |
|---|---|---|---|
| **WP-A** | loom-software-engineer (sonnet) | `seed/mock_profiles.yaml`, `seed/canned-responses-hand.yaml`, `seed/persona-configs/realestate.yaml`, `seed/persona-configs/rs-self.yaml`, `internal/rules/loader.go` (add `Phone` field on `RealtorEntry`), `internal/rules/predicates.go` (no changes — `traits.known: email` is sufficient) | rest of repo |
| **WP-B** | loom-software-engineer (sonnet) | `internal/window/window.go`, `internal/window/snapshot.go`, `internal/window/window_test.go`, `internal/dispatch/template.go` (NEW), `internal/dispatch/template_test.go` (NEW), `internal/dispatch/payload_adapter.go` (modify to call template-fill), `internal/dispatch/runtime_dispatch.go` if it exists (or wherever the trigger SSE message is built) — must thread `enriched_traits` and `assigned_realtor` into the SSE payload | `internal/rules/`, `internal/activation/`, `internal/llm/`, `internal/event/` |
| **WP-C** | loom-software-engineer (sonnet) | `internal/demofire/personas.go`, `internal/demofire/events.go`, `internal/demofire/firer.go`, `internal/demofire/firer_test.go`, `internal/demofire/personas_test.go` (if exists; else create), `internal/api/handlers_demo.go`, `cmd/realtime-trigger/serve.go` (only the firer-callback wiring), `internal/api/router.go` (only adapter to fireScript signature change) | `internal/event/`, `internal/sse/` |

**Round 1 acceptance criteria** (run by main agent before Round 2):

1. `cd /Users/kumar/workspace/realtime-ai-trigger-svc && go build -o /tmp/realtime-trigger ./cmd/realtime-trigger` succeeds with no errors.
2. `go test ./internal/...` passes (zero failures, including new tests in WP-B and WP-C).
3. `/tmp/realtime-trigger seed --from hand --seed-dir ./seed` runs without error.
4. `psql -c "SELECT id_value FROM mock_profiles ORDER BY id_value"` shows the 8 realestate + 3 rs-self entries (anon_demo-re-001/002/004/005/006/007 + 3 rs-self by anonymous_id and user_id).
5. `psql -c "SELECT template_name FROM canned_responses ORDER BY template_name"` shows `realestate_realtor_anonymous`, `realestate_realtor_pitch`, `rs_destination_error`, `rs_onboarding_stuck`.
6. `bash scripts/start-backend-local.sh` brings the backend up; `curl /healthz` returns ok.
7. `curl -s -X POST http://localhost:8080/api/demo/fire-script -H 'content-type: application/json' -d '{"persona":"realestate","count":3,"speed":1.0}'` returns 200 with `event_count > 0`.
8. After step 7, watching the events SSE stream shows three distinct anonymousIds firing concurrently.
9. After step 7's idle window, `psql -c "SELECT rule_name, anonymous_id, dispatch_status FROM triggers ORDER BY fired_at DESC LIMIT 5"` shows two `realtor_known_high_intent` rows + one `realtor_anonymous_high_intent` row, all `dispatch_status=ok`.
10. `psql -c "SELECT llm_parsed::text FROM triggers ORDER BY fired_at DESC LIMIT 1"` shows a fully-rendered Slack message with no `{{...}}` placeholders remaining (or with `n/a` substituted only where data was genuinely missing — never raw braces).

### 6.2 Round 2 — three parallel subagents

| WP | Owner | Files OWNED (write) | Files READ-ONLY |
|---|---|---|---|
| **WP-D** | loom-software-engineer (sonnet) | `frontend/components/wizard/UseCaseGallery.tsx` (NEW), `frontend/lib/use-cases.ts` (NEW), `frontend/app/onboarding/page.tsx` (modify to insert UseCaseGallery as step 1) | `frontend/components/wizard/PersonaPicker.tsx`, `frontend/components/wizard/QAStep.tsx`, `frontend/components/wizard/ConfigPreview.tsx`, `frontend/components/wizard/Stepper.tsx` |
| **WP-E** | loom-software-engineer (sonnet) | `frontend/components/dashboard/OutcomeBanner.tsx` (NEW), `frontend/components/dashboard/ROITile.tsx` (NEW), `frontend/app/dashboard/page.tsx` (modify to mount OutcomeBanner + ROITile) | `frontend/lib/sse.ts`, `frontend/lib/api-client.ts`, `frontend/types/` |
| **WP-F** | loom-software-engineer (sonnet) | `frontend/components/demo/Controller.tsx` | `frontend/lib/api-client.ts`, `frontend/types/` |

**Round 2 acceptance criteria**:

1. `cd frontend && pnpm typecheck` (or `pnpm tsc --noEmit`) passes.
2. `pnpm build` succeeds.
3. `pnpm dev` runs; visiting `http://localhost:3000/onboarding` shows a 2×2 grid of 4 outcome cards.
4. Clicking any card advances to the QA step with the correct persona pre-selected.
5. Visiting `http://localhost:3000/dashboard` with backend up + after firing 1 realestate trigger:
   - ROI tile shows `Triggers fired: 1`, `Est. revenue protected: $1,...`, `Avg. time-to-action: ~Xs`.
   - OutcomeBanner appears with text matching the realestate-known template (visitor name visible).
6. After firing `count=3 speed=1x`, OutcomeBanner cycles through 3 messages (anonymous variant for one of them); ROI tile increments correctly.
7. Demo controller shows segmented controls for sessions (1/2/3) and speed (0.5x/1x/2x).

### 6.3 Round 3 — main agent integration smoke test (sequential)

Main agent (orchestrator) performs:

1. End-to-end Playwright-style flow: load /onboarding, pick "Win back high-intent anonymous visitors", customize idle_seconds=5, activate, navigate to dashboard, click "Fire 3 realestate sessions @ 1x", observe 3 cards appear, wait for triggers, verify OutcomeBanner cycles correctly, verify ROI tile updates.
2. Run `bash scripts/start-backend-local.sh` and verify Slack message arrives in #realestate-realtor-pings (if SLACK_WEBHOOK_URL is set in `.env.local`).
3. Run `pnpm test` if frontend tests exist.
4. Final commit: separate semantic commits per WP scope (e.g., `feat(seed): expand mock profiles with rich demographics`, `feat(dispatch): templated canned responses with placeholder fill`, `feat(frontend): use-case gallery wizard`).
5. Push to `git@github.com:kuldeep0020/realtime-events-ai-trigger-svc.git`.

### 6.4 Failure handling (no human input available)

Main agent makes these decisions autonomously per failure mode:

| Failure | Decision |
|---|---|
| WP-A subagent fails (data) | Critical-path; retry once with a more detailed prompt. If 2nd retry fails, abort the round and ship what's done. |
| WP-B fails (template-fill) | Critical-path; retry once. If 2nd fails, fall back to a simplified template (no nested-map placeholders, only flat `{{first_name}}`-style); rewrite YAML to match. |
| WP-C fails (concurrent firer) | Retry once. If 2nd fails, fall back to single-session firing only — the dashboard story still holds with one rich Slack message. |
| WP-D fails (gallery) | Retry once. If 2nd fails, keep the existing PersonaPicker as wizard step 1; move on. |
| WP-E fails (banner+tile) | Retry once. If 2nd fails, ship banner alone (skip ROI tile) — banner is the primary value. |
| WP-F fails (controller) | Retry once. If 2nd fails, ship without count/speed UI — keep the existing Fire button calling `count=1 speed=1.0`. |
| Tests fail with ambiguous cause | Run `go vet ./...` + `go test -run <one test>` for diagnosis; fix obvious issues; retry. |
| Backend won't start | Tail `/tmp/rt-svc-logs/serve.log`; common causes are env vars or port conflicts. Kill any existing `/tmp/realtime-trigger` and retry. |
| Frontend won't typecheck | Read the error, fix the type, retry once. If subagent introduced unrelated breakage, revert their commit and re-spawn the WP. |

**Hard cutoff**: 6 hours of wall-clock time. Beyond that, ship what works and write a status note in `HANDOFF.md`.

## 7. Open questions answered (no user input needed)

| Question | Answer |
|---|---|
| Should the firer load mock_profiles.yaml dynamically? | No — hardcode the rotation in `personas.go`. Simpler, faster, and YAML is the canonical *activation* source-of-truth (firer's identify traits are a deliberate subset). |
| Where do realtor phone numbers come from? | Hand-authored in `realestate.yaml` (§5.4). |
| Should `dominant_suburb` use mode (count) or last (recency)? | Mode (count). For sessions where multiple suburbs appear, the most-frequent is more representative. Tie-break on recency. |
| Should the use-case gallery cards be configurable from backend? | No — hardcoded in `frontend/lib/use-cases.ts`. Demo doesn't need server-driven content; cards' `rule_template` IDs map to backend templates which are server-driven. |
| OutcomeBanner cycle interval | 4 seconds per banner; total visibility 12 seconds for the last one (configurable in component constants). |
| ROI tile estimation method | Sum `last_listing.price` for realestate; flat $40K per rs-self trigger. The number is illustrative; subagent must add a small `(est.)` label so it doesn't read as a real metric. |
| Should template-fill happen in dispatcher or earlier? | Dispatcher. Keeps the LLM client interface unchanged; render context lives where it's consumed. |
| Should we use Bedrock to enrich canned responses? | No. Hand-templating is sufficient and deterministic. Bedrock key is available as an emergency fallback if a subagent reports inadequate language quality after retries. |
| What if activation API misses for `id_type=anonymous_id`? | Existing fallback chain still applies: try `user_id`. If both miss, `enriched_traits` is empty `{}`, template-fill renders missing fields as `n/a`, the `realtor_anonymous_high_intent` rule fires (since `traits.known: email` is false for these too — note: anonymous profiles don't send email in identify either), and the anonymous Slack message variant is dispatched. |
| Concurrency of multiple `Fire` clicks in flight | Each `RunConcurrent` call is independent. The auto-reset clears state before each click. The user can click Fire multiple times in succession — the simplest behavior is "in-flight click finishes its scripts, next click reset+fires again". No queueing needed. |

## 8. Test strategy

- **Unit tests** (Go): `internal/dispatch/template_test.go` covers placeholder substitution, missing paths, format hints, recursive maps, slice walking. `internal/window/window_test.go` adds cases for `LastListingProps`, `LastFilterProps`, `DominantSuburb`. `internal/demofire/firer_test.go` covers `RunConcurrent` with mocked HTTP and a clock.
- **Integration tests** (Go): `internal/api/api_test.go` extends with a count=3 fire-script test against an in-memory mock pulsar.
- **Smoke tests** (main agent): the §6.1 / §6.2 acceptance lists are run by the main agent and the user.
- **No new e2e Playwright tests** in this scope (existing ones still apply if present).

## 9. Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Concurrent firer goroutines step on each other | low | each goroutine has its own anonymousId; HTTP client is shared but Go's `http.Client` is goroutine-safe |
| Template-fill renders empty Slack message because activation missed | low | hardcoded defaults + `n/a` fallback per §3.3; existing dispatcher's hardcoded fallback (compiled into binary) still applies if canned response is missing |
| `traits.known: email` predicate behaves unexpectedly | low | covered by new unit test in WP-A; exact behavior verified in `predicates.go::buildTraitsKnown` source |
| Subagent edits a shared file outside its ownership | medium | strict file ownership table in §6; subagent preamble per CLAUDE.md Rule 5 forbids out-of-scope edits |
| Frontend build fails because of TypeScript strictness | medium | each frontend WP must run `pnpm typecheck` as acceptance criterion |
| Demo accidentally cooldowns mid-rehearsal | low | auto-reset already in place; `/api/demo/reset` clears cooldowns table |
| Stage cooldown tracking breaks when new rule names are introduced | low | the existing rules engine indexes cooldowns by `(rule_id, anonymous_id)`; new rule names get fresh rows; no migration needed |
| Subagents collectively exceed context | low | each WP is bounded to ≤8 files and ≤500 lines of new/changed code |
| Bedrock key becomes the critical path | low | spec uses hand-templating; Bedrock is fallback-only |

## 10. Sign-off checklist (pre-approval, user reviews)

- [ ] §1 Goals match what I asked for (concurrent users, mature mock activation, business outcome legibility)
- [ ] §4.2 mock_profiles list has the right characters and the right ratio of known/anonymous
- [ ] §5.2 canned response template text reads correctly in my voice for the demo
- [ ] §5.7 use-case card copy is what I want the audience to see first
- [ ] §6 file ownership table has no collisions (visual scan)
- [ ] §6.4 failure handling defaults are acceptable — main agent makes these calls without waking me
- [ ] §7 open-questions answers are all reasonable defaults

Once Kuldeep approves this spec by saying "go ahead" or equivalent, the main agent executes the implementation plan in `docs/plans/PLAN-demo-maturity.md` autonomously.
