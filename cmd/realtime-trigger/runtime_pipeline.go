package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/consumer"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/db"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/llm"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/rules"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/sse"
	"github.com/rudderlabs/realtime-ai-trigger-svc/internal/window"
)

// runPipeline drains consumerOut, applies the filter, updates windows,
// fans the event to the archive channel, and evaluates rules per event.
//
// One event per goroutine pass — no batching here. The downstream archive
// writer batches.
func (rt *runtime) runPipeline(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case pe, ok := <-rt.consumerOut:
			if !ok {
				return
			}
			processed, keep := rt.flt.Process(pe)
			if !keep {
				continue
			}
			// Update window aggregations.
			rt.windows.Update(processed.Event)

			// Stream the event to the SSE events channel — best-effort.
			rt.hub.Publish(sse.StreamEvents, sse.Message{
				Event: sse.StreamEvents,
				Data:  rt.eventSummary(processed),
			})
			// Stream a window snapshot too so the dashboard can repaint.
			if snap, ok := rt.windows.Snapshot(processed.Event.EffectiveAnonymousID()); ok {
				rt.hub.Publish(sse.StreamWindows, sse.Message{
					Event: sse.StreamWindows,
					Data:  snap,
				})
			}

			// Hand off to archive writer (drops on full to avoid stalling).
			select {
			case rt.archiveCh <- processed:
			default:
				rt.log.Warn("serve: archive channel full — dropping event",
					"anon", processed.Event.EffectiveAnonymousID())
			}

			// Hot-path rule evaluation.
			rt.evaluateAndDispatch(ctx, processed.Event.EffectiveAnonymousID(), false)
		}
	}
}

// runIdleTicker scans windows on a fixed cadence and re-evaluates any
// time-dependent rules. The ticker fires every second so the §6.2 idle
// trigger (idle_seconds >= 10) lands within ~1s of becoming eligible.
func (rt *runtime) runIdleTicker(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rt.windows.ScanIdle(time.Second, func(snap window.Snapshot) {
				rt.evaluateAndDispatchSnap(ctx, snap, true)
			})
		}
	}
}

// fanoutPrunes consumes the window pruner channel and emits SSE
// `window_pruned` notifications. Run as its own goroutine to avoid
// pinning the pruner.
func (rt *runtime) fanoutPrunes(ctx context.Context) {
	prunes := rt.windows.PrunedChan()
	for {
		select {
		case <-ctx.Done():
			return
		case anonID, ok := <-prunes:
			if !ok {
				return
			}
			rt.hub.Publish(sse.StreamWindows, sse.Message{
				Event: sse.EventWindowPruned,
				Data:  map[string]string{"anonymous_id": anonID},
			})
		}
	}
}

// runArchive batches up to 50 events every 200ms and writes them to PG.
// Slow Postgres won't backpressure the pipeline because the archive
// channel drops on full (see runPipeline).
func (rt *runtime) runArchive(ctx context.Context) {
	const (
		batchSize   = 50
		flushPeriod = 200 * time.Millisecond
	)

	batch := make([]consumer.ProcessedEvent, 0, batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		for _, pe := range batch {
			payload, _ := json.Marshal(pe.Event)
			anon := pe.Event.EffectiveAnonymousID()
			if anon == "" {
				continue
			}
			if _, err := db.InsertEvent(ctx, rt.pool,
				anon, pe.Event.UserID, pe.WriteKey,
				pe.Event.Type, pe.Event.Event, pe.Event.PagePath(),
				payload,
			); err != nil {
				rt.log.Warn("serve: archive insert failed", "err", err, "anon", anon)
			}
		}
		batch = batch[:0]
	}

	t := time.NewTicker(flushPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case pe, ok := <-rt.archiveCh:
			if !ok {
				flush()
				return
			}
			batch = append(batch, pe)
			if len(batch) >= batchSize {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

// runDispatcher starts a fixed worker pool (workerCount=4) that reads from
// matchCh and calls fireMatch for each item. Each fire gets its own bounded
// context so a stuck Slack call cannot block the worker indefinitely.
//
// On shutdown (gCtx cancelled) the pool drains in-flight items up to a 3s
// grace period before exiting.
func (rt *runtime) runDispatcher(gCtx context.Context) {
	const (
		workerCount   = 4
		fireTimeout   = 30 * time.Second
		drainTimeout  = 3 * time.Second
	)

	worker := func() {
		for {
			select {
			case item, ok := <-rt.matchCh:
				if !ok {
					return
				}
				fireCtx, cancel := context.WithTimeout(context.Background(), fireTimeout)
				rt.fireMatch(fireCtx, item.m, item.persona)
				cancel()
			case <-gCtx.Done():
				// Drain remaining matches up to drainTimeout.
				drainCtx, drainCancel := context.WithTimeout(context.Background(), drainTimeout)
				defer drainCancel()
				for {
					select {
					case item, ok := <-rt.matchCh:
						if !ok {
							return
						}
						fireCtx, cancel := context.WithTimeout(context.Background(), fireTimeout)
						rt.fireMatch(fireCtx, item.m, item.persona)
						cancel()
					case <-drainCtx.Done():
						return
					}
				}
			}
		}
	}

	for i := 0; i < workerCount; i++ {
		go worker()
	}
	// Block until gCtx is cancelled so the errgroup goroutine returns cleanly.
	<-gCtx.Done()
}

// evaluateAndDispatch is the hot-path entry — looks up the snapshot for
// anonID and runs the engine. timeOnly=true reuses the same code path
// for the idle ticker.
func (rt *runtime) evaluateAndDispatch(ctx context.Context, anonID string, timeOnly bool) {
	if anonID == "" {
		return
	}
	snap, ok := rt.windows.Snapshot(anonID)
	if !ok {
		return
	}
	rt.evaluateAndDispatchSnap(ctx, snap, timeOnly)
}

// evaluateAndDispatchSnap runs the engine against a Snapshot. We split
// per-persona so rules scoped to one persona don't leak across users.
//
// We probe BOTH personas because the consumer doesn't carry persona —
// the rule's persona scope filters out non-matches. This is cheap with
// in-memory aggregations.
//
// Cooldown gating (Allow + Mark) is performed synchronously by the engine
// before the Match is queued. Only the downstream fireMatch work (LLM + Slack
// + PG) is async. This preserves cooldown-gate ordering guarantees.
func (rt *runtime) evaluateAndDispatchSnap(_ context.Context, snap window.Snapshot, timeOnly bool) {
	now := time.Now().UTC()
	for _, persona := range []string{llm.PersonaRealestate, llm.PersonaRSSelf} {
		var matches []rules.Match
		if timeOnly {
			matches = rt.engine.EvaluateOnTick(snap, persona, now)
		} else {
			matches = rt.engine.EvaluateOnEvent(snap, persona, now)
		}
		for _, m := range matches {
			item := dispatchItem{m: m, persona: persona}
			select {
			case rt.matchCh <- item:
			default:
				dropped := rt.matchDropped.Add(1)
				rt.log.Warn("serve: match_dropped — dispatch worker pool saturated",
					"rule", m.RuleName,
					"anon", m.Anonymous,
					"total_dropped", dropped)
			}
		}
	}
}

// sseEventDTO matches the SSEEventPayload TypeScript type in
// frontend/types/api.ts (§3.10 / §6.4). JSON tags are camelCase to match the
// v3 RudderStack JS-SDK wire shape the frontend expects.
// Extra backend fields (pagePath, writeKey, pulsarMessageId) are included so
// the dashboard has routing context without a separate fetch; the frontend
// ignores unknown keys.
type sseEventDTO struct {
	Type              string         `json:"type"`
	Channel           string         `json:"channel"`
	Event             string         `json:"event,omitempty"`
	AnonymousID       string         `json:"anonymousId"`
	UserID            string         `json:"userId,omitempty"`
	MessageID         string         `json:"messageId"`
	OriginalTimestamp string         `json:"originalTimestamp"`
	Properties        map[string]any `json:"properties,omitempty"`
	Context           map[string]any `json:"context,omitempty"`
	Traits            map[string]any `json:"traits,omitempty"`
	// Extra backend fields (frontend ignores):
	PagePath        string `json:"pagePath,omitempty"`
	WriteKey        string `json:"writeKey,omitempty"`
	PulsarMessageID string `json:"pulsarMessageId,omitempty"`
}

// eventSummary returns an SSE payload for the events stream that matches
// SSEEventPayload in frontend/types/api.ts. The camelCase field names are
// mandatory — the frontend EventCard reads event.payload.anonymousId directly.
func (rt *runtime) eventSummary(pe consumer.ProcessedEvent) sseEventDTO {
	dto := sseEventDTO{
		Type:              pe.Event.Type,
		Channel:           pe.Event.Channel,
		Event:             pe.Event.Event,
		AnonymousID:       pe.Event.EffectiveAnonymousID(),
		UserID:            pe.Event.UserID,
		MessageID:         pe.Event.MessageID,
		OriginalTimestamp: pe.Event.OriginalTimestamp.UTC().Format("2006-01-02T15:04:05.000Z"),
		PagePath:          pe.Event.PagePath(),
		WriteKey:          pe.WriteKey,
		PulsarMessageID:   pe.PulsarMessageID,
	}
	if props := pe.Event.PropertiesMap(); len(props) > 0 {
		dto.Properties = props
	}
	if traits := pe.Event.TraitsMap(); len(traits) > 0 {
		dto.Traits = traits
	}
	return dto
}
