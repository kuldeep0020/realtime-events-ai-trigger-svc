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
 * Used by WP-H for the dashboard live columns. Exported from here so WP-H
 * only needs to import one hook.
 */

import { useEffect, useRef, useCallback } from "react";
import type { StreamName } from "@/types/api";
import eventsRealestate from "@/mocks/events-realestate.json";
import eventsRsSelf from "@/mocks/events-rs-self.json";
import triggerRealestate from "@/mocks/trigger-realestate.json";
import triggerRsSelf from "@/mocks/trigger-rs-self.json";

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

export type SSEMessageHandler = (message: SSEMessage) => void;

// ──────────────────────────────────────────────────────────────────────────────
// Mock replay sequences keyed by stream name
// ──────────────────────────────────────────────────────────────────────────────

const MOCK_SEQUENCES: Partial<Record<StreamName, unknown[]>> = {
  events: [...eventsRealestate, ...eventsRsSelf],
  triggers: [triggerRealestate, triggerRsSelf],
  windows: [],
  mock_emails: [],
};

// ──────────────────────────────────────────────────────────────────────────────
// Hook
// ──────────────────────────────────────────────────────────────────────────────

/**
 * useSSEStream — subscribes to a named stream and calls onMessage for each event.
 *
 * The connection is torn down on component unmount or when stream/onMessage
 * reference changes. onMessage is wrapped in a stable ref to prevent infinite
 * re-subscription loops when the caller passes an inline function.
 */
export function useSSEStream(
  stream: StreamName,
  onMessage: SSEMessageHandler,
  enabled = true
): void {
  const handlerRef = useRef<SSEMessageHandler>(onMessage);
  handlerRef.current = onMessage;

  const stableHandler = useCallback((msg: SSEMessage) => {
    handlerRef.current(msg);
  }, []);

  useEffect(() => {
    if (!enabled) return;

    if (isMockMode) {
      // Replay mock events at ~2s intervals
      const sequence = MOCK_SEQUENCES[stream] ?? [];
      if (sequence.length === 0) return;
      let idx = 0;
      const timerId = setInterval(() => {
        if (idx >= sequence.length) {
          clearInterval(timerId);
          return;
        }
        stableHandler({
          event: stream,
          data: sequence[idx],
        });
        idx++;
      }, 2000);
      return () => clearInterval(timerId);
    }

    // Live mode: native EventSource
    const url = `${BASE}/api/streams/${stream}`;
    const source = new EventSource(url);

    source.onmessage = (ev: MessageEvent<string>) => {
      let parsed: unknown;
      try {
        parsed = JSON.parse(ev.data) as unknown;
      } catch {
        parsed = ev.data;
      }
      stableHandler({ id: ev.lastEventId, event: ev.type, data: parsed });
    };

    // Listen for named events the server emits (e.g., event: "trigger")
    const eventNames: string[] = [
      "events",
      "windows",
      "triggers",
      "mock_emails",
      "window_pruned",
    ];
    const namedListeners = eventNames.map((name) => {
      const listener = (ev: MessageEvent<string>) => {
        let parsed: unknown;
        try {
          parsed = JSON.parse(ev.data) as unknown;
        } catch {
          parsed = ev.data;
        }
        stableHandler({ id: ev.lastEventId, event: name, data: parsed });
      };
      source.addEventListener(name, listener);
      return { name, listener };
    });

    source.onerror = () => {
      // EventSource auto-reconnects on error; no explicit action needed.
    };

    return () => {
      namedListeners.forEach(({ name, listener }) =>
        source.removeEventListener(name, listener)
      );
      source.close();
    };
  }, [stream, stableHandler, enabled]);
}
