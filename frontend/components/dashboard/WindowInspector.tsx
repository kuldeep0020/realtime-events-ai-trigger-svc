"use client";

/**
 * Column 2 — Rolling-window inspector.
 * One card per active session keyed by anonymousId.
 * When a trigger fires for a session, the card flashes red→green with a sparkle.
 */

import { useState, useEffect, useCallback } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Badge } from "@/components/ui/badge";
import { useSSEStream } from "@/lib/sse";
import type { SSEWindowPayload } from "@/types/api";

interface WindowEntry {
  data: SSEWindowPayload;
  updatedAt: number;
}

interface SessionCardProps {
  entry: WindowEntry;
  triggered: boolean;
}

function useRelativeTime(ts: number): string {
  const [label, setLabel] = useState(() => relLabel(ts));
  useEffect(() => {
    const id = setInterval(() => setLabel(relLabel(ts)), 1000);
    return () => clearInterval(id);
  }, [ts]);
  return label;
}

function relLabel(updatedAt: number): string {
  const seconds = Math.floor((Date.now() - updatedAt) / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  return `${Math.floor(seconds / 60)}m ago`;
}

function topProps(eventNameCount: Record<string, number>): [string, number][] {
  return Object.entries(eventNameCount)
    .sort(([, a], [, b]) => b - a)
    .slice(0, 3);
}

function SessionCard({ entry, triggered }: SessionCardProps) {
  const { data } = entry;
  const lastEventAge = useRelativeTime(entry.updatedAt);
  const top = topProps(data.event_name_count ?? {});
  const anonSuffix = data.anonymous_id.slice(-6);

  return (
    <motion.div
      layout
      animate={
        triggered
          ? {
              boxShadow: [
                "0 0 0 rgba(239,68,68,0)",
                "0 0 20px rgba(239,68,68,0.6)",
                "0 0 20px rgba(34,197,94,0.6)",
                "0 0 0 rgba(34,197,94,0)",
              ],
            }
          : {}
      }
      transition={{ duration: 0.8 }}
      className="rounded-lg border border-slate-800 bg-slate-900/60 px-3 py-2.5 text-xs"
    >
      <div className="flex items-center gap-2 mb-1.5">
        <span className="font-mono text-slate-200 font-medium">…{anonSuffix}</span>
        {data.has_error_event && (
          <Badge className="bg-red-900/60 text-red-300 border-red-800 text-[10px] px-1.5 py-0">
            error
          </Badge>
        )}
        {triggered && (
          <motion.span
            initial={{ scale: 0 }}
            animate={{ scale: [0, 1.3, 1] }}
            transition={{ duration: 0.4 }}
            className="ml-auto text-base"
            aria-label="trigger fired"
          >
            🎯
          </motion.span>
        )}
      </div>

      <div className="grid grid-cols-3 gap-x-2 text-slate-400 mb-1.5">
        <div>
          <span className="text-slate-500">events</span>
          <div className="text-slate-200 font-medium">{data.event_count}</div>
        </div>
        <div>
          <span className="text-slate-500">pages</span>
          <div className="text-slate-200 font-medium">
            {Object.keys(data.event_type_count ?? {}).length > 0
              ? (data.event_type_count?.page ?? 0)
              : 0}
          </div>
        </div>
        <div>
          <span className="text-slate-500">last seen</span>
          <div className="text-slate-200 font-medium">{lastEventAge}</div>
        </div>
      </div>

      {top.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {top.map(([name, count]) => (
            <span
              key={name}
              className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full bg-slate-800 text-slate-300 text-[10px]"
            >
              {name}
              <span className="text-slate-500">{count}</span>
            </span>
          ))}
        </div>
      )}
    </motion.div>
  );
}

interface WindowInspectorProps {
  /** Set of anonymousIds that fired a trigger (so we can flash them) */
  triggeredIds?: Set<string>;
}

export function WindowInspector({ triggeredIds = new Set() }: WindowInspectorProps) {
  const [windows, setWindows] = useState<Map<string, WindowEntry>>(new Map());

  const onMessage = useCallback((msg: { event?: string; data: unknown }) => {
    if (msg.event === "windows" || msg.event === "window_pruned" || msg.event === undefined) {
      const payload = msg.data as SSEWindowPayload & { pruned?: boolean };
      if (payload.anonymous_id == null) return;

      setWindows((prev) => {
        const next = new Map(prev);
        if (payload.pruned) {
          next.delete(payload.anonymous_id);
        } else {
          next.set(payload.anonymous_id, {
            data: payload,
            updatedAt: Date.now(),
          });
        }
        return next;
      });
    }
  }, []);

  useSSEStream("windows", onMessage);

  const entries = Array.from(windows.values()).sort(
    (a, b) => b.updatedAt - a.updatedAt
  );

  return (
    <section className="flex flex-col h-full" aria-label="Active Sessions">
      <div className="flex items-center gap-2 px-3 py-2 border-b border-slate-800 shrink-0">
        <span className="text-sm font-medium text-slate-200">Active Sessions</span>
        <Badge
          variant="outline"
          className="text-[10px] border-slate-700 text-slate-400 px-1.5 py-0"
        >
          {windows.size}
        </Badge>
      </div>

      <div className="flex-1 overflow-y-auto p-2 space-y-1.5">
        <AnimatePresence initial={false}>
          {entries.length === 0 && (
            <motion.p
              key="empty"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="text-xs text-slate-500 text-center pt-8"
            >
              No active sessions
            </motion.p>
          )}
          {entries.map((entry) => (
            <SessionCard
              key={entry.data.anonymous_id}
              entry={entry}
              triggered={triggeredIds.has(entry.data.anonymous_id)}
            />
          ))}
        </AnimatePresence>
      </div>
    </section>
  );
}
