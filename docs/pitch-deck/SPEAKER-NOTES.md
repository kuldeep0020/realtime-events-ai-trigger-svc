# Speaker notes — Realtime Events AI Triggers

**Total**: 6-8 min · 6 slides · ~30-40s per slide + 4 min demo
The 4-minute live demo lands between slide 4 and slide 5.

---

## Slide 1 — Title (5-10s)

> "Realtime Events AI Triggers — a real-time, AI-personalized action layer for the streaming events you already collect."

Hold for a beat. Move on.

---

## Slide 2 — The Gap (45s)

Set up the problem with three quotes. Don't read them — paraphrase.

> "Three teams, same problem. Marketing watches anonymous high-intent visitors leak out of the funnel — and they only learn about it tomorrow. Customer Success learns at the QBR that a whale was on pricing a week ago. Onboarding watches customers churn at the destination-connect step, and every error needs a different fix."

Closing line:
> "Real-time pipelines deliver events. Warehouse audiences segment over weeks. There's nothing in between that holds short-term memory of a session, watches for live patterns, and acts in seconds. So teams stitch it together with brittle scripts and miss the moment."

---

## Slide 3 — Use Cases (60s)

Six cards in a 3×2 grid. Don't read them — gesture and call out the structure.

> "Six moments worth catching. The top row is the alerting + capture flow — anonymous capture, known engagement handoff, error rescue. The bottom row is the platform layer — re-engaging stalled flows, generating synthetic events from patterns, and exposing short-term context as a queryable API."
>
> "And — the ribbon at the bottom — the same engine handles every single-event webhook pattern you'd otherwise wire by hand in Transformer."

If asked about specific outcomes during Q&A:
> "We don't have customer data yet — these are capability claims, not measured outcomes. The point of the hackathon was proving the engine works end-to-end, which the demo will show."

---

## Slide 4 — How it works + DEMO CTA (60s + 4 min demo)

Brief on the pipeline AND the algorithm:

> "Five stages. Events come in over Pulsar or any SDK. The window manager keeps per-user state — for every anonymousId, we hold roughly the last 15 minutes of behavior in memory: event counts by type and name, distinct page paths, last-seen timestamp, last error event, and identify-time traits. That's the short-term memory."
>
> "The rules engine reads YAML predicates: window.idle_seconds, window.event_count, window.event_path_matches, traits.known, has_event_name. They compose with AND/OR/NOT. An idle ticker scans every second; when a predicate combination matches AND no cooldown is active for that user, the rule fires."
>
> "On fire, we enrich with the customer's profile from the Activation API and template-fill an action — Slack with name, email, propensity, the listing they dwelled on; or an email referencing their exact error code and stack. Then we dispatch — Slack, email, in-app, anywhere."
>
> "Three things make this different from cron-job alerting. Twelve composable predicates including stateful ones like idle_seconds. AI-templated action content with placeholders filled at fire time. Pluggable destinations."

Click the demo CTA bar — switch to the live dashboard:

> "Let me show you. Three concurrent visitors, two Slack pings, one personalized email, in about 30 seconds."

**[Run the demo per the runbook below.]**

---

## Slide 5 — Competitive landscape (45s)

> "We're not the first to think about real-time event-driven action — but no one composes all six of these capabilities into a single substrate. Segment Functions and Tealium handle simple webhooks. Customer.io and Braze do stateful windowing inside their own messaging engines but lock you into their channels. None of them touch LLM-personalized content; they ship templates only."
>
> "We're the gap-filler. Same engine, every event, every channel."

---

## Slide 6 — What's next (30s)

> "Today, this is hackathon code. Six work packages, end-to-end working. Production path: multi-tenant scaling, live LLM mode, real Activation API integration, ClickHouse for archive and replay, CEL or Drools for richer rule expressions, a pluggable destination registry."
>
> "The substrate piece for AI-native automation. Same engine, every event, every customer."

Hold on the closing line. Q&A.

---

## How intent is captured — algorithm cheat sheet for Q&A

If asked "how does the system know when intent is high?":

**Per-user window state (sharded, in-memory)**
Every event for a given anonymousId folds into:
- `EventCount` — total events in window
- `EventTypeCount` — `{page: 4, track: 8, identify: 1}`
- `EventNameCount` — `{Listing Viewed: 3, Filter Applied: 2}`
- `DistinctPaths` — set of page paths visited
- `PathLatest` — current page
- `PropertyMaxNum` / `PropertyLast` — for tracked numeric/string keys (price, beds_min)
- `LastSeen` — server-side wall clock; basis for idle detection
- `HasErrorEvent` / `LastErrorEvent` — for onboarding rescue
- `Traits` — identify-time attributes (email, plan, role)
- `LastListingProps` / `LastFilterProps` / `DominantSuburb` — domain-specific aggregations

**Idle ticker**
Every second a goroutine scans windows whose `LastSeen` is within reach of any time-based predicate threshold and re-evaluates rules.

**Predicates compose**
A "high intent anonymous" rule isn't one signal — it's a composition:
```
all:
  - window.event_count: { ">=": 3 }
  - window.event_path_matches: "^/listings(/|$).*"
  - window.idle_seconds: { ">=": 8 }
  - window.has_event_type: page
  - not:
      traits.known: email
```
That's "viewed enough listings + dwelled on the right path + paused + still anonymous". The combination is what defines intent — no single signal would.

**Cooldown + dedup**
After a rule fires for a user, a cooldown row in Postgres prevents re-firing for the configured duration (1 hour for realtor pings, 24 hours for onboarding nudges). Survives pod restart.

**Fire path**
1. Match → enrich with Activation API profile (name, traits, propensity)
2. Generate action via canned template + placeholder fill (`{{trait.first_name}}`, `{{window.last_listing.id}}`, `{{realtor.name}}`)
3. Dispatch (Slack webhook, mock email writer, plug-in interface for any destination)
4. Persist `triggers` row + cooldown
5. Broadcast via SSE to dashboard

**Why this isn't just a Transformer rule**
A Transformer fires on a single event. This system reasons over **a window of events** — counts, time gaps, narrowing patterns, presence/absence of identify, last-error context — and personalizes the action with the customer's profile at fire time. That's what makes "high intent" detectable in the first place.

---

## Demo runbook (4 min, runs between slides 4 and 5)

**Pre-demo setup** (do BEFORE the meeting):
1. `bash scripts/start-backend-local.sh` — backend up at :8080
2. Frontend at :3001 with `NEXT_PUBLIC_API_BASE=http://localhost:8080` in `.env.local`
3. Reset state: `curl -s -X POST http://localhost:8080/api/demo/reset > /dev/null`
4. Pre-load `http://localhost:3001/onboarding` in the browser
5. Have Slack channel `#realestate-realtor-pings` open in another tab

**Path 1 — wizard + realestate fire (~150s)**

1. **`/onboarding`** is open. Point at the four cards: *"This is what the product solves, not how it works."*
2. Click **"Alert realtors to known high-value leads"** (second card).
3. Right pane fills with the tracking plan. *"These are the events the SDK is already capturing."*
4. Drop **idle_seconds from 30 → 5**. Click **Generate config**.
5. Preview shows **2 rules**. Click **Activate & continue** → land on `/dashboard`.
6. ROI tile shows empty state. Set Sessions = **3**, Speed = **1x**.
7. Click **Fire 3 realestate sessions @ 1x**. Status reads "Firing 3 realestate sessions @ 1x — events streaming…"
8. **At T+3s**: Live Events column starts filling, Active Sessions count → 3.
9. **At T+33s**: triggers fire. OutcomeBanner cycles three messages — Sarah Chen, Marcus Lee, Anonymous. ROI tile populates.
10. **Click into a trigger card** in the Triggers Fired column. Expand the Slack message. Show: name, email, age, occupation at Stripe, propensity 87%, recommended realtor with phone, $1.5M deal value.
11. **Switch to your Slack tab**: same message delivered live.

**Path 2 — rs-self path (~75s)**

12. Click **Reset demo**.
13. Click **Fire 2 rs-self sessions @ 1x**.
14. **At T+15s**: 2 triggers fire.
15. Click the **Emails tab**. Show 2 emails to alex@acme.io and jamie@beacon.dev with personalized subjects referencing each customer's stack and the exact error code.
16. Click an email — show the personalized fix guide referencing AMP_INVALID_API_KEY and Acme.

**If anything breaks mid-demo**:
- Live Events stuck "Waiting for events…" → reload `/dashboard` (rev-8 SSE consolidation should prevent; if recurs, browser console: type `__sse_debug()` to inspect)
- Fire button stuck "Resetting…" → click **Reset demo** manually, then Fire again
- OutcomeBanner doesn't show → click **Replay last trigger**
- Slack message didn't arrive → ignore on stage; show the Triggers Fired card content instead

**Pacing**: at speed=1x the realestate demo is ~32s of script + 8s idle = ~40s before triggers. Switch to Speed = 2x to halve script time. The audience won't see it as rushed — the dashboard fills the whole 40s with visible event activity.
