# Speaker notes — Realtime Events AI Triggers

**Total**: 6-8 min · 6 slides · ~30-40s per slide + 4 min demo

The 4-minute live demo lands between slide 4 and slide 5.

---

## Slide 1 — Title (5-10s)

> "Realtime Events AI Triggers — a real-time, AI-personalized action layer for the streaming events you already collect."

Just hold for a beat. Move on.

---

## Slide 2 — The Gap (45s)

Set up the problem with three quotes. Don't read them — just glance and paraphrase.

> "Three teams, same problem. Marketing watches anonymous high-intent visitors leak out the bottom of the funnel — and they only find out about it tomorrow. Customer Success learns about a whale browsing pricing at the QBR — a week too late. Onboarding watches customers churn at the destination-connect step, and every error needs a different fix."

Closing line:
> "Today's options force a choice — real-time but generic, or personalized but on yesterday's batch. Customers stitch this gap with cron jobs, BigQuery dashboards, and Slack scripts that age fast."

---

## Slide 3 — Use Cases (45s)

Don't read all four — gesture at the grid and call out the structure:

> "Four moments worth catching. Each maps to one of the pain points we just saw."
> 
> "Top-left: anonymous high-intent rescue. Top-right: known-customer alerting. Bottom-left and bottom-right: rescuing and re-engaging customers in onboarding flows."
> 
> "And — the ribbon at the bottom — the same engine handles every single-event webhook pattern you'd otherwise wire by hand in Transformer. Same plumbing, no extra layer."

---

## Slide 4 — How it works + DEMO CTA (60s + 4 min demo)

Brief on the pipeline:

> "Five stages. Events come in over Pulsar or any SDK, the window manager keeps per-user state — counts, idle, dwell. The rules engine reads YAML predicates with AND/OR/NOT composition. Enrichment pulls the customer's Activation API profile and an LLM call generates the action content. Then we dispatch — Slack, email, in-app, anywhere."
> 
> "Three things make this different from cron-job alerting: twelve composable predicates including stateful ones like idle_seconds, AI-templated action content with placeholders filled at fire time, and pluggable destinations."

Click the demo CTA bar — switch to the live dashboard:

> "Let me show you. Three concurrent visitors, two Slack pings, one personalized email, in about 30 seconds."

**[Run the demo per the runbook in `docs/pitch-deck/SPEAKER-NOTES.md`'s Demo section below.]**

---

## Slide 5 — Competitive landscape (45s)

> "We're not the first to think about real-time event-driven action — but no one composes all six of these capabilities into a single substrate. Segment Functions and Tealium handle simple webhooks. Customer.io and Braze do stateful windowing inside their own messaging engines but lock you into their channels. None of them touch LLM-personalized content; templates only."
> 
> "We're the gap-filler. Same engine, every event, every channel."

---

## Slide 6 — What's next (30s)

> "Today, this is hackathon code. Six work packages, end-to-end working. Tomorrow's path: multi-tenant scaling, live LLM mode, real Activation API integration, ClickHouse for archive and replay, CEL or Drools for richer rule expressions, a pluggable destination registry."
> 
> "The substrate piece for AI-native automation. Same engine, every event, every customer."

Hold on the closing line. Q&A.

---

## Demo runbook (4 min, runs between slides 4 and 5)

**Pre-demo setup** (do BEFORE the meeting starts):
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
15. Click the **Emails tab**. Show: 2 emails to alex@acme.io and jamie@beacon.dev with personalized subjects referencing each customer's stack and the exact error code.
16. Click an email — show the personalized fix guide referencing AMP_INVALID_API_KEY and Acme.

**If anything breaks mid-demo**:
- Live Events stuck "Waiting for events…" → reload `/dashboard` (the rev-8 SSE consolidation should prevent this, but if it recurs, browser console: type `__sse_debug()` to see broadcaster state)
- Fire button stuck "Resetting…" → click **Reset demo** manually, then Fire again
- OutcomeBanner doesn't show → click **Replay last trigger**
- Slack message didn't arrive → ignore on stage; show the Triggers Fired card content instead

**Pacing**: at speed=1x the realestate demo is 32s of script + 8s idle = ~40s before triggers. If you're tight on time, switch to **Speed = 2x** which halves the script time. The audience won't see it as "rushed" — the dashboard fills the whole 40s with visible event activity.
