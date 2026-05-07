# Realtime Events AI Triggers — Demo Runbook

**Audience**: presenter (you) + senior leadership (audience). Shell-agnostic (bash / zsh / fish). Total runtime ~6 minutes.

The demo follows the natural narrative arc: **author rules** (onboarding wizard) → **observe events** (live dashboard) → **see actions fire** (Slack + email). Use cohort/multi-user scripts after the single-user flow if you have time.

---

## §0 Pre-flight (30 seconds)

```bash
# All four services must be alive
docker ps --filter name=rt-pg --filter name=pulsar-local-ssl --format '{{.Names}} {{.Status}}'
pgrep -fl /tmp/realtime-trigger
pgrep -fl 'next dev --port 3001'
curl -s http://localhost:8080/healthz
```

**Expected**: 2 docker containers `Up`, 2 process matches, `{"started":"...","status":"ok"}`.

If anything's missing → §6 cold-start. Otherwise continue.

> **One-time activation per persona** — the seed loaders auto-run on first start, but the wizard's "Activate" call promotes a persona's seeded config. We'll do this through the wizard in §1.

---

## §1 Demo Flow A — Author the rules via the onboarding wizard (~60s)

This is the audience's *first impression*: how a non-engineer would set up the system.

### 1.1 Open the wizard

Chrome → **http://localhost:3001/onboarding**

**QA checks on first paint:**
- [ ] Header reads "**RudderStack** Realtime Events AI Triggers"
- [ ] Three-step progress nav at top: **Choose persona** / **Configure rules** / **Preview & activate**
- [ ] Step 1 is highlighted; two persona cards: **Real-estate Portal** + **RudderStack Onboarding**
- [ ] Right panel reads "Tracking Plan / Select a persona to view its tracking plan"
- [ ] Console: 0 errors

### 1.2 Pick Real-estate Portal

Click the **Real-estate Portal** card.

**QA checks:**
- [ ] Right panel populates with the tracking plan — events the system will listen for (Listing Viewed, Filter Applied, page, identify) with property schemas
- [ ] **Next** button enables

Click **Next** → step 2 (Configure rules).

### 1.3 Configure & generate

Step 2 shows pre-filled questions:
- **Realtors per suburb** (textarea, format `Name → suburb-1, suburb-2`)
- **Price range / typical hot leads** (text — display only, not used in any rule)
- **Idle seconds before abandoned** (number, default 30)

> **Demo talking point**: "These answers actually drive the YAML on the next page. If leadership asks 'what if we want to fire faster?', I can change idle_seconds here and re-generate."

**Live customization (recommended for the live demo)**: change `Idle seconds` from 30 → **5** to make the trigger fire faster (we'll see this fire at ~27s instead of ~32s). Optionally edit a realtor name to prove the textarea is wired.

Click **Generate config**.

**QA checks:**
- [ ] Page advances to step 3 (Preview & activate)
- [ ] Heading: "Your generated config"
- [ ] YAML preview shows `realtor_session_abandoned` rule with `idle_seconds: { '>=': <your value> }` reflecting your input (NOT the seed default of 10)
- [ ] If you edited realtors, the YAML's `realtors:` block reflects the new entries
- [ ] **Activate & continue** button visible

### 1.4 Activate

Click **Activate & continue**.

**QA checks:**
- [ ] URL changes to `http://localhost:3001/dashboard`
- [ ] Dashboard renders: 3 columns + Emails tab + floating Demo Controls
- [ ] Live Events column shows status `live` immediately (NOT `connecting`)
- [ ] Audience now sees the *system listening for events* with the rule armed

What just happened: the wizard's "Activate" called `POST /api/onboarding/activate` with the customized `config_yaml`. The backend parsed it, replaced the rules in Postgres in a single transaction (NULLing trigger FKs first to preserve history), and called `engine.Reload(ctx)` so the new rule is live IMMEDIATELY (no 30s wait for the periodic reload tick).

You can prove the customization persisted with:
```bash
docker exec rt-pg psql -U postuser -d postdb -c \
  "SELECT spec->'when'->'all' FROM rules r JOIN configs c ON r.config_id=c.id WHERE c.persona='realestate' AND c.active=true LIMIT 1;"
# Expect: idle_seconds reflects the value YOU just typed in the wizard
```

> **Demo talking point**: "We just authored a real-time trigger via a 3-step wizard. No code, no SQL. Whatever I just typed is now live in the rules engine — when we fire events in the next step, this is the threshold that decides if the rule matches."

### 1.5 Bad input handling (optional, for showing robustness)

If asked "what if I submit garbage?", the activate endpoint validates user input strictly:
- Malformed YAML → `400 invalid config_yaml: ...`
- YAML missing the `rules:` field → `400 config_yaml must contain at least one rule`

(Engine reload errors are logged but never surfaced to the wizard — the user sees success and the rule remains stale; operators can `tail /tmp/rt-svc-logs/serve.log` for warnings.)

---

## §2 Demo Flow B — Real-estate session abandonment → Slack (~35s)

### 2.1 Fire the demo script

Click the violet **Real-estate** button on the floating Demo Controls.

**Auto-reset note**: each Fire click first calls `/api/demo/reset` to clear DB + cooldowns + dashboard React state, THEN runs the script. So you can re-fire as many times as you want without manually clicking Reset between runs. Status banner briefly reads "Resetting state…" before "Fired N events — fired".

**QA timeline (~32 seconds total):**

| Time | What you should see |
|---|---|
| 0–2s | Live Events: `identify` card; Active Sessions card `…re-001`, events: 1 |
| 2–5s | `(page)` card; events: 2, pages: 1 |
| 5–9s | `Listing Viewed` card |
| 9–13s | `Filter Applied` card |
| 13–21s | Two more `Listing Viewed` cards. Active Sessions: `Listing Viewed (3)` |
| 20–22s | Two `(page)` cards on `/listings/L112` (the dwell). Pill `Filter Applied (2)` |
| ~32s | **Trigger fires.** Card lands in Column 3: `realtor_session_abandoned · realestate`. Active Session card sprouts a persistent **🎯 trigger fired** badge with emerald border |

### 2.2 QA the trigger card

In Column 3 click `▾ Why`:
- [ ] `Events: 8`, `Idle: 10s`, `Top events: Listing Viewed (3), Filter Applied (2)` — these MUST match the Active Session card counts (no partial-snapshot regression)

Click `▾ Decision`:
- [ ] Headline: "Anonymous visitor abandoned a high-intent session in Suburb 1"
- [ ] Urgency badge: red `high`
- [ ] 3 talking points (L101/L107/L112, beds_min=3, 22-second L112 dwell)
- [ ] CTA: "Call within 30 minutes; lead with L112…"
- [ ] Realtor badge: `Priya N.`

Click `▾ Delivered`:
- [ ] Green `sent` badge + `slack:realestate-realtor-pings`

Open **Slack** → `#realestate-realtor-pings`:
- [ ] Block Kit message arrived within ~2s of the trigger card

### 2.3 SQL sanity (optional)

```bash
docker exec rt-pg psql -U postuser -d postdb -c \
  "SELECT rule_name, dispatch_status, (window_snapshot->>'event_count')::int AS events, (window_snapshot->>'idle_seconds')::int AS idle FROM triggers ORDER BY fired_at DESC LIMIT 1;"
```

Expected:
```
         rule_name         | dispatch_status | events | idle
---------------------------+-----------------+--------+------
 realtor_session_abandoned | sent            |      8 |   10
```

> **Demo talking point**: "From the user idling on the listing detail to the realtor's Slack channel: ~1.5 seconds end-to-end."

---

## §3 Demo Flow C — Tab persistence + RS-self → mock email (~30s)

The audience now sees a *different rule* fire on a *different persona*, ending in a different action type (email).

### 3.1 Verify tab persistence (Bug fix from QA round)

Click the **Emails** tab → switch back to **Dashboard** tab.

**QA checks:**
- [ ] Active Sessions still shows the `…re-001` card with the persistent 🎯 fired badge (state survives tab switch — `forceMount` keeps both panels mounted)
- [ ] Triggers Fired column still shows the realtor_session_abandoned card
- [ ] Live Events may have aged out (TTL=30s) — that's fine

### 3.2 Fire RS-self

Click the blue **RS-self** button.

Auto-reset clears the realestate state. Dashboard returns to empty briefly, then RS-self events stream in.

**QA timeline (~22s total):**

| Time | What you should see |
|---|---|
| 0–4s | `identify` then `Account Created` cards. Active Session `…rs-001`, events: 2 |
| 5–10s | `Source Created` card. Pill: `Source Created (1)` |
| 9–12s | **`Destination Setup Error` card** with red error icon. Active Session: red `error` badge. **`onboarding_errored` trigger card lands ~1s later**, 🎯 badge persists |
| 12–14s | A `(page)` card (user lingering) |
| ~22s | **Second trigger** (`onboarding_stuck`) lands. The two trigger cards have DIFFERENT canned content (post-fix) |

### 3.3 QA the two distinct emails

Both rs-self trigger cards have a "View email" button. Click each — the modals should differ:

| Trigger | Email subject | Email focus |
|---|---|---|
| `onboarding_errored` | "Quick fix: Amplitude API key error in your destination setup" | 3 numbered root causes for the AMP_INVALID_API_KEY error specifically |
| `onboarding_stuck` | "Stuck on the Amplitude API key error? Here's the fix in 3 steps" | More general "stuck on Amplitude setup" re-engagement flow |

Different subjects, different bodies, different sign-offs. Same canned-LLM mechanism producing TWO different rule-specific responses.

### 3.4 Emails tab

Click **Emails** tab.

**QA checks:**
- [ ] Mock Emails count: `2`
- [ ] Both emails listed with their distinct subjects (newest first)
- [ ] Click a card → modal opens with rendered markdown body, From/To/Date headers, doc links

The Emails tab now hydrates from `GET /api/mock-emails` on mount, so even if you opened it for the first time AFTER the triggers fired, the existing rows are shown.

> **Demo talking point**: "Same engine, different rule, different action template. The framework is policy-as-data — adding a new use case is a YAML edit, not a code change."

---

## §4 Demo Flow D — Multi-user cohort (Python toolkit, ~35s)

The "this scales" moment. Three concurrent users abandoning sessions, three independent triggers, three Slack messages.

### 4.1 First-time only: install Python deps

```bash
cd /Users/kumar/workspace/realtime-ai-trigger-svc/scripts/demo_events
uv sync
```

### 4.2 Run the cohort

Shell-agnostic — `.env.local` auto-loads (`python-dotenv`). No `set -a; source ...` needed.

```bash
# bash / zsh / fish — all the same
cd /Users/kumar/workspace/realtime-ai-trigger-svc/scripts/demo_events
uv run demo_realestate.py --cohort-size 3 --seed 42 -v
```

This publishes 3 concurrent realestate user sessions to local Pulsar with deterministic `--seed 42` (same anonymous IDs every run).

**QA timeline (back on Dashboard tab):**

| Phase | Dashboard |
|---|---|
| 0–22s | Live Events fills with cards from 3 different `…<hex8>` anonymous IDs. Active Sessions count climbs 0 → 1 → 2 → 3 |
| 22–32s | Cards keep flowing. Idle counters tick on session cards |
| ~32s | **Three trigger cards** land in Column 3. Each session card gets the persistent 🎯 fired badge |

**QA checks:**
- [ ] Active Sessions: `3`
- [ ] Triggers Fired: `3`
- [ ] Three distinct anonymous-id suffixes
- [ ] Slack received 3 separate Block Kit messages

### 4.3 SQL sanity

```bash
docker exec rt-pg psql -U postuser -d postdb -c \
  "SELECT rule_name, anonymous_id, dispatch_status FROM triggers ORDER BY fired_at DESC LIMIT 5;"
```

Expected: 3 rows, all `realtor_session_abandoned`, distinct `anonymous_id`, all `sent`.

### 4.4 Stress test (optional)

```bash
uv run demo_combined.py --realestate-cohort 8 --rs-self-cohort 4 --total-duration 30
```

12 concurrent users, two personas. Verify console has no errors and `tail -50 /tmp/rt-svc-logs/serve.log | grep -E 'match_dropped|archive channel full'` is empty.

---

## §5 Resilience flows (optional)

### 5.1 Browser refresh — Replay

1. Press `⌘⇧R` (or Ctrl+Shift+R) on the dashboard.
2. The 3 columns reset to empty (browser state lost) but the *backend has all the state*.
3. Click the floating **🔄 Replay** icon.
4. Status: "Last trigger replayed — see Triggers Fired column"
5. The most recent trigger card re-renders in Column 3 from the DB.

> **Demo talking point**: "Triggers persist. The dashboard is just one consumer of the SSE event stream — losing it doesn't lose data."

### 5.2 Hard navigate to /dashboard — initial fetch

Open `http://localhost:3001/dashboard` in a fresh tab (or after a server restart). The columns auto-populate via the new `/api/recent-events`, `/api/active-sessions`, `/api/recent-triggers` endpoints — no Replay click needed for the most-recent-data view.

### 5.3 Reset confirmation

Click the trash-can **Reset** icon → **Reset** in the dialog.

- [ ] All 3 columns clear immediately (`reset` SSE signal flows to every column)
- [ ] Emails tab also clears
- [ ] Click **🔄 Replay** → "No triggers to replay yet — fire a script first" (friendly message, not "Replay failed:")

---

## §6 Cooldown tweak (knob you may need on stage)

The realestate rule has `cooldown_seconds: 3600` (1 hour). Same anonymousId firing the rule twice within an hour gets the second fire suppressed. Auto-reset on Fire bypasses this *between demos*, but if you want to demo the cooldown itself OR rapid-fire without auto-reset, lower it temporarily.

```bash
# 1. Edit the persona config
sed -i '' 's/cooldown_seconds: 3600/cooldown_seconds: 30/' \
  /Users/kumar/workspace/realtime-ai-trigger-svc/seed/persona-configs/realestate.yaml

# 2. Re-seed (UPSERT updates the rule row)
POSTGRES_DSN='postgresql://postuser:postpass@localhost:5432/postdb?sslmode=disable' \
  /tmp/realtime-trigger seed --from hand \
  --seed-dir /Users/kumar/workspace/realtime-ai-trigger-svc/seed

# 3. Restart backend (rules are cached at startup)
pkill -f /tmp/realtime-trigger
# ...full env-prefixed restart per §7 cold-start...

# 4. Reset (clears any active cooldown rows from prior fires)
curl -s -X POST http://localhost:8080/api/demo/reset >/dev/null
```

Restore the 1h cooldown after the demo:
```bash
sed -i '' 's/cooldown_seconds: 30/cooldown_seconds: 3600/' \
  /Users/kumar/workspace/realtime-ai-trigger-svc/seed/persona-configs/realestate.yaml
# re-seed + restart
```

The rs-self rules use `cooldown_seconds: 86400` (24h) — same procedure if you need to lower them.

---

## §7 Cold-start (only if §0 fails)

```bash
# 1. Postgres
docker start rt-pg 2>/dev/null || docker run -d --name rt-pg -p 5432:5432 \
  -e POSTGRES_USER=postuser -e POSTGRES_PASSWORD=postpass -e POSTGRES_DB=postdb postgres:16
until docker exec rt-pg pg_isready -U postuser -d postdb >/dev/null 2>&1; do sleep 1; done

# 2. Pulsar (local Docker, JWT + TLS)
cd /Users/kumar/workspace/pulsar-local-ssl
docker compose up -d
until docker compose logs broker 2>/dev/null | grep -q 'Created TLS service'; do sleep 2; done

# 3. Build + migrate + seed (only on first run, or after schema/seed changes)
cd /Users/kumar/workspace/realtime-ai-trigger-svc
go build -o /tmp/realtime-trigger ./cmd/realtime-trigger
POSTGRES_DSN='postgresql://postuser:postpass@localhost:5432/postdb?sslmode=disable' \
  /tmp/realtime-trigger migrate
POSTGRES_DSN='postgresql://postuser:postpass@localhost:5432/postdb?sslmode=disable' \
  /tmp/realtime-trigger seed --from hand --seed-dir ./seed

# 4. Backend (bash/zsh syntax shown — fish equivalent below)
mkdir -p /tmp/rt-svc-logs
set -a; source .env.local; set +a   # bash/zsh only
POSTGRES_DSN='postgresql://postuser:postpass@localhost:5432/postdb?sslmode=disable' \
INGESTION_URL='https://rudderstacvilo.dev-rudder.rudderlabs.com' \
ALLOWED_WRITE_KEYS='3DNyjJW7sRSqftUb1UQuMJdxlFw,3DNyveG1sfuVHAV598ESyJza3i3' \
SLACK_WEBHOOK_URL='https://hooks.slack.com/services/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' \
LLM_MODE=canned KAPA_MODE=canned ACTIVATION_MODE=mock LOG_LEVEL=info \
DEMO_FIRE_TARGET=pulsar \
nohup /tmp/realtime-trigger serve > /tmp/rt-svc-logs/serve.log 2>&1 &
sleep 3 && curl -s http://localhost:8080/healthz | jq .
```

For **fish shell**, replace the `set -a; source .env.local; set +a` line with:
```fish
for line in (grep -v '^#' .env.local | grep '=')
    set -gx (string split -m 1 '=' -- $line)
end
```

Or skip env loading entirely — only the Python toolkit auto-loads `.env.local`. The Go backend takes its env from the explicit `VAR=...` prefix on the launch line above.

```bash
# 5. Frontend (always rm -rf .next on backend rebuild — dev cache goes stale)
cd frontend
pkill -f 'next dev' 2>/dev/null
rm -rf .next
NEXT_PUBLIC_API_BASE=http://localhost:8080 nohup pnpm dev --port 3001 \
  > /tmp/rt-svc-logs/frontend.log 2>&1 &
sleep 8 && curl -sI http://localhost:3001/dashboard | head -1   # HTTP/1.1 200 OK

# 6. Python toolkit (one-time install)
cd scripts/demo_events
uv sync
```

---

## §8 Troubleshooting matrix

| Symptom | Likely cause | Fix |
|---|---|---|
| Dashboard stuck `connecting` | Stale Next dev cache | `pkill -f 'next dev'; rm -rf frontend/.next; pnpm dev` then hard-reload |
| `Failed to fetch` on every API call | Backend down or CORS broken | `curl http://localhost:8080/healthz`; restart backend if needed |
| Trigger fires with `event_count < 8` | Regression of timestamp fix | Verify backend at commit `8d91d82+`; check `git log --oneline -5` |
| Active session card never gets 🎯 badge | Frontend not on commit `8d91d82+` | Pull latest + rebuild frontend |
| Cohort users all share one anonymousId | Faker race / `--seed` not honored | Verify Python toolkit at commit `8d91d82+` |
| Tab switch wipes Dashboard cards | `forceMount` regression | Verify `frontend/app/dashboard/page.tsx` has `forceMount` on both `TabsContent` |
| Slack message missing | Rate limit OR webhook expired | Test webhook: `curl -X POST $SLACK_WEBHOOK_URL -d '{"text":"ping"}'`; check `dispatch_status` in `triggers` |
| Both rs-self emails identical content | Old seed loaded — check `canned_responses` table | `docker exec rt-pg psql -U postuser -d postdb -c "SELECT template_name FROM canned_responses WHERE persona='rs-self';"` should return BOTH `rs_destination_error` and `rs_onboarding_stuck`. If only one, re-seed. |
| Re-fire doesn't produce new Slack | Cooldown active (1h default) | Reset clears cooldowns. Or lower `cooldown_seconds` per §6. |
| `event_count` keeps growing across fires | Auto-reset on Fire didn't run | Check `/api/demo/reset` returns 200 in DevTools network tab. Possibly stale frontend bundle — rebuild. |
| `pkill -f /tmp/realtime-trigger` doesn't restart cleanly | Pulsar Shared subscription holding | Wait 10s after kill before restart |
| Python script: `PULSAR_JWT_TOKEN is not set` | `.env.local` not at repo root or `scripts/demo_events/` | Pass `--env-file PATH` explicitly |
| Python script: TLS cert error | `PULSAR_TLS_TRUST_CERTS` path wrong | Set absolute path in `.env.local` to `/Users/kumar/workspace/pulsar-local-ssl/certs/ca.cert.pem` |

---

## §9 Cluster deployment — sending events to ingestion-svc (post-deploy)

The local demo publishes events directly to a local Pulsar broker (`DEMO_FIRE_TARGET=pulsar`). When the service is deployed in your namespace, you have two options for routing events:

### Option A — Direct-to-Pulsar (same as local)

Useful if your deployed Pulsar is the production StreamNative cluster and you want the demo to bypass the ingestion-svc round-trip.

Helm `values.yaml` overrides:
```yaml
env:
  PULSAR_URL: "pulsar+ssl://pc-0148c683.aws-use1-prod-3u4fn.aws.snio.cloud:6651"
  PULSAR_TOPIC: "persistent://public/enterprise/source-events-rudderstacvilo"
  DEMO_FIRE_TARGET: "pulsar"
secretEnv:
  PULSAR_JWT_TOKEN: <streamnative-jwt>   # via --set
```

`/api/demo/fire-script` and the Python `demo_*.py --target pulsar` scripts both publish directly. The deployed backend's consumer reads from the same topic. Latency: ~1 sec from event publish → trigger fire → Slack.

### Option B — Through ingestion-svc (production-realistic)

This routes the demo events through the real ingestion-svc, exercising the full RudderStack data plane (auth → bot detection → Pulsar). Use this if leadership asks "is this how it'd actually look in production?".

Helm `values.yaml` overrides:
```yaml
env:
  INGESTION_URL: "https://rudderstacvilo.dev-rudder.rudderlabs.com"
  DEMO_FIRE_TARGET: "http"           # ← key difference
  ALLOWED_WRITE_KEYS: "<your-workspace-write-keys-csv>"
  PULSAR_URL: "pulsar+ssl://..."     # consumer still reads from Pulsar
  PULSAR_TOPIC: "persistent://public/enterprise/source-events-rudderstacvilo"
secretEnv:
  PULSAR_JWT_TOKEN: <streamnative-jwt>
```

When `DEMO_FIRE_TARGET=http`, `/api/demo/fire-script` POSTs `{batch:[...]}` to `${INGESTION_URL}/v1/batch` with HTTP Basic auth (writeKey as username, empty password). Ingestion-svc validates, applies bot detection, publishes to its Pulsar topic. Your deployed consumer subscribes to the same topic and processes normally. Latency adds a hop (~50-200 ms more).

### Python toolkit on the cluster

The Python scripts have the same `--target` flag:
```bash
# From your laptop, hitting the deployed ingestion-svc
cd scripts/demo_events
uv run demo_realestate.py --target http --ingestion-url https://rudderstacvilo.dev-rudder.rudderlabs.com -v

# Or set the env var
INGESTION_URL=https://rudderstacvilo.dev-rudder.rudderlabs.com uv run demo_realestate.py --target http -v
```

The HTTP firer uses the persona's hard-coded `--write-key` (overridable via `WRITE_KEY_REALESTATE` / `WRITE_KEY_RS_SELF` env or `--write-key`). For the deployed setup, plug in your workspace's writeKeys.

### Switching modes at runtime

`DEMO_FIRE_TARGET` is read at backend startup. To switch modes without a redeploy, restart the pod. For a cleaner UX, the Python toolkit can flip `--target` per invocation without touching the backend.

### Sanity check post-deploy

After deploy:
```bash
# Health check
curl https://<your-deployed-host>/healthz
# Expect: {"started":"...","status":"ok"}

# Stream subscription works (long-running)
curl -sN https://<your-deployed-host>/api/streams/triggers
# Expect: ": connected\n\n" then heartbeats every 15s

# Fire a script (will publish via DEMO_FIRE_TARGET path)
curl -X POST https://<your-deployed-host>/api/demo/fire-script \
  -H 'Content-Type: application/json' -d '{"persona":"realestate"}'

# Wait ~32s, check trigger fired
curl -s https://<your-deployed-host>/api/recent-triggers?limit=5 | jq -r '.triggers[].rule_name'
# Expect: realtor_session_abandoned
```

If `dispatch_status=failed` for Slack on the deployed cluster, check egress firewall rules — your namespace may not have outbound HTTPS to `hooks.slack.com`. Mock email destination has no external dependencies.

---

## §10 Reset between rehearsals

```bash
# Clears DB events/triggers/mock_emails/cooldowns + in-memory window store + cooldown gate
# Frontend dashboard auto-clears via the `reset` SSE signal — no refresh needed
curl -s -X POST http://localhost:8080/api/demo/reset | jq .
```

The dashboard's three columns + Emails tab clear themselves within ~50ms.

---

## §11 What "passing the demo" looks like

End-to-end without:
- Any console errors
- Any partial trigger fires (`event_count` always matches Active Sessions)
- Any stuck "connecting" state
- Any UI element not refreshing on Reset
- Any divergence between trigger card "Why" panel and the Slack/email content
- Any duplicate or missing email content

If you hit anything weird: screenshot it, capture `tail -100 /tmp/rt-svc-logs/serve.log`, and ping me — I'll spawn an engineer + reviewer pair on the regression.
