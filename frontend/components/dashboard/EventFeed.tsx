"use client";

/**
 * Column 1 — Live event feed.
 * Displays a stack of incoming RudderStack events (newest on top).
 * Events fade and slide off after 30 seconds. Animate-in via Framer Motion.
 */

import { useState, useEffect, useCallback, useRef } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { FileText, Navigation, User, Wifi, WifiOff } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useSSEStream } from "@/lib/sse";
import { listRecentEvents } from "@/lib/api-client";
import type { SSEEventPayload } from "@/types/api";

interface LiveEvent {
  id: string;
  payload: SSEEventPayload;
  receivedAt: number;
}

const EVENT_TTL_MS = 30_000;
const MAX_VISIBLE = 10;

function typeIcon(type: string) {
  if (type === "page") return <Navigation className="w-3.5 h-3.5 text-sky-400" aria-hidden="true" />;
  if (type === "identify") return <User className="w-3.5 h-3.5 text-violet-400" aria-hidden="true" />;
  return <FileText className="w-3.5 h-3.5 text-emerald-400" aria-hidden="true" />;
}

function eventLabel(ev: SSEEventPayload): string {
  if (ev.type === "track") return ev.event ?? "(unnamed track)";
  if (ev.type === "page") {
    const path = (ev.properties?.path as string) ?? ev.properties?.url ?? "(page)";
    return path;
  }
  return "identify";
}

function anonSuffix(id: string): string {
  return id.slice(-6);
}

interface EventCardProps {
  event: LiveEvent;
  onSelect: (ev: LiveEvent) => void;
  highlighted: boolean;
}

function EventCard({ event, onSelect, highlighted }: EventCardProps) {
  const age = Date.now() - event.receivedAt;
  const opacity = Math.max(0.3, 1 - age / EVENT_TTL_MS);

  return (
    <motion.div
      layout
      initial={{ opacity: 0, y: -16 }}
      animate={{ opacity, y: 0 }}
      exit={{ opacity: 0, y: 8, scale: 0.97 }}
      transition={{ duration: 0.2 }}
      className={`cursor-pointer rounded-lg border px-3 py-2 text-xs transition-colors ${
        highlighted
          ? "border-violet-500 bg-violet-950/40 shadow-[0_0_12px_rgba(116,71,252,0.25)]"
          : "border-slate-800 bg-slate-900/60 hover:border-slate-700"
      }`}
      onClick={() => onSelect(event)}
      role="button"
      tabIndex={0}
      aria-label={`Event: ${eventLabel(event.payload)}`}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelect(event);
        }
      }}
    >
      <div className="flex items-center gap-1.5 mb-1">
        {typeIcon(event.payload.type)}
        <span className="font-mono truncate text-slate-200 max-w-[140px]">
          {eventLabel(event.payload)}
        </span>
        <Badge
          variant="outline"
          className="ml-auto font-mono text-[10px] text-slate-400 border-slate-700 px-1 py-0"
        >
          …{anonSuffix(event.payload.anonymousId)}
        </Badge>
      </div>
      <div className="text-slate-500 text-[10px]">
        {new Date(event.payload.originalTimestamp).toLocaleTimeString()}
      </div>
      {event.payload.properties && (
        <details className="mt-1">
          <summary className="text-slate-500 cursor-pointer select-none text-[10px] hover:text-slate-400">
            properties
          </summary>
          <pre className="mt-1 text-[10px] font-mono text-slate-400 whitespace-pre-wrap break-all">
            {JSON.stringify(event.payload.properties, null, 2)}
          </pre>
        </details>
      )}
    </motion.div>
  );
}

interface EventFeedProps {
  /** anonymousIds of sessions currently being highlighted (from trigger fires) */
  highlightedIds?: Set<string>;
  /** subscriber count from window stream */
  subscriberCount?: number;
}

export function EventFeed({ highlightedIds = new Set(), subscriberCount = 0 }: EventFeedProps) {
  const [events, setEvents] = useState<LiveEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [selected, setSelected] = useState<LiveEvent | null>(null);

  // Epoch counter to guard against the initial-fetch/reset race:
  // if a `reset` SSE arrives while the GET /api/recent-events is still
  // in-flight, the fetch may resolve after the reset and re-populate state
  // with stale rows. Incrementing the epoch on every reset and capturing it
  // before the fetch lets us discard results that belong to a prior era.
  const resetEpochRef = useRef(0);

  // On mount, seed the event list with recent events from the backend so the
  // dashboard is populated immediately after a browser refresh or direct URL load.
  useEffect(() => {
    const epoch = resetEpochRef.current;
    listRecentEvents(50)
      .then((resp) => {
        if (epoch < resetEpochRef.current) return; // a reset arrived — drop stale result
        setEvents((prev) => {
          const seen = new Set(prev.map((e) => e.id));
          const merged = [...prev];
          for (const ev of resp.events) {
            const id = ev.messageId ?? `evt-init-${Date.now()}`;
            if (!seen.has(id)) {
              seen.add(id);
              merged.push({
                id,
                payload: ev,
                // Use originalTimestamp for relative ordering; cap to now so
                // TTL pruning doesn't immediately discard these seed events.
                receivedAt: Math.min(
                  new Date(ev.originalTimestamp).getTime() || Date.now(),
                  Date.now()
                ),
              });
            }
          }
          merged.sort((a, b) => b.receivedAt - a.receivedAt);
          return merged.slice(0, MAX_VISIBLE);
        });
        setConnected(true);
      })
      .catch((err) => console.warn("[EventFeed] initial fetch failed:", err));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Prune events older than TTL every 5s
  useEffect(() => {
    const id = setInterval(() => {
      const now = Date.now();
      setEvents((prev) => prev.filter((e) => now - e.receivedAt < EVENT_TTL_MS));
    }, 5_000);
    return () => clearInterval(id);
  }, []);

  const onMessage = useCallback(
    (msg: { event?: string; data: unknown }) => {
      if (msg.event === "reset") {
        // Server-side demo reset: advance epoch so any in-flight initial fetch
        // is discarded on resolve, then clear local event list.
        resetEpochRef.current += 1;
        setEvents([]);
        return;
      }
      if (msg.event === "events" || msg.event === undefined) {
        setConnected(true);
        const payload = msg.data as SSEEventPayload;
        setEvents((prev) => {
          const next = [
            {
              id: payload.messageId ?? `evt-${Date.now()}`,
              payload,
              receivedAt: Date.now(),
            },
            ...prev,
          ].slice(0, MAX_VISIBLE);
          return next;
        });
      }
    },
    []
  );

  useSSEStream("events", onMessage, true, () => setConnected(true));

  // Safety net: also mark connected once events start arriving (e.g. in mock mode).
  useEffect(() => {
    if (events.length > 0) setConnected(true);
  }, [events.length]);

  return (
    <section className="flex flex-col h-full" aria-label="Live Events">
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-slate-800 shrink-0">
        <span className="text-sm font-medium text-slate-200">Live Events</span>
        {subscriberCount > 0 && (
          <Badge
            variant="outline"
            className="text-[10px] border-slate-700 text-slate-400 px-1.5 py-0"
          >
            {subscriberCount} session{subscriberCount !== 1 ? "s" : ""}
          </Badge>
        )}
        <div className="ml-auto flex items-center gap-1">
          {connected ? (
            <Wifi className="w-3 h-3 text-emerald-400" aria-label="Connected" />
          ) : (
            <WifiOff className="w-3 h-3 text-slate-500" aria-label="Connecting" />
          )}
          <span className="text-[10px] text-slate-500">
            {connected ? "live" : "connecting"}
          </span>
        </div>
      </div>

      {/* Event list */}
      <div
        className="flex-1 overflow-y-auto overflow-x-hidden p-2 space-y-1.5"
        aria-live="polite"
        aria-relevant="additions"
      >
        <AnimatePresence initial={false}>
          {events.length === 0 && (
            <motion.p
              key="empty"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="text-xs text-slate-500 text-center pt-8"
            >
              Waiting for events…
            </motion.p>
          )}
          {events.map((ev) => (
            <EventCard
              key={ev.id}
              event={ev}
              onSelect={setSelected}
              highlighted={highlightedIds.has(ev.payload.anonymousId)}
            />
          ))}
        </AnimatePresence>
      </div>

      {/* Full payload dialog */}
      <Dialog
        open={selected !== null}
        onOpenChange={(open) => !open && setSelected(null)}
      >
        <DialogContent className="bg-slate-900 border-slate-700 max-w-lg">
          <DialogHeader>
            <DialogTitle className="text-slate-100 text-sm">
              {selected && eventLabel(selected.payload)}
            </DialogTitle>
          </DialogHeader>
          <pre className="text-xs font-mono text-slate-300 overflow-auto max-h-80 whitespace-pre-wrap break-all">
            {selected && JSON.stringify(selected.payload, null, 2)}
          </pre>
        </DialogContent>
      </Dialog>
    </section>
  );
}
