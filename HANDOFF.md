# Realtime AI Trigger Service — Hackathon Handoff Snapshot

**Snapshot taken**: 2026-05-07 ~18:20 IST
**Status**: backend pipeline fully functional end-to-end (Pulsar → consumer → window → rules → LLM canned → Slack/email). Frontend dashboard has a known SSE-rendering gap (visual only; doesn't block the demo).

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

### Demo-relevant frontend gap (not yet fixed)

**Issue D — Dashboard 3-column SSE rendering blocked by React setState-during-render in `EventCard`.**
- The backend now emits the correct `event: events` wire format (verified via curl: 9 records with the new name flowed during the smoke).
- The frontend listener (`frontend/lib/sse.ts:112-131`) registers `addEventListener("events", ...)` — should fire.
- But the browser console shows 6 `Cannot update a component while rendering a different component` warnings stemming from `EventCard` (Framer Motion + AnimatePresence interaction) and the columns stay at "Waiting for events…" / "0".
- This is a pre-existing frontend bug (probably from WP-H), unrelated to today's SSE fix — but it's the reason the dashboard's visual flair doesn't work.
- **Workaround for the demo**: project the Slack channel directly. The audience sees the Slack message land within ~2s of clicking Fire — that's the wow moment. Dashboard remains a slide-only architectural diagram.
- **Real fix (post-demo)**: refactor `EventCard` so the AnimatePresence onExitComplete handler doesn't call setState during render. Engineer + reviewer pair when time permits.

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

# 5. Frontend
cd frontend
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
