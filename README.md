# realtime-events-ai-trigger-svc

A real-time AI trigger service built for a RudderStack internal hackathon. It consumes a Pulsar topic of browser-channel RudderStack events, evaluates hand-authored rules over per-user sliding-window aggregations, enriches trigger context with mock Activation API profiles and canned Kapa.ai knowledge, generates personalized actions via a canned-first LLM client, and dispatches to Slack webhooks or a mock email viewer — all streaming live to a Next.js dashboard via SSE.

> **Hackathon prototype — 12 h build.** Production hardening (multi-tenancy, ClickHouse hot path, CEL rules, live LLM, write-back to Activation API) is explicitly deferred. See §11 of the design doc for the production roadmap.

Canonical design doc: `/Users/kumar/.claude/plans/the-scope-of-this-sorted-flame.md`

---

## Local development

```bash
# 1. Copy env template and fill in required values
cp .env.example .env.local

# 2. Build the binary
make build

# 3. Apply database migrations (requires POSTGRES_DSN in env)
make migrate

# 4. Seed canned responses and reference data
make seed

# 5. Start the service
make serve

# 6. Fire a demo event sequence in a second terminal
make demo-fire-realestate
# or:
make demo-fire-rs-self

# Alternatively, publish events directly to Pulsar (skips the HTTP gateway).
# Requires PULSAR_URL, PULSAR_JWT_TOKEN, and PULSAR_TOPIC to be set in env.
./realtime-trigger demo-fire --persona realestate --target pulsar
# With an explicit write-key and a self-signed CA cert for a local broker:
./realtime-trigger demo-fire --persona realestate --target pulsar \
  --write-key 3DNyjJW7sRSqftUb1UQuMJdxlFw \
  --pulsar-tls-certs /path/to/ca.cert.pem
```

---

## Architecture overview

The service is a single Go binary (`realtime-trigger`) with four subcommands: `serve`, `seed`, `demo-fire`, and `migrate`. In `serve` mode it runs a tight pipeline on a single pod:

```text
             Demo control UI (Next.js)
                    │ REST + SSE
                    ▼
             fire-script API ──── POST /v1/batch ──► ingestion-svc
                                                          │
                                                          ▼
                                               Pulsar (StreamNative)
                                                          │ Shared sub
                                                          ▼
   ┌─────────────────────────────────────────────────────────────────┐
   │ realtime-events-ai-trigger-svc (Go, single-pod)                 │
   │                                                                 │
   │  consumer ──► filter/redact ──► window manager (aggregations)   │
   │                                        │                        │
   │  [idle ticker 1s] ──────────────► rule evaluator ──[trigger]─┐  │
   │                                                              │  │
   │  enricher: full events from PG + mock activation + canned    │  │
   │            Kapa lookup                                       │  │
   │                  │                                           │  │
   │  llm action generator (CannedClient reads canned_responses)  │  │
   │                  │                                           │  │
   │  dispatcher (Slack webhook | mock-email writer)              │  │
   │                  └──► SSE hub ──────► UI streams             │  │
   └─────────────────────────────────────────────────────────────────┘
                  │
                  ▼
       Postgres: events, configs, rules, triggers, cooldowns,
                 mock_profiles, canned_responses, canned_kapa_responses
```

Two demo personas are supported out of the box:

- **realestate** — real-estate listing browsing; triggers a Slack ping to an assigned realtor when a visitor session abandons after high-intent browsing.
- **rs-self** — RudderStack onboarding; triggers a personalized email suggestion when a user hits a destination setup error or goes idle mid-flow.
