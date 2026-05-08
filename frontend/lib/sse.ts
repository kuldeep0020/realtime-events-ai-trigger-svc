"use client";

/**
 * Typed EventSource hook for SSE streams.
 *
 * In live mode (NEXT_PUBLIC_API_BASE set): connects to
 *   GET {BASE}/api/streams/{stream}
 *
 * In mock mode: replays events from the hardcoded sequences at one event
 * per ~2 seconds using a synthetic timer, so the UI looks live without a
 * backend. The mock sequences match /mocks/events-*.json.
 *
 * ──────────────────────────────────────────────────────────────────────────────
 * Connection-budget design
 * ──────────────────────────────────────────────────────────────────────────────
 *
 * Browsers cap HTTP/1.1 connections per origin at 6. Earlier the dashboard
 * opened five separate EventSources (events, windows, triggers×3 from
 * TriggerStream/OutcomeBanner/ROITile, mock_emails on the Emails tab),
 * doubled in dev mode by React StrictMode + HMR. This pinned the connection
 * pool, causing /api/demo/reset and /api/mock-emails fetches to queue
 * indefinitely (4s+ to abort).
 *
 * This module fixes that by maintaining ONE EventSource per stream name
 * across the whole tree, with a refcounted broadcaster. Components subscribe
 * via the same useSSEStream hook; behind the scenes their callbacks get
 * added to a shared listener set. When refcount drops to 0 (last subscriber
 * unmounts), the EventSource closes.
 *
 * Net effect: max 4 simultaneous EventSources (events, windows, triggers,
 * mock_emails — and only the ones currently subscribed), leaving plenty of
 * connection slots for fetches.
 */

import { useEffect, useRef, useCallback } from "react";
import type { StreamName } from "@/types/api";
import eventsRealestate from "@/mocks/events-realestate.json";
import eventsRsSelf from "@/mocks/events-rs-self.json";
import triggerRealestate from "@/mocks/trigger-realestate.json";
import triggerRsSelf from "@/mocks/trigger-rs-self.json";
import windowsRealestate from "@/mocks/windows-realestate.json";
import windowsRsSelf from "@/mocks/windows-rs-self.json";

const BASE = process.env.NEXT_PUBLIC_API_BASE ?? "";
const isMockMode = BASE === "";

// ──────────────────────────────────────────────────────────────────────────────
// Types
// ──────────────────────────────────────────────────────────────────────────────

export interface SSEMessage {
  id?: string;
  event?: string;
  data: unknown;
}

export type SSEMessageHandler = (msg: SSEMessage) => void;

// ──────────────────────────────────────────────────────────────────────────────
// Mock replay sequences keyed by stream name
// ──────────────────────────────────────────────────────────────────────────────

const MOCK_SEQUENCES: Partial<Record<StreamName, unknown[]>> = {
  events: [...eventsRealestate, ...eventsRsSelf],
  triggers: [triggerRealestate, triggerRsSelf],
  windows: [...windowsRealestate, ...windowsRsSelf],
  mock_emails: [],
};

const NAMED_EVENT_NAMES: readonly string[] = [
  "events",
  "windows",
  "triggers",
  "mock_emails",
  "window_pruned",
  "reset",
];

// ──────────────────────────────────────────────────────────────────────────────
// Singleton broadcaster per stream
// ──────────────────────────────────────────────────────────────────────────────

interface BroadcasterEntry {
  source: EventSource | null;
  listeners: Set<SSEMessageHandler>;
  /** Tracks who's interested in onOpen — invoked once on next establish. */
  pendingOpenCallbacks: Set<() => void>;
  /** Already opened? (latched true on first onopen; reset on close.) */
  opened: boolean;
  /** Mock-mode timer (when isMockMode === true). */
  mockTimer: ReturnType<typeof setInterval> | null;
}

const broadcasters = new Map<StreamName, BroadcasterEntry>();

// Expose a debug inspector at window.__sse_debug so we can check broadcaster
// health from the browser console (e.g., "is the EventSource for 'events'
// stuck in CLOSED state?"). No-op in SSR.
if (typeof window !== "undefined") {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (window as any).__sse_debug = () =>
    Array.from(broadcasters.entries()).map(([stream, e]) => ({
      stream,
      readyState: e.source?.readyState ?? "no-source",
      // 0=CONNECTING, 1=OPEN, 2=CLOSED
      readyStateLabel:
        e.source?.readyState === 0
          ? "CONNECTING"
          : e.source?.readyState === 1
            ? "OPEN"
            : e.source?.readyState === 2
              ? "CLOSED"
              : "no-source",
      listeners: e.listeners.size,
      opened: e.opened,
    }));
}

function getOrCreate(stream: StreamName): BroadcasterEntry {
  let entry = broadcasters.get(stream);
  if (!entry) {
    entry = {
      source: null,
      listeners: new Set(),
      pendingOpenCallbacks: new Set(),
      opened: false,
      mockTimer: null,
    };
    broadcasters.set(stream, entry);
  }
  return entry;
}

function fanOut(entry: BroadcasterEntry, msg: SSEMessage): void {
  // Snapshot listeners so handlers can subscribe/unsubscribe during fan-out
  // without mutating the iteration set.
  for (const cb of Array.from(entry.listeners)) {
    try {
      cb(msg);
    } catch (err) {
      // Don't let a single handler error kill the broadcaster.
      // eslint-disable-next-line no-console
      console.error("[sse] listener threw", err);
    }
  }
}

function connect(stream: StreamName, entry: BroadcasterEntry): void {
  // Self-heal: if we still hold a reference to a previous EventSource that
  // the browser silently closed (typically on Next.js client-side navigation
  // or when the network drops below the HTTP/1.1 connection cap during
  // dashboard ↔ onboarding round-trips), drop it and open a fresh one. Without
  // this, the early-return below would short-circuit forever and the
  // broadcaster's listeners would never receive another message — the
  // symptom users see as "click Fire → 'Resetting…' stuck → refresh fixes."
  if (entry.source !== null && entry.source.readyState === 2 /* CLOSED */) {
    entry.source.close();
    entry.source = null;
    entry.opened = false;
  }
  if (entry.source !== null || entry.mockTimer !== null) return;

  if (isMockMode) {
    const sequence = MOCK_SEQUENCES[stream] ?? [];
    if (sequence.length === 0) {
      // Even in mock mode, surface "opened" so consumers like EventFeed
      // can flip from "connecting" to "live".
      entry.opened = true;
      entry.pendingOpenCallbacks.forEach((cb) => cb());
      entry.pendingOpenCallbacks.clear();
      return;
    }
    let idx = 0;
    entry.opened = true;
    entry.pendingOpenCallbacks.forEach((cb) => cb());
    entry.pendingOpenCallbacks.clear();
    entry.mockTimer = setInterval(() => {
      if (idx >= sequence.length) {
        if (entry.mockTimer) clearInterval(entry.mockTimer);
        entry.mockTimer = null;
        return;
      }
      fanOut(entry, { event: stream, data: sequence[idx] });
      idx++;
    }, 2000);
    return;
  }

  // Live mode
  const url = `${BASE}/api/streams/${stream}`;
  const source = new EventSource(url);
  entry.source = source;

  source.onmessage = (ev: MessageEvent<string>) => {
    let parsed: unknown;
    try {
      parsed = JSON.parse(ev.data) as unknown;
    } catch {
      parsed = ev.data;
    }
    fanOut(entry, { id: ev.lastEventId, event: ev.type, data: parsed });
  };

  for (const name of NAMED_EVENT_NAMES) {
    source.addEventListener(name, (ev) => {
      const msgEv = ev as MessageEvent<string>;
      let parsed: unknown;
      try {
        parsed = JSON.parse(msgEv.data) as unknown;
      } catch {
        parsed = msgEv.data;
      }
      fanOut(entry, { id: msgEv.lastEventId, event: name, data: parsed });
    });
  }

  source.onopen = () => {
    entry.opened = true;
    entry.pendingOpenCallbacks.forEach((cb) => cb());
    entry.pendingOpenCallbacks.clear();
  };

  source.onerror = () => {
    // EventSource auto-reconnects on error; no explicit action needed.
  };
}

function disconnect(entry: BroadcasterEntry): void {
  if (entry.source) {
    entry.source.close();
    entry.source = null;
  }
  if (entry.mockTimer) {
    clearInterval(entry.mockTimer);
    entry.mockTimer = null;
  }
  entry.opened = false;
  entry.pendingOpenCallbacks.clear();
}

// ──────────────────────────────────────────────────────────────────────────────
// Hook
// ──────────────────────────────────────────────────────────────────────────────

/**
 * useSSEStream — subscribes to a named stream and calls onMessage for each
 * event. Multiple components subscribing to the same stream share ONE
 * underlying EventSource (refcounted across the tree). The connection is
 * established on first subscriber and torn down when the last subscriber
 * unmounts.
 *
 * onOpen, if provided, is called when the EventSource connection is
 * established (or immediately if already open). Use this to flip UI state
 * from "connecting" to "connected" without waiting for the first data event.
 */
export function useSSEStream(
  stream: StreamName,
  onMessage: SSEMessageHandler,
  enabled = true,
  onOpen?: () => void
): void {
  const handlerRef = useRef<SSEMessageHandler>(onMessage);
  handlerRef.current = onMessage;

  const stableHandler = useCallback((msg: SSEMessage) => {
    handlerRef.current(msg);
  }, []);

  const onOpenRef = useRef<(() => void) | undefined>(onOpen);
  onOpenRef.current = onOpen;

  useEffect(() => {
    if (!enabled) return;

    const entry = getOrCreate(stream);
    entry.listeners.add(stableHandler);

    // Establish the connection if this is the first subscriber.
    connect(stream, entry);

    // If the connection is already open by the time we subscribe (other
    // components opened it earlier), fire onOpen synchronously.
    if (entry.opened) {
      onOpenRef.current?.();
    } else if (onOpenRef.current) {
      // Otherwise queue it for the next onopen.
      const cb = () => onOpenRef.current?.();
      entry.pendingOpenCallbacks.add(cb);
    }

    return () => {
      entry.listeners.delete(stableHandler);
      // If we still have any subscribers, keep the connection alive.
      if (entry.listeners.size === 0) {
        disconnect(entry);
      }
    };
  }, [stream, stableHandler, enabled]);
}
