# Implementation Plan — Demo Maturity

**Companion spec**: `docs/specs/2026-05-08-demo-maturity-design.md`
**Mode**: Autonomous main-agent execution while user sleeps. No user input requested mid-flight.
**Hard cutoff**: 6 hours wall-clock from approval.
**Repo**: `/Users/kumar/workspace/realtime-ai-trigger-svc`
**Working branch**: `main` (current). No new branch — commits land directly on the working branch and push to GitHub at the end.

---

## 0. Pre-flight (main agent)

Before spawning any subagent:

1. `cd /Users/kumar/workspace/realtime-ai-trigger-svc`
2. `git status` — confirm clean working tree (no uncommitted changes from prior work).
3. `bash scripts/start-backend-local.sh --foreground` in a separate background process via `run_in_background: true`, OR ensure backend is already running. (Backend must be reachable at `localhost:8080` for some Round 1 acceptance tests.)
4. Verify Postgres + Pulsar are running (`pg_isready -h localhost -p 5432`).
5. Mark a chapter: `mcp__ccd_session__mark_chapter` with title "Demo maturity execution".

If any pre-flight check fails, fix the underlying issue before spawning subagents (do not paper over with `|| true`).

---

## 1. Round 1 — three parallel subagents (Backend)

Spawn all three in **one message** with three Task tool calls. Each subagent uses the `loom-software-engineer` subagent type.

### 1.1 WP-A subagent prompt

```text
** READ docs/specs/2026-05-08-demo-maturity-design.md FIRST. **
Read §3, §4, §5.2-§5.4 in particular before writing any code.

⛔ SUBAGENT RESTRICTIONS - YOU ARE A SUBAGENT:
- NEVER run git commit - the main agent will commit your work
- NEVER run loom stage complete - this is not a loom plan
- NEVER run git add -A or git add . - the main agent handles staging
- DO write code, run tests locally, and report results back

YOUR WORK PACKAGE: WP-A — Backend data + realtor roster expansion
Authoritative spec sections: §4.2, §4.3, §4.4, §5.2, §5.4
Files you OWN (write):
  - seed/mock_profiles.yaml (full replacement — 8 realestate + 3 rs-self)
  - seed/canned-responses-hand.yaml (rewrite four templates with placeholders)
  - seed/persona-configs/realestate.yaml (split into two rules; expand realtor roster with phone numbers; lower idle_seconds 10→8)
  - seed/persona-configs/rs-self.yaml (no rule changes; only confirm template names align with WP-A's canned-responses)
  - internal/rules/loader.go (add Phone string field on RealtorEntry; preserve existing fields and YAML/JSON tags)
Files READ-ONLY:
  - rest of repo (especially internal/rules/predicates.go to understand existing predicates)

DO NOT TOUCH:
  - internal/rules/predicates.go (no new predicates needed; spec uses existing traits.known)
  - internal/rules/expr.go (Not is already implemented)
  - internal/window/, internal/dispatch/, internal/demofire/, internal/api/, frontend/, cmd/

Detailed task:
1. Replace seed/mock_profiles.yaml entirely with the 8 realestate profiles + 3 rs-self profiles per spec §4.2. Use the FIXED rotation order (anon_demo-re-001 through anon_demo-re-008, demo-rs-001 through demo-rs-003). For each rs-self profile, emit BOTH a user_id-typed entry AND an anonymous_id-typed entry with the same id_value (per spec §4.2 last paragraph). Profile #3 (anon_demo-re-003) and Profile #8 (anon_demo-re-008) are ANONYMOUS — do NOT add them to the YAML at all (not even with empty traits). Profile #6 (anon_demo-re-006) is partial — has email + first_name + last_name + propensity_score only.

2. Rewrite seed/canned-responses-hand.yaml with FOUR templates per spec §5.2: realestate_realtor_pitch (modified, KNOWN), realestate_realtor_anonymous (NEW), rs_destination_error (modified), rs_onboarding_stuck (modified). Use the verbatim template text from §5.2. Preserve the canned_kapa block at the end unchanged.

3. Rewrite seed/persona-configs/realestate.yaml per spec §4.4: TWO rules (realtor_known_high_intent and realtor_anonymous_high_intent), expanded realtor roster with phone numbers per §5.4, slack_channel preserved.

4. Update seed/persona-configs/rs-self.yaml: change the action_template references to use rs_destination_error and rs_onboarding_stuck (these names already match what's there — confirm and only edit if drift is found). NO other changes.

5. Add the Phone field to RealtorEntry in internal/rules/loader.go:
   ```go
   type RealtorEntry struct {
       Name    string   `yaml:"name" json:"name"`
       Phone   string   `yaml:"phone,omitempty" json:"phone,omitempty"`  // NEW
       Suburbs []string `yaml:"suburbs" json:"suburbs"`
       Hours   string   `yaml:"hours,omitempty" json:"hours,omitempty"`
   }
   ```
   That's the entire Go change for WP-A.

6. Verify your work:
   - `cd /Users/kumar/workspace/realtime-ai-trigger-svc && go build -o /tmp/realtime-trigger ./cmd/realtime-trigger`
   - `go test ./internal/rules/...` (existing tests must still pass with the Phone field added)
   - `yq . seed/canned-responses-hand.yaml > /dev/null` (parses)
   - `yq . seed/mock_profiles.yaml > /dev/null` (parses)
   - `yq . seed/persona-configs/realestate.yaml > /dev/null` (parses)
   - `/tmp/realtime-trigger seed --from hand --seed-dir ./seed` runs without error
   - `psql -d postdb -c "SELECT id_value FROM mock_profiles ORDER BY id_value"` shows the expected rows

Deliverable on completion: written summary including (a) files modified with one-sentence purpose each, (b) any deviations from spec with reasons, (c) acceptance criteria results (pass/fail per check), (d) one-line follow-up note to the main agent.
```

### 1.2 WP-B subagent prompt

```text
** READ docs/specs/2026-05-08-demo-maturity-design.md FIRST. **
Read §3, §4.5, §4.6, §5.1, §5.2 in particular before writing any code.

⛔ SUBAGENT RESTRICTIONS - YOU ARE A SUBAGENT:
- NEVER run git commit - the main agent will commit your work
- NEVER run loom stage complete - this is not a loom plan
- NEVER run git add -A or git add . - the main agent handles staging
- DO write code, run tests locally, and report results back

YOUR WORK PACKAGE: WP-B — Window enrichment + dispatcher template fill + trigger SSE additive fields
Authoritative spec sections: §3.3, §4.5, §4.6, §5.1
Files you OWN (write):
  - internal/window/window.go
  - internal/window/snapshot.go
  - internal/window/window_test.go
  - internal/dispatch/template.go (NEW)
  - internal/dispatch/template_test.go (NEW)
  - internal/dispatch/payload_adapter.go (modify: call template-fill before formatting Slack/email)
  - any file in internal/dispatch/ or internal/runtime/ where the trigger SSE payload is constructed (find via `grep -rn "StreamTriggers" internal/`); add enriched_traits and assigned_realtor to the SSE message
Files READ-ONLY:
  - internal/rules/, internal/activation/, internal/llm/, internal/event/
  - seed/ (look at canned-responses-hand.yaml AFTER WP-A delivers — that defines what placeholders you must support; if WP-A is not yet committed, refer to spec §5.2 for the placeholder list)

DO NOT TOUCH:
  - any file outside internal/window/ or internal/dispatch/
  - frontend/

Detailed task:
1. Extend UserWindow per spec §4.5: add LastListingProps map[string]any, LastFilterProps map[string]any, DominantSuburb string, and unexported suburbCounts map[string]int. Update apply() to populate these incrementally:
   - On any track event, if properties.suburb is set, increment suburbCounts[suburb] and update DominantSuburb if count exceeds previous max.
   - On Listing Viewed events, replace LastListingProps with a deep-copy of the full properties map.
   - On Filter Applied events, replace LastFilterProps similarly.
   - newUserWindow must initialize these maps to non-nil empty values.

2. Update Snapshot in internal/window/snapshot.go to include the three new fields. Update copyStringAnyMap (or add a helper) for the Last* maps. Snapshot's deep-copy guarantee must hold: callers may mutate the snapshot freely.

3. Add tests in internal/window/window_test.go for the three new fields:
   - LastListingProps captures the latest Listing Viewed event's properties
   - LastFilterProps captures the latest Filter Applied event's properties
   - DominantSuburb is the most-frequent suburb (with a tie-break test)
   - Snapshot includes all three

4. Create internal/dispatch/template.go per spec §5.1:
   - RenderContext struct with four map fields (Trait, Window, Realtor, Outcome)
   - Render(parsed map[string]any, ctx RenderContext) (rendered map[string]any, missingPaths []string)
   - Recursive walk of map values; substitute {{section.path}} via regexp.MustCompile(`\{\{([a-z]+(?:\.[a-z_]+)*)(?::([a-z]+))?\}\}`)
   - Format hints: :pct (multiply by 100, append "%", 0 decimals), :money (humanize integer with $ and commas)
   - Missing path: render "n/a", append to missingPaths, log at slog.Warn level (one log per missing path per call, not per occurrence)
   - Slice walking: walk every element if string-typed; if map-typed, recurse

5. Add comprehensive tests in internal/dispatch/template_test.go:
   - Simple substitution
   - Nested-map substitution (e.g., trait.first_name and window.last_listing.id)
   - Missing path → "n/a"
   - Format hints (:pct, :money)
   - Recursive walk through slices of maps (talking_points list)
   - Multi-occurrence in one string ("{{trait.first_name}} {{trait.last_name}}")

6. Modify internal/dispatch/payload_adapter.go: where the canned response's Parsed map is consumed, call template.Render with the appropriate RenderContext BEFORE constructing the Slack blocks / email body. The RenderContext is built by the dispatcher caller (find where the canned response and the activation traits and the snapshot meet — that's the place to thread RenderContext through). Outcome map is populated per-template:
   - realestate_realtor_pitch: {estimated_deal_value: "$" + last_listing.price (commas), urgency_minutes: "30"}
   - realestate_realtor_anonymous: {recommended_action: "Trigger an in-app banner offering instant tour booking", urgency_minutes: "60"}
   - rs_destination_error / rs_onboarding_stuck: {fix_eta_minutes: "5"}

7. Find where the trigger SSE payload is constructed (grep `StreamTriggers` in internal/). Add two top-level fields to the SSE message data:
   - enriched_traits: the activation.ProfileResponse.Data map
   - assigned_realtor: the selected realtor as map[string]any{name, phone, hours, suburbs}
   Also add these to handleReplayLastTrigger so replay works.

8. Verify:
   - go build -o /tmp/realtime-trigger ./cmd/realtime-trigger
   - go test ./internal/window/... ./internal/dispatch/... -v
   - All existing tests still pass (go test ./internal/...)

Deliverable on completion: written summary as above.
```

### 1.3 WP-C subagent prompt

```text
** READ docs/specs/2026-05-08-demo-maturity-design.md FIRST. **
Read §3, §5.3, §5.5, §5.6 in particular before writing any code.

⛔ SUBAGENT RESTRICTIONS - YOU ARE A SUBAGENT:
- NEVER run git commit - the main agent will commit your work
- NEVER run loom stage complete - this is not a loom plan
- NEVER run git add -A or git add . - the main agent handles staging
- DO write code, run tests locally, and report results back

YOUR WORK PACKAGE: WP-C — Concurrent firer with profile rotation + speed multiplier + API params
Authoritative spec sections: §5.3, §5.5, §5.6
Files you OWN (write):
  - internal/demofire/personas.go
  - internal/demofire/events.go
  - internal/demofire/firer.go
  - internal/demofire/firer_test.go
  - internal/demofire/personas_test.go (create if missing)
  - internal/api/handlers_demo.go
  - cmd/realtime-trigger/serve.go (only the firer-callback wiring at the FireScript hookup)
  - internal/api/router.go (only the FireScript signature in api.Config)
Files READ-ONLY:
  - internal/event/, internal/sse/, internal/db/

DO NOT TOUCH:
  - seed/, internal/rules/, internal/window/, internal/dispatch/, frontend/

Detailed task:
1. Add profileSpec struct to internal/demofire/personas.go per spec §5.3. Define realestateProfileSpecs (8 entries) and rsSelfProfileSpecs (3 entries) as package-level []profileSpec — using the EXACT identify-traits subset from spec §5.3 + Variation table. For anonymous profiles (#3, #8), IdentifyTraits should ONLY contain {membership_tier: "browse"} (no first_name/last_name/email).

2. Refactor RealestateScript so it accepts an optional profile + variation. Add a new function:
   func RealestateScriptForProfile(p profileSpec, v realestateVariation) []ScriptStep
   Use the variation's Suburb, Listings, Filter beds_min, FinalDwellListing fields to construct the 8-step script. The identify event uses p.IdentifyTraits.

3. Add realestateVariations table per spec §5.3 (8 entries matching the 8 profiles).

4. Same for RSSelfScriptForProfile.

5. Rename ScriptForPersona → ScriptForPersonaIndex(persona, idx int) returning a []ScriptStep for the idx-th profile in rotation. Keep the old ScriptForPersona name as a thin wrapper returning idx=0 for backwards compatibility with any tests.

6. Modify internal/demofire/firer.go to add Speed field to Firer (default 1.0):
   type Firer struct {
       // ... existing fields
       Speed float64 // multiplier on DelayMs; 1.0 = unchanged, 0.5 = double delays, 2.0 = halve delays
   }
   Inside Fire(), before sleepWithCtx, scale: actualDelay := time.Duration(float64(step.DelayMs) * (1.0 / speed)) * time.Millisecond. If Speed is 0 or negative, treat as 1.0.

7. Add RunConcurrent(ctx, scripts []NamedScript, speed float64) (totalSent int, err error) to firer.go:
   - NamedScript = {Persona, Script, AnonID}
   - Spawn goroutine per script; staggered start with 500ms × index offset
   - Capture first error via mutex-guarded variable; wait for all goroutines to drain (do not abandon them)
   - Respect ctx cancellation in each goroutine via existing sleepWithCtx
   - Each goroutine constructs its own Firer (or share the parent's HTTPClient) — sharing is fine since http.Client is goroutine-safe

8. Modify api.Config.FireScript signature: from func(ctx, persona) (count int, err error) to func(ctx, persona string, count int, speed float64) (eventsSent int, err error). Update Server struct.

9. Modify internal/api/handlers_demo.go::handleFireScript to accept count + speed. Both via JSON body OR query params. Validate: count ∈ {1,2,3}; speed ∈ {0.5, 1.0, 2.0} (use a small whitelist; reject other values with 400 + JSON error). Default count=1, speed=1.0 if absent.

10. Modify cmd/realtime-trigger/serve.go where the FireScript callback is wired: build a closure that reads count + speed, looks up the persona's profileSpecs and variations, builds N NamedScripts (clipped to len(specs)), and calls firer.RunConcurrent.

11. Add tests:
    - internal/demofire/firer_test.go: TestRunConcurrent_ThreeScripts (mock HTTP with httptest.Server; verify 3 sets of events POSTed with 3 distinct anonymousIds; verify staggered start by checking timestamps), TestSpeed_HalvesDelays
    - internal/demofire/personas_test.go: TestProfileSpecs_HaveDistinctAnonIDs, TestRealestateScriptForProfile_PreservesEightSteps

12. Verify:
    - go build -o /tmp/realtime-trigger ./cmd/realtime-trigger
    - go test ./internal/demofire/... ./internal/api/... -v
    - All existing tests still pass: go test ./internal/...

Deliverable on completion: written summary as above.
```

### 1.4 Round 1 integration (main agent)

After all three subagents return:

1. Re-read each subagent's actual file changes (`git diff --stat`) — verify only owned files were touched. If a subagent modified out-of-scope files, revert those specific paths and re-spawn that subagent with explicit reminder.

2. Run all Round 1 acceptance commands from spec §6.1:
   ```bash
   cd /Users/kumar/workspace/realtime-ai-trigger-svc
   go build -o /tmp/realtime-trigger ./cmd/realtime-trigger
   go test ./internal/...
   /tmp/realtime-trigger seed --from hand --seed-dir ./seed
   psql -d postdb -c "SELECT id_value FROM mock_profiles ORDER BY id_value"
   psql -d postdb -c "SELECT template_name FROM canned_responses ORDER BY template_name"
   ```

3. Restart backend: `pkill -f /tmp/realtime-trigger; bash scripts/start-backend-local.sh` (background).

4. End-to-end fire test:
   ```bash
   curl -s -X POST http://localhost:8080/api/demo/reset
   curl -s -X POST http://localhost:8080/api/demo/fire-script \
     -H 'content-type: application/json' \
     -d '{"persona":"realestate","count":3,"speed":1.0}'
   sleep 35   # wait for idle window + dispatch
   psql -d postdb -c "SELECT rule_name, anonymous_id, dispatch_status FROM triggers ORDER BY fired_at DESC LIMIT 5"
   psql -d postdb -c "SELECT llm_parsed::text FROM triggers ORDER BY fired_at DESC LIMIT 1"
   ```
   Expected: 3 trigger rows (2× realtor_known_high_intent + 1× realtor_anonymous_high_intent), llm_parsed has fully-rendered text with no `{{...}}` placeholders.

5. Stage commits — per WP, with semantic messages:
   ```bash
   git add seed/ internal/rules/loader.go
   git commit -m "$(cat <<'EOF'
feat(seed): mature mock activation with rich profiles + templated canned responses

- Expand mock_profiles to 8 realestate + 3 rs-self with full demographics
- Templated canned responses with {{trait.x}}, {{window.x}}, {{realtor.x}} placeholders
- Split realestate rule into known-high-intent and anonymous-high-intent variants
- Add phone numbers to realtor roster, lower idle_seconds 10→8

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"

   git add internal/window/ internal/dispatch/
   git commit -m "$(cat <<'EOF'
feat(dispatch): templated canned responses with placeholder fill at trigger time

- New internal/dispatch/template.go with regex-based {{section.path}} substitution
- Window struct gains LastListingProps, LastFilterProps, DominantSuburb
- Trigger SSE payload now includes enriched_traits + assigned_realtor

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"

   git add internal/demofire/ internal/api/handlers_demo.go internal/api/router.go cmd/realtime-trigger/serve.go
   git commit -m "$(cat <<'EOF'
feat(demofire): concurrent multi-session firer with profile rotation + speed multiplier

- Fire 1/2/3 sessions per click, each with distinct anonymousId from rotation
- Speed multiplier 0.5x/1x/2x scales inter-event delays
- /api/demo/fire-script accepts count + speed JSON params

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
   ```

6. If any acceptance fails, follow §6.4 of spec (failure handling). Do NOT proceed to Round 2 until Round 1 acceptances pass — incomplete backend will cascade into frontend confusion.

---

## 2. Round 2 — three parallel subagents (Frontend)

Spawn all three in **one message** with three Task tool calls.

### 2.1 WP-D subagent prompt

```text
** READ docs/specs/2026-05-08-demo-maturity-design.md FIRST. **
Read §5.7 in particular before writing any code.

⛔ SUBAGENT RESTRICTIONS - YOU ARE A SUBAGENT (same as before — no commits, no git add).

YOUR WORK PACKAGE: WP-D — Use-case gallery wizard step
Authoritative spec section: §5.7
Files you OWN (write):
  - frontend/components/wizard/UseCaseGallery.tsx (NEW)
  - frontend/lib/use-cases.ts (NEW — exports the useCases array per spec §5.7)
  - frontend/app/onboarding/page.tsx (modify: insert UseCaseGallery as the new first step BEFORE the existing PersonaPicker step)
Files READ-ONLY:
  - rest of frontend/

DO NOT TOUCH:
  - frontend/components/dashboard/, frontend/components/demo/ (Round 2's other WPs)

Detailed task:
1. Create frontend/lib/use-cases.ts exporting the useCases array exactly as specified in §5.7 (4 entries with all fields populated).

2. Create frontend/components/wizard/UseCaseGallery.tsx:
   - 2×2 grid of cards (CSS grid, 2 cols)
   - Each card uses the existing shadcn/ui Card primitive (frontend/components/ui/card.tsx — confirm its named exports)
   - Realestate cards: border-l-4 border-blue-500. Rs-self cards: border-l-4 border-emerald-500.
   - Card layout: Icon + Title (font-semibold) + Subtitle (text-sm text-muted-foreground) + 2-line gap + preview_action (text-xs uppercase tracking-wide opacity-60) + outcome_metric (text-xs font-medium)
   - Hover: hover:shadow-md transition-shadow cursor-pointer
   - Icon: simple emoji or lucide-react icon based on the icon field. Map: rescue→🚨, alert→📞, wrench→🔧, inbox→✉️ (use these emojis for v1; lucide can come later)
   - On click, call props.onSelect(useCase: UseCase) — parent advances the wizard

3. Modify frontend/app/onboarding/page.tsx:
   - Add a new wizard step "use_case" BEFORE "persona"
   - When the use-case is selected, populate the wizard state with persona + rule_template, then auto-advance to the QA step (skip the PersonaPicker step entirely if a use case was picked)
   - Keep PersonaPicker as a fallback escape hatch: at the bottom of UseCaseGallery, render a small text link "Or pick by persona →" that advances to the persona step instead

4. Verify:
   - cd frontend && pnpm install (if node_modules is missing)
   - pnpm tsc --noEmit (or pnpm typecheck if defined in package.json)
   - pnpm build
   - pnpm dev (verify locally; just confirm the page renders without runtime errors at http://localhost:3000/onboarding)

Deliverable on completion: written summary as above.
```

### 2.2 WP-E subagent prompt

```text
** READ docs/specs/2026-05-08-demo-maturity-design.md FIRST. **
Read §4.6, §5.8, §5.9 in particular before writing any code.

⛔ SUBAGENT RESTRICTIONS - YOU ARE A SUBAGENT (no commits, no git add).

YOUR WORK PACKAGE: WP-E — OutcomeBanner + ROITile
Authoritative spec sections: §4.6, §5.8, §5.9
Files you OWN (write):
  - frontend/components/dashboard/OutcomeBanner.tsx (NEW)
  - frontend/components/dashboard/ROITile.tsx (NEW)
  - frontend/app/dashboard/page.tsx (modify: mount both NEW components above the existing 3-column grid)
Files READ-ONLY:
  - rest of frontend/, especially frontend/lib/sse.ts and frontend/lib/api-client.ts

DO NOT TOUCH:
  - frontend/components/wizard/, frontend/components/demo/ (other Round 2 WPs)

Detailed task:
1. Create frontend/components/dashboard/OutcomeBanner.tsx per spec §5.8:
   - Subscribes to the triggers SSE stream (use the same EventSource pattern already used by frontend/components/dashboard/TriggerStream.tsx — read it for reference)
   - Maintains a queue of unseen triggers; cycles through at 4s intervals
   - Each banner displays variant-specific text (3 variants: realestate-known, realestate-anonymous, rs-self)
   - Uses Framer Motion (already imported elsewhere — check frontend/package.json) for slide-from-top + fade-in over 250ms
   - Auto-fades after 12s of no new triggers
   - Empty state: do not render when queue is empty (return null)

2. Create frontend/components/dashboard/ROITile.tsx per spec §5.9:
   - Three numbers: Triggers fired (count), Est. revenue protected ($X.XM with one-decimal precision), Avg. time-to-action (Xs)
   - Computed from the same triggers SSE history that OutcomeBanner consumes — use a shared zustand or context store if one exists; if not, lift state to the dashboard page
   - Empty state: "Fire a script to see live impact"
   - Layout: horizontal flex with three stat cards, gap-4, each card uses the shadcn/ui Card primitive

3. Modify frontend/app/dashboard/page.tsx:
   - Mount ROITile at the top, BELOW the page heading and ABOVE the column grid
   - Mount OutcomeBanner BELOW ROITile, ABOVE the column grid (so it doesn't compete with the columns visually)
   - Keep existing components (EventFeed, WindowInspector, TriggerStream, EmailOutbox, Controller) unchanged in position

4. Verify:
   - pnpm tsc --noEmit
   - pnpm build
   - pnpm dev — visit http://localhost:3000/dashboard, confirm tile + banner placeholders render

Deliverable on completion: written summary as above.
```

### 2.3 WP-F subagent prompt

```text
** READ docs/specs/2026-05-08-demo-maturity-design.md FIRST. **
Read §5.10 in particular before writing any code.

⛔ SUBAGENT RESTRICTIONS - YOU ARE A SUBAGENT (no commits, no git add).

YOUR WORK PACKAGE: WP-F — Demo controller count + speed pickers
Authoritative spec section: §5.10
Files you OWN (write):
  - frontend/components/demo/Controller.tsx
Files READ-ONLY:
  - rest of frontend/ (especially api-client.ts to find demoFireScript / demoReset)

DO NOT TOUCH:
  - frontend/components/wizard/, frontend/components/dashboard/, frontend/app/

Detailed task:
1. Add two segmented controls to Controller.tsx (use shadcn/ui ToggleGroup or render plain buttons with active state — pick whichever already exists):
   - Sessions: 1 / 2 / 3 (default 2)
   - Speed: 0.5x / 1x / 2x (default 1x)

2. Update the Fire button label dynamically: `Fire {count} {persona} session{count > 1 ? 's' : ''} @ {speed}x`. Both Fire buttons (realestate and rs-self) get this treatment.

3. Update handleFireScript (or whatever the click handler is named) to send count + speed in the POST body to /api/demo/fire-script.

4. Preserve all existing behavior: auto-reset on Fire, friendly replay error, controller layout.

5. Verify:
   - pnpm tsc --noEmit
   - pnpm build

Deliverable on completion: written summary as above.
```

### 2.4 Round 2 integration (main agent)

After all three subagents return:

1. Run `git diff --stat` to verify file ownership.
2. Run frontend acceptance:
   ```bash
   cd frontend
   pnpm tsc --noEmit
   pnpm build
   ```
3. Spin up frontend in dev mode (`pnpm dev` background). Verify in a browser via Playwright MCP if available, or via curl:
   ```bash
   curl -s http://localhost:3000/onboarding | head -50
   curl -s http://localhost:3000/dashboard | head -50
   ```
   (Both should return HTML, not error pages.)

4. End-to-end smoke test using Playwright MCP if available:
   - Navigate to /onboarding, click the "Win back high-intent anonymous visitors" card
   - Verify QA step shows persona=realestate
   - Activate the rule
   - Navigate to /dashboard
   - Click Fire button with count=3
   - Wait 35 seconds
   - Verify ROI tile shows "Triggers fired: 3" and OutcomeBanner has cycled through messages

5. Commit per WP:
   ```bash
   git add frontend/lib/use-cases.ts frontend/components/wizard/UseCaseGallery.tsx frontend/app/onboarding/page.tsx
   git commit -m "feat(frontend): use-case gallery wizard step with 4 outcome cards"

   git add frontend/components/dashboard/OutcomeBanner.tsx frontend/components/dashboard/ROITile.tsx frontend/app/dashboard/page.tsx
   git commit -m "feat(frontend): outcome banner + ROI tile on dashboard"

   git add frontend/components/demo/Controller.tsx
   git commit -m "feat(frontend): demo controller count + speed selectors"
   ```

---

## 3. Round 3 — final integration (main agent, sequential)

1. Restart backend + frontend together. Verify both reachable.
2. Final end-to-end smoke per spec §6.3.
3. Update `HANDOFF.md` with a polish-round-6 summary that lists what shipped and what (if anything) was skipped due to failure cascades.
4. `git push origin main` to push all commits to GitHub.
5. Mark a chapter for the user's morning review: `mcp__ccd_session__mark_chapter` with title "Demo maturity ready for review".
6. End the session with a summary message ready for the user when they wake up: list of commits, any tradeoffs, and what to verify before the demo.

---

## 4. Failure ladder (autonomous decisions)

Per spec §6.4. Decisions made in priority order:

1. **Acceptance fails** → Diagnose with `go vet`, single-test run, `tail` logs. One retry of the offending subagent with a more detailed prompt that quotes the exact failure.
2. **Second retry fails** → Apply the spec-§6.4 fallback for that WP (e.g., simplified template, single-session firing, no banner cycling). Continue.
3. **Round 1 fundamentally broken (multiple WPs failing)** → Stop. Revert all Round 1 commits via `git reset --hard <pre-flight-sha>`. Write status note in HANDOFF.md. End session.
4. **6-hour cutoff approaching** → Ship what's ready, write status note, push. Do not start work that can't finish in remaining time.

---

## 5. Output for user (morning review)

When the user wakes up, the session must end with:

1. A summary message listing:
   - Final commits (with SHAs)
   - What works end-to-end (verified)
   - What was skipped or simplified (with reason)
   - One paragraph of "what to test before the demo"
2. The branch pushed to GitHub.
3. The backend running locally (or cleanly stoppable).
4. HANDOFF.md updated.

---

**END OF PLAN. EXECUTION STARTS ON USER APPROVAL.**
