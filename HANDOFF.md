# Realtime Events AI Triggers — Hackathon Handoff Snapshot

**Snapshot taken**: 2026-05-07 ~23:35 IST (rev 4)
**Status**: ready to demo to senior leadership. Service renamed to `realtime-events-ai-trigger-svc` (display: "Realtime Events AI Triggers"). Three rounds of engineer + reviewer pairs have polished the demo: SSE wire-shape alignment (`3ef416b`) → 7 demo-quality issues (`584ddd7`) → rename + 3 polish (`8d91d82`) → tab persistence + initial-fetch + race fix (next commit). Demo runbook lives at `docs/DEMO_RUNBOOK.md`.

## Demo polish round 5 (rev 5)

The wizard wasn't actually interactive — `handleGenerateConfig` returned the seed YAML verbatim and `handleActivateConfig` ignored `config_yaml`. Fixed:

- `handleGenerateConfig` parses `req.Answers` and applies them to the YAML via `gopkg.in/yaml.v3` round-trip. Realestate honors `idle_seconds` (number, also accepts string-of-digits) and `realtors` (textarea, parses "Name → suburb-1, suburb-2" lines). RS-self honors `idle_seconds` (drives `onboarding_stuck`) and `error_events` (narrows `onboarding_errored` to selected event names). Empty/nil answers → seed verbatim.
- `handleActivateConfig` with `config_yaml` parses + replaces rules in DB via new `db.ReplaceConfigRules`: NULLs `triggers.rule_id` FKs (preserves history), DELETE+INSERT rules, UPDATE `configs.config_yaml`. Atomic per-config tx.
- Engine reload via new `EngineReloader` callback in `api.Config`. Activate calls `engine.Reload(ctx)` after the DB commit so customizations go live in seconds, not 30s.
- Defensive validation: bad YAML → 400 (not 500). YAML missing `rules:` → 400 (would silently delete all rules). Reload errors are logged via `slog`; the user-facing response stays 200 (no transient warning surface needed).
- Tests: 8 new in `handlers_onboarding_test.go` (4 generate-config edge cases + 4 activate-config edge cases including bad YAML and rules-less). All gated tests pass with TEST_DATABASE_URL.

End-to-end via Playwright: edit `idle_seconds` from default 30 → 5 in wizard → YAML preview shows `idle_seconds: { '>=': 5 }` → Activate succeeds → DB confirms `spec.when.all` has idle_seconds=5 → Fire script → trigger fires with `window_snapshot.idle_seconds = 5` (not seed's 10). Wizard now drives the live rule engine.

Runbook §1.3 updated to walk through the live customization. New §9 covers cluster deployment with both `DEMO_FIRE_TARGET=pulsar` and `=http` modes for production-realistic event routing.

## Demo polish round 4 (rev 4)

User QA caught 5 more rough edges:

| # | Issue | Fix |
|---|---|---|
| Naming | Service didn't convey "events + rolling window" | Renamed go module + helm chart + display name to `realtime-events-ai-trigger-svc` / "Realtime Events AI Triggers". Pulsar subscription literal `realtime-ai-trigger-svc-v1` preserved (offset tracking) |
| Re-fire pollution | Re-clicking Fire piled events on top of stale window; Slack didn't fire (1h cooldown) | Auto-reset on Fire button click — `Controller.handleFireScript` calls `demoReset()` first, then fires. Failed reset is non-fatal |
| Same-content emails | Both rs-self rules used the same canned template | New `rs_destination_error` template wired to `onboarding_errored`; `rs_onboarding_stuck` stays on the other rule. Two distinct emails per rs-self run |
| Fish-shell incompat | `set -a; source .env.local; set +a` is bash-only | Python scripts auto-load `.env.local` via `python-dotenv`. Two-phase argparse: `--env-file` extracted via `parse_known_args` BEFORE full parse so defaults pick up env values |
| Tab switch wipes state | Radix Tabs unmounts inactive panels | `forceMount` on both `TabsContent` + 3 new GET endpoints (`/api/recent-events`, `/api/active-sessions`, `/api/recent-triggers`) for hydration on browser refresh. Initial-fetch uses epoch-ref to drop results that arrive after a `reset` SSE |
| Onboarding wizard not in demo path | Audience didn't see how rules get authored | Demo runbook now starts at `/onboarding`. Wizard already worked; just hadn't been documented |

## Demo-quality fixes (commit `584ddd7`, rev 3)

User QA round caught seven rough edges. All fixed:

| # | Issue | Root cause | Fix |
|---|---|---|---|
| 1 | Realestate trigger fired with `event_count=4` (and Slack canned text said "3 listings", contradicting dashboard) | All 8 script events shared the same `OriginalTimestamp` — captured once at script-build time; `LastSeen` never advanced; idle ticker fired at real-time T0+10s with only 4 events ingested | Firer re-stamps `OriginalTimestamp` + `SentAt` per send (local copy, not slice mutation); window uses authoritative `receivedAt` (server clock) for `LastSeen` |
| 2 | Live Events status stuck on "connecting" until first event | `connected` flag only flipped inside `onMessage`; `EventSource.onopen` was never wired | `lib/sse.ts` accepts `onOpen` callback; EventFeed wires `setConnected(true)` |
| 3 | Active session card had no persistent fired indicator | `triggeredIds` was auto-cleared after 1.2s | Keep `triggeredIds` until reset; static green "🎯 trigger fired" badge in SessionCard |
| 4 | Reset clears DB but dashboard cards persist | No reset signal flowed to frontend | `OnDemoReset` publishes `sse.EventReset` on all 4 streams; each column listens and clears in-memory state |
| 5 | Replay shows "Replay failed: no triggers yet" after reset | Generic error wrapping; backend message was OK but UX prefix was misleading | Frontend maps "no triggers" to "No triggers to replay yet — fire a script first" |
| 6 | Replay returned 200 but card never reappeared (separate bug found in QA) | Handler returned JSON only; no SSE re-publish | `handleReplayLastTrigger` now publishes the trigger to `sse.StreamTriggers` after DB fetch |
| 7 | Emails tab empty after triggers fire on Dashboard tab | EmailOutbox only subscribed to SSE on mount; no initial GET fetch | `EmailOutbox.tsx` calls `listMockEmails()` on mount with functional setState merge (de-duped by id) |

**E2E QA results (Playwright):** live status immediate / event_count=8 / 🎯 badges persist / reset clears 3 columns + emails tab / replay re-renders card / emails tab shows existing rows on switch / 0 console errors.

## Earlier rounds



This document is the canonical state-of-the-world after the live click-through smoke. It exists so the conversation can be `/compact`-ed without losing context. After compact, anyone (Claude or human) can read this file plus `PITCH.md` and resume.

---

## Live demo loop — what works RIGHT NOW

```text
Browser click "Fire Real-Estate"  →  POST /api/demo/fire-script
  →  internal/demofire publishes 8 events directly to local Pulsar (target=pulsar)
  →  consumer reads them via JWT-auth + TLS trust cert
  →  filter / window aggregations / archive into PG
  →  rules engine fires `realtor_session_abandoned` after 10s idle
  →  enricher pulls full events from PG + mock activation traits
  →  LLM canned response (`realestate_realtor_pitch`)
  →  dispatcher.SlackBackend POSTs Block Kit message
  →  #realestate-realtor-pings receives the realtor pitch
```

End-to-end latency: ~1.5 sec from idle threshold to Slack delivery.

Same engine, RS-self persona:
- 5 events fired (identify, Account Created, Source Created, Destination Setup Error with `AMP_INVALID_API_KEY`, page idle)
- `onboarding_errored` rule fires immediately on the error event
- Mock email rendered to `mock_emails` table (subject "Stuck on the Amplitude API key error? Here's the fix in 3 steps", body markdown, 3 doc links)

---

## Running services (local)

| Service | PID/container | URL | Notes |
|---|---|---|---|
| `realtime-trigger serve` | latest pid via `pgrep -f /tmp/realtime-trigger` | `http://localhost:8080` | binary at `/tmp/realtime-trigger`, built from this repo |
| Frontend (`pnpm dev`) | `pgrep -f 'next dev --port 3001'` | `http://localhost:3001` | env: `NEXT_PUBLIC_API_BASE=http://localhost:8080` |
| Postgres | docker container `rt-pg` | `localhost:5432` | `postuser/postpass/postdb` |
| Pulsar broker | docker container `pulsar-local-ssl` | `pulsar+ssl://localhost:6651` | JWT auth via `/pulsar/secrets/pulsar-jwt.key`, topic `persistent://public/enterprise/source-events-rudderstacvilo` |
| Local agent API (LLM) | port-forwarded by user | `http://localhost:3000/api/agent/stream` | seed-time use only; runtime is canned |

**To restart everything from scratch**: see "Restart recipe" at the bottom.

---

## Bugs found and fixed (chronological, with commit refs)

### Round 1–4 (subagent-built core)
- 8 commits from f9f5ba1 through 7b1a0a4 implementing WP-0…WP-H per `the-scope-of-this-sorted-flame.md`.

### Opus subagent code review delivered 60-finding punch list (GO-WITH-FIXES)
Top-3 fixed in commit `7a9cebe`:
- **H1**: synchronous Slack dispatch on hot path → async via 4-worker pool with 30s per-fire timeout + drop counter on backpressure
- **H2**: PG cooldown override of in-mem engine gate was silent → now logs `slog.Warn(reason="pg_cooldown_overrode_engine_gate")` + atomic counter
- **H3**: demo reset only truncated PG; in-mem engine cooldowns + window store stayed dirty → wired `runtime.OnDemoReset()` callback into `handleDemoReset` to call `engine.PurgeCooldowns()` + `windows.Reset()`

### Bug — Pulsar consumer was JWT-only, local broker is mTLS-then-JWT
- **Commit `0a591e6`**: added `TLSTrustCertsFile` / `TLSValidateHostname` / `TLSAllowInsecure` to `consumer.Config`; wired from `PULSAR_TLS_TRUST_CERTS` / `PULSAR_TLS_VALIDATE_HOSTNAME` / `PULSAR_TLS_ALLOW_INSECURE` env vars.

### Bug — frontend "Failed to fetch" on every API call (CORS)
- **Commit `281dff1`**: added permissive `corsAllowLocalhost` middleware to chi router. Reflects any localhost / 127.0.0.1 Origin (any port, http or https). Hackathon scope; production should narrow to deployed FE origin.

### Bug — `/api/demo/fire-script` posted to cluster ingestion-svc, not local Pulsar
- **Commit `f4948de`**: added `DEMO_FIRE_TARGET=pulsar|http` (default `pulsar`). When pulsar, `runtime.makeFireScript` instantiates `demofire.PulsarFirer` with the same env vars the consumer uses. M2 from the immediate review (hardcoded `SourceID="hackathon-local"`) was applied: now falls back to WriteKey for consistency with the CLI path.

### Bug — wizard "Activate" created empty config and demoted seeded one
- **Commit `8ec125c` (part A)**: new `db.ActivatePersonaSeededConfig` finds the oldest config with at least one enabled rule for the persona and atomically promotes it (deactivating siblings). `handleActivateConfig` rewritten to call it instead of inserting. Tests cover success + 404 (no rules) + 400 (bad request).

### Bug — dashboard SSE columns silently dropped all messages
- **Commit `8ec125c` (part B)**: backend producers emitted singular event names (`event`, `window`, `trigger`, `mock_email`) but frontend `useSSEStream` listens for plural stream names (`events`, `windows`, `triggers`, `mock_emails`). Replaced 4 literals with `sse.Stream*` constants from `internal/sse/hub.go`. New backend test asserts `event: events\n` on the wire.

### Bug — `.gitignore` silently ignored `cmd/realtime-trigger/` directory
- **Commit `8ec125c` (part C)**: `.gitignore` line `realtime-trigger` (no leading slash) was matching both the binary and the directory. Anchored to `/realtime-trigger`. Added 15 source files (~2373 LOC) to git for the first time. No secrets in the staged set; `.env.local` remains correctly ignored via `*.local`.

### Process bug — engineer subagent returned with the wrong cwd
- Tracked: my engineer prompts now always state the absolute repo path (`/Users/kumar/workspace/realtime-ai-trigger-svc`) explicitly; that was the friction in the first attempt at the M2 SourceID fix.

---

## Open issues (NOT demo-blocking, backlog post-pitch)

### Reviewer findings from the 3 engineer+reviewer pairs

**From Reviewer A (handleActivateConfig fix):**
- A-H1 — `ActivatePersonaSeededConfig` lacks `tenant_id` filter. Single-tenant demo unaffected. Multi-tenant prod = silent cross-tenant deactivation.
  - **Fix**: pass `tenantID` (defaulting to "default") into the query; add `AND c.tenant_id = $2` to the SELECT and both UPDATEs.
- A-H2 — TOCTOU between SELECT and transaction. Low-probability race; fix by moving SELECT inside the tx with `FOR UPDATE SKIP LOCKED`.
- A-M1 — legacy `id` branch returns 200 even when the row no longer exists. Pre-existing; not a regression.
- A-M2/M3 — test hygiene (`openTestPool` dead helper + `TEST_DATABASE_URL` guard on a no-DB code path).

**From Reviewer B (SSE event-name fix):**
- B-H1 — only `events` stream has a regression test; should add equivalents for `windows` / `triggers` / `mock_emails` (3 more tests, 1 each).
- B-M (window_pruned literal) — `runtime_pipeline.go:96` still publishes `Event: "window_pruned"` as a literal; same class of bug. Either add `sse.EventWindowPruned` constant or document the intentional literal.
- B-M (writeSSEMessage) — silently drops the `event:` line when `m.Event == ""`. Zero-value footgun. Should `panic` or log in non-prod builds.

**From Reviewer C (.gitignore fix):**
- C-M — Engineer A's modified files were bundled into Engineer C's git add (commit hygiene). Decision: accepted bundling for hackathon time pressure. Single commit covers all three fixes.

### Issue D — RESOLVED in commit `3ef416b`

**Original diagnosis (rev 1) was wrong.** The "Cannot update a component while rendering a different component" warning was a downstream symptom, not the cause. The real bug was a 4-stream backend-frontend wire-shape mismatch:

| Stream | Backend was emitting | Frontend expected (`frontend/types/api.ts`) | Failure mode |
|---|---|---|---|
| `events` | `{anonymous_id, event_type, event_name, pulsar_msg_id, received_at}` | `SSEEventPayload`: camelCase `{anonymousId, type, event, messageId, originalTimestamp, properties, ...}` | `event.payload.anonymousId.slice(-6)` → TypeError → React warning + Column 1 blank |
| `windows` | `{AnonymousID, EventCount, ...}` (PascalCase Go field names — Snapshot had no json tags) | `SSEWindowPayload`: snake_case `{anonymous_id, event_count, idle_seconds, ...}` | `if (payload.anonymous_id == null) return` → silent drop → Column 2 blank |
| `triggers` | missing `window_snapshot` field | `SSETriggerPayload` requires `window_snapshot` | TriggerCard reads `snapshot.event_count` → TypeError → Column 3 crashes when trigger fires |
| `mock_emails` | `{trigger_id, persona, subject, body_md}` (no id, wrong key body_md vs body_markdown) | `MockEmailPayload`: `{id, to_email, body_markdown, links, created_at}` | `if (!payload.id) return` → silent drop → Email tab empty |

**Fix scope** (commit 3ef416b, reviewed via Claude Opus subagent):
- `cmd/realtime-trigger/runtime_pipeline.go` — replaced `eventSummary` `map[string]any` with typed `sseEventDTO` carrying camelCase json tags. Used `sse.EventWindowPruned` constant instead of bare literal.
- `cmd/realtime-trigger/runtime_dispatch.go` — added `window_snapshot` + RFC3339 `fired_at` to trigger SSE; replaced mock_emails SSE body with full MockEmailPayload shape (id from dispatcher's `dispatchedURL` parsed via `strings.TrimPrefix("/api/mock-emails/")`); skip publish entirely on dispatch failure or empty emailID.
- `internal/window/snapshot.go` — added explicit snake_case json tags on every field; new `IdleSeconds int \`json:"idle_seconds"\`` field.
- `internal/window/store.go` — both `Snapshot()` AND `ScanIdle` now populate `snap.IdleSeconds = int(snap.IdleFor(now).Seconds())`. The latter was a reviewer-found follow-up bug — without it, the trigger that fires *because* of idle_seconds was reporting "Idle: 0s" on the dashboard.
- `internal/sse/hub.go` — added `EventWindowPruned = "window_pruned"` constant.
- `internal/api/api_test.go` — 4 new shape-assertion regression tests, one per stream.
- `internal/window/window_test.go` — regression test for ScanIdle idle_seconds.

**Verification (Playwright + Chrome, post-restart of frontend with cleaned `.next`)**:
- Real-estate fire: Live Events column streams 8 events (identify → page → 3 Listing Viewed → Filter Applied → 2 page); Active Sessions card shows `…re-001` with 7 events / 2 pages / event-name pills "Listing Viewed (3)" / "Filter Applied (1)"; Triggers Fired shows `realtor_session_abandoned` for realestate persona with full canned LLM card (headline, urgency=high, 3 talking points, CTA, "Priya N." badge). Slack message confirmed delivered.
- RS-self fire: 5 events with red "error" badge on the session card from `Destination Setup Error`; 2 triggers fired (`onboarding_errored` + `onboarding_stuck`); clicking "View email" inside the trigger card opens the email modal with full markdown body, From/To/Date headers, and 3 clickable doc links.
- 0 console errors throughout.

### Pre-existing frontend gaps (NOT demo-blocking; backlog)

- **Tab switch wipes state** — Radix Tabs unmounts panels when switching, so navigating Dashboard → Emails → Dashboard tears down both panels' SSE state (live events, sessions, triggers all reset). Demo flow stays on Dashboard so this doesn't matter; would matter if the demo wanted to project both tabs.
- **Emails tab has no initial fetch** — `EmailOutbox` only subscribes to SSE on mount; doesn't `GET /api/mock-emails`. So if user navigates to Emails AFTER triggers fire, the tab is empty even though DB has rows. Workaround for demo: use the in-trigger "View email" button on the rs-self trigger card (which embeds the email payload from the trigger SSE itself — works regardless of tab).
- **Frontend `.next` build can go stale** — happened during this session after the backend rebuild. Symptom: 404s on `_next/static/chunks/app/*.js`. Fix: `pkill -f 'next dev'; rm -rf frontend/.next; pnpm dev`. Documented in restart recipe below.

---

## Credentials & rotation list

⚠️ All shared in-chat — should be rotated after the hackathon:

| Credential | Where it lives now | Rotation priority |
|---|---|---|
| Slack OAuth tokens (`xoxe.xoxp-*`, `xoxe-*`) | shared in chat | **HIGH** — revoke at https://api.slack.com/apps |
| Slack incoming webhook URL | `.env.local` + helm values | low (rotation affects only the demo channel) |
| Pulsar admin JWT token | `/Users/kumar/workspace/pulsar-local-ssl/secrets/jwt-admin.token` | low (local broker only) |
| Pulsar service JWT token | `.env.local` (`PULSAR_JWT_TOKEN`) | low (local broker only) |
| Pulsar HS256 secret key | `/Users/kumar/workspace/pulsar-local-ssl/secrets/pulsar-jwt.key` | low (local broker only) |
| Bedrock 12h presigned key | shared in chat earlier | already TTL-expired |
| Local agent API bearer token | shared in chat | medium (rotate via the agent service) |
| Kapa.ai API key | shared in chat (`4U1oOrms...`) | medium |
| Harbor token (`vp2A6c...`) | shared in chat | high (in case any other repos used it) |
| Anthropic API key | not used (code is canned-mode) | n/a |

---

## What's still left for the live pitch

1. **Rehearsal × 3** with the full demo flow (onboarding → dashboard → Slack-projected). Time-box each rehearsal at 6 min.
2. **Fallback video recording** per persona (~45s each). Use QuickTime or Loom; record the Slack channel + dashboard side-by-side. Keep both videos in `~/Downloads/` for offline playback if the laptop loses network.
3. **4-slide pitch deck** rendered from `PITCH.md`. Either:
   - Slidev (`npx slidev`) — markdown-driven; preserves the structure of PITCH.md
   - Google Slides — manual but trivial; Soumyadeb will probably appreciate it
4. **Optional**: fix Issue D so the dashboard's visual flair works on stage. Backend already supports it. Engineer+reviewer pair to refactor `EventCard`'s setState pattern.

---

## Architecture decisions LOCKED (do not revisit)

- Pulsar (StreamNative cluster prod, local Docker for hackathon)
- Postgres for everything in hackathon (ClickHouse stays as production roadmap slide)
- Activation API mocked in-process per the official v1 wire shape
- LLM canned-first via PG; live mode via local-agent + Bedrock (`LLM_MODE=live`) post-demo
- Kapa.ai canned-first via PG; live mode via real API (`KAPA_MODE=live`)
- Demo-fire defaults to `--target pulsar` (`DEMO_FIRE_TARGET=pulsar`)
- Rules-first; LLM only at action generation time (cost ceiling)
- Aggregations-only window manager; full events fetched from PG at trigger fire time
- Two demo personas: real-estate (Slack out) + RS-self (mock email out)
- Frontend stack: Next.js 14 App Router + shadcn/ui + Tailwind + Framer Motion + native EventSource

---

## Restart recipe (full local stack from cold)

```bash
# 1. Postgres
docker run -d --name rt-pg -p 5432:5432 \
  -e POSTGRES_USER=postuser -e POSTGRES_PASSWORD=postpass -e POSTGRES_DB=postdb \
  postgres:16
until docker exec rt-pg pg_isready -U postuser -d postdb; do sleep 2; done

# 2. Pulsar (assumes /Users/kumar/workspace/pulsar-local-ssl is set up)
cd /Users/kumar/workspace/pulsar-local-ssl
docker compose up -d
# wait until 6651 accepts TLS

# 3. Build + migrate + seed
cd /Users/kumar/workspace/realtime-ai-trigger-svc
go build -o /tmp/realtime-trigger ./cmd/realtime-trigger
POSTGRES_DSN='postgresql://postuser:postpass@localhost:5432/postdb?sslmode=disable' \
  /tmp/realtime-trigger migrate
POSTGRES_DSN='postgresql://postuser:postpass@localhost:5432/postdb?sslmode=disable' \
  /tmp/realtime-trigger seed --from hand --seed-dir ./seed

# 4. Serve (with full env from .env.local)
set -a; source .env.local; set +a
POSTGRES_DSN='postgresql://postuser:postpass@localhost:5432/postdb?sslmode=disable' \
INGESTION_URL='https://rudderstacvilo.dev-rudder.rudderlabs.com' \
ALLOWED_WRITE_KEYS='3DNyjJW7sRSqftUb1UQuMJdxlFw,3DNyveG1sfuVHAV598ESyJza3i3' \
SLACK_WEBHOOK_URL='https://hooks.slack.com/services/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' \
LLM_MODE=canned KAPA_MODE=canned ACTIVATION_MODE=mock LOG_LEVEL=info \
DEMO_FIRE_TARGET=pulsar \
PULSAR_URL="$PULSAR_URL" PULSAR_TOPIC="$PULSAR_TOPIC" PULSAR_SUBSCRIPTION="$PULSAR_SUBSCRIPTION" \
PULSAR_JWT_TOKEN="$PULSAR_JWT_TOKEN" PULSAR_TLS_TRUST_CERTS="$PULSAR_TLS_TRUST_CERTS" \
PULSAR_TLS_VALIDATE_HOSTNAME="$PULSAR_TLS_VALIDATE_HOSTNAME" \
nohup /tmp/realtime-trigger serve > /tmp/rt-svc-logs/serve.log 2>&1 &

# 5. Frontend (always rm -rf .next when restarting backend — dev cache goes stale)
cd frontend
rm -rf .next
NEXT_PUBLIC_API_BASE=http://localhost:8080 nohup pnpm dev --port 3001 > /tmp/rt-svc-logs/frontend.log 2>&1 &

# 6. Smoke
curl -s -X POST http://localhost:8080/api/onboarding/activate -H 'Content-Type: application/json' -d '{"persona":"realestate"}'
curl -s -X POST http://localhost:8080/api/demo/reset
curl -s -X POST http://localhost:8080/api/demo/fire-script -H 'Content-Type: application/json' -d '{"persona":"realestate"}'
sleep 35
docker exec rt-pg psql -U postuser -d postdb -c "SELECT rule_name, dispatch_status, dispatched_at FROM triggers ORDER BY fired_at DESC LIMIT 1;"
# expect: realtor_session_abandoned | sent | <recent timestamp>
# also check #realestate-realtor-pings on Slack — message should be present
```

---

## Process protocol (current)

The user established a strict process during this session:

- **Code changes**: must go through a senior-engineer subagent (`loom-senior-software-engineer` or `loom-software-engineer` with model override). Main agent does **not** edit source files directly.
- **Code review**: every change goes through a **separate** reviewer subagent (typically `loom-senior-software-engineer`). Main agent doesn't self-review.
- **Codex CLI fallback**: if used, run with the recipe `export AZURE_FOUNDRY_KEY=$(security find-generic-password -s AZURE_FOUNDRY_KEY -w)` then `codex review --commit <sha>` (NO custom prompt — `--commit` is mutually exclusive with the prompt arg). 5-min timeout. If timeout twice, fall back to a Claude Opus 4.7 reviewer subagent.
- **Subagent restrictions**: no git commit/add/anything destructive; main agent owns git operations.

---

## Where to look first when resuming

1. Read `the-scope-of-this-sorted-flame.md` (canonical design)
2. Read `PITCH.md` (4-slide deck draft + 90s demo scripts + risk buffers + Q&A soundbites)
3. Read THIS FILE (current state + open issues + restart recipe)
4. `git log --oneline | head -10` to see commit history
5. `docker ps` to see what's running
6. `pgrep -f /tmp/realtime-trigger` and `pgrep -f 'next dev'` for live processes
7. To resume the demo loop: just execute the smoke commands at the bottom of the restart recipe

---

*draft v1; refine after rehearsals*
