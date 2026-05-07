# Real-time AI Trigger Service — Pitch Deck Draft

**Slot**: 6 min target / 8 min cap, Q&A to 15. Two live demos.

---

## SLIDE 1 — The Gap *(0:00–0:45)*

> **Real-time AI triggers: the missing leg of an agentic CDP.**

**The setup, in three beats:**

1. **Today, RudderStack does two things really well.** The data plane reliably routes events to destinations. The warehouse aggregates them into a Customer 360 — Profiles + Activation API — that any system can query in milliseconds.
2. **But there's a gap between *event happened* and *something useful happens.*** Customers solve it today by gluing Twilio Engage, Hightouch streaming, Customer.io, bespoke webhooks — fragmented stack, weeks of integration, no consistent governance.
3. **This is the "act in seconds" leg of Soumyadeb's "CDPs as substrate for AI agents" thesis.** Profiles + Activation = read-only batch features. The missing piece is **live reasoning + outbound action** — observe the stream, decide in seconds, fire an enriched personalized payload to any destination.

**One-liner**: *"We're proposing the agent-side substrate that sits on top of the data plane and turns events into actions before the user has closed the tab."*

Visual: simple before/after diagram — left: events → warehouse → batch ETL → Twilio/Hightouch (slow, glued); right: events → trigger engine (LLM-augmented) → action (seconds).

---

## SLIDE 2 — Architecture *(0:45–1:30)*

```text
SDK ──► ingestion-svc ──► Pulsar ──► realtime-ai-trigger-svc ──► Slack / Email / Webhook
                                              │                          ▲
                                              ├─► Postgres event archive │
                                              ├─► Activation API (read)  │
                                              └─► Kapa.ai (RAG)          │
                                                  ↓                      │
                                              LLM (canned in demo,       │
                                              live in production)        │
                                                  ↓                      │
                                              Action JSON ───────────────┘

In-memory aggregations only (sharded by anonymousId). Full events fetched
from Postgres at trigger fire time. Rules-first; LLM only at action gen.
```

**Speak to**:
- **Pulsar fan-out** keeps the consumer independent of the rest of the data plane — replay-friendly, cleanly isolatable.
- **Aggregations-only memory** keeps the hot path cheap (no event copies); rules engine has 12 predicates over deterministic counts.
- **Trigger fire = enrich + decide + act.** Read full events from PG (last 15 min, indexed on `anonymous_id`) → fetch enriched traits from Activation API → for help-flows, retrieve relevant docs from Kapa → render LLM action template → adapt to dispatch → out the door.
- **Canned-first LLM** means zero network risk during the demo. Same client interface; flip `LLM_MODE=live` for production via local-agent or Bedrock.
- **Async dispatch** (4-worker pool, per-fire 30s timeout, drop-on-backpressure counter) means a slow Slack call can't freeze the consumer.
- **SSE hub** fans live state to the dashboard UI for the audience to watch.

---

## SLIDE 3 — Generality: Same Engine, N Use Cases *(3:00–4:30)*

Cards layout — four use cases, same engine, different persona config:

| # | Use case | Trigger pattern | Action |
|---|---|---|---|
| 1 | **Real-estate session abandonment** *(LIVE DEMO 1)* | 3+ listing views in one suburb + filter applied + 10s idle | Slack ping to the matching realtor with a personalized pitch |
| 2 | **RudderStack onboarding stuck** *(LIVE DEMO 2)* | `Destination Setup Error` event with code `AMP_INVALID_API_KEY` | Mock email with explanation, doc links from Kapa, next-step CTA |
| 3 | **B2B PQL detection** *(slide-only)* | `Pricing Page Viewed` ≥ 3 + identify within 7 days | Slack to AE with auto-generated dossier + talking points |
| 4 | **Real-time data-quality alert** *(slide-only)* | `Purchase` events drop to zero for 30 min OR new property has >50% nulls | Slack to data-eng with the affected source + diff |

**Speak to**: same Pulsar consumer + window manager + rules engine + dispatcher. **Only the persona config (rules + action template + canned response) and the destination differ.** That's the productization wedge — every customer onboards via a conversational LLM wizard that asks 3 domain questions and generates the rules YAML automatically. No analytics-engineer dependency.

---

## SLIDE 4 — Production Roadmap *(4:30–5:15)*

What's hackathon-shaped vs what ships in v1:

| Layer | Hackathon today | Production v1 |
|---|---|---|
| Event archive | Postgres (single-pod) | ClickHouse (sharded, 30-day TTL) |
| LLM at runtime | Canned (Postgres-backed) | Live via local-agent + Bedrock fallback (`LLM_MODE=live`) |
| Activation API | Mocked in-process | Real `POST /v1/activation` (Bearer SAT) |
| Multi-tenancy | Single tenant | Per-tenant rate limits + LLM budget caps + isolation |
| Rules DSL | Hand-rolled (12 predicates) | CEL or Drools — richer expressions, sandboxed eval |
| Activation enrichment | Read-only | Write-back: derived signals (intent score, segment, churn risk) flow back into the profile |
| Latency target | <2s end-to-end (single host) | <5s p99, multi-region |
| Pricing model | — | Per-trigger fired + LLM-token passthrough |

**Closing line**: *"The data plane is the foundation. Profiles + Activation gives agents a read substrate. This adds the act substrate — the missing leg. Six minutes of code; the moat is the integration breadth and governance, not the model."*

---

## DEMO 1 SCRIPT — Real-estate (90s, 1:30–3:00)

| Time | On stage | Behind the scenes |
|---|---|---|
| 0:00 | Open `/onboarding`, pick "Real-estate" | Frontend hits `/api/tracking-plan/realestate` |
| 0:10 | Show the auto-loaded tracking plan (page, Listing Viewed, Filter Applied with rich props) | Wizard reads PG `tracking_plans` |
| 0:15 | Pre-filled answers: 3 realtors × suburbs, $1M-$1.8M, 30s idle | (no LLM call — preset config) |
| 0:25 | Click Generate → preview the rules YAML → Activate | `POST /api/onboarding/generate-config` → `POST /api/onboarding/activate` |
| 0:30 | Land on `/dashboard`. Click "Fire Real-Estate Script" | `POST /api/demo/fire-script` → demo-fire CLI publishes 8 events directly to local Pulsar |
| 0:35–1:00 | **Column 1** animates incoming events; **Column 2** shows the session aggregating; visitor's `anonymousId` builds up event count, distinct paths, top properties | Consumer reads from Pulsar; window store updates aggregations |
| 1:00 | After last `Listing Viewed` on L112, **idle ticker** crosses 10s threshold | Rules engine fires `realtor_session_abandoned`; matchCh enqueues |
| 1:05 | **Column 3** flashes a new trigger card: "Why" (the rule + window snapshot), "Decision" (the LLM-rendered Slack message preview), "Delivered" (status: pending → sent) | Dispatcher reads canned LLM response; Slack webhook delivers |
| 1:15 | **Slack channel projected on screen** receives the message: realtor pitch with talking points, best CTA, urgency | Block Kit render |
| 1:25 | Narration: *"~1.5s end-to-end. Realtor calls in 60 seconds, not 24 hours."* | — |

---

## DEMO 2 SCRIPT — RudderStack onboarding (90s, 3:00–4:30 if extended)

| Time | On stage | Behind the scenes |
|---|---|---|
| 0:00 | Switch persona → "RudderStack onboarding" via wizard | Same `/onboarding` flow, different tracking plan |
| 0:15 | Pre-filled answers: which error events trigger help, idle window, channel = email | (preset config) |
| 0:30 | Run "Fire RS-Self Script" → 5 events fire over 12s | demo-fire publishes directly to Pulsar |
| 0:30–0:50 | Column 1 shows: identify → Account Created → Source Created → **Destination Setup Error** (red highlight, error_code AMP_INVALID_API_KEY visible) | Same pipeline; `HasErrorEvent` aggregation flips |
| 0:55 | Trigger fires immediately on the error event | Rules: `onboarding_errored` matches |
| 1:00 | Column 3 trigger card. **Decision** subsection renders an email preview: subject, markdown body explaining the error, **3 Kapa.ai-retrieved doc links**, next-step CTA | Enricher pulls Kapa canned response; LLM renders email-shaped action |
| 1:15 | Click "View email" → faux-Gmail dialog renders the full body with clickable doc links | UI dialog (no SMTP) |
| 1:25 | Narration: *"Free-tier user gets the experience of a CSM without the headcount."* | — |

---

## RISK BUFFERS / FALLBACK LADDER

1. **Slack 5xx mid-demo** → async dispatch absorbs it; trigger row still records; UI shows `dispatch_status=failed`; we narrate "this is the H1 fix from this morning's review" (turns a bug into a credibility moment).
2. **Pulsar disconnect mid-demo** → consumer auto-reconnects; meanwhile we use "Replay last trigger" button to re-emit the prior trigger to the SSE stream so the audience sees the action.
3. **Canned response missing** → hardcoded compiled-in defaults so output is never empty.
4. **Frontend SSE hiccup** → reload page; reconnect handler resumes.
5. **Total backend crash** → switch to pre-recorded fallback video per persona (record both before going live).

---

## OPEN ITEMS BEFORE GOING LIVE

- [ ] Pulsar local broker switched to JWT (in flight via Pulsar JWT setup subagent)
- [ ] demo-fire `--target pulsar` mode (in flight via demo-fire subagent)
- [ ] Single end-to-end smoke run for both personas
- [ ] Three rehearsals
- [ ] Two fallback videos recorded (one per persona, ~45s each)
- [ ] Final 4-slide deck rendered (this draft → Slidev or Google Slides)

---

## SOUNDBITES TO DROP IN Q&A

- *"Same engine. Different config. Each customer onboards in conversation."*
- *"Canned at runtime, live for the seed run. The swap is a single env var."*
- *"Profiles tells you who they are. This tells you what to do about it, in seconds."*
- *"The moat isn't the model. The moat is being the only place where every event, every destination, and every customer profile already line up."*
- *"Costs at hackathon scale: zero LLM tokens at runtime. At production scale: one LLM call per fired trigger, capped per tenant."*
- On scale (for Leo/Fotis): *"50k events/sec ingest is trivial for Pulsar; the rules engine evaluates aggregations at >100k EPS per shard. The expensive part — LLM — is gated to the trigger boundary, which is 0.1–1% of events. Per-tenant LLM budget cap stops a noisy neighbor."*
- On differentiation (for Sumanth/Soumyadeb): *"Hightouch streaming reverse ETL is seconds-from-warehouse. We're seconds-from-event-bus — no warehouse round-trip, no separate messaging stack, no analytics-engineer required."*

---

*draft v1 — refine post-rehearsal*
