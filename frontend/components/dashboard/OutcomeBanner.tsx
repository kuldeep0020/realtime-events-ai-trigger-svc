"use client";

/**
 * OutcomeBanner — horizontal banner above the dashboard columns.
 *
 * Subscribes to the triggers SSE stream. Maintains a queue of unseen
 * triggers received since mount; cycles display at CYCLE_INTERVAL_MS,
 * keeping the last item visible for LINGER_MS before the queue drains
 * to nothing.
 *
 * Three display variants:
 *   A. realtor_known_high_intent  → blue-50 tint
 *   B. realtor_anonymous_high_intent → amber-50 tint
 *   C. rs_destination_error | rs_onboarding_stuck → emerald-50 tint
 */

import { useState, useEffect, useCallback, useRef } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { useSSEStream } from "@/lib/sse";
import type { SSETriggerPayload } from "@/types/api";

// ─── Timing constants ─────────────────────────────────────────────────────────

const CYCLE_INTERVAL_MS = 4_000;
const LINGER_MS = 12_000;
const MAX_QUEUE = 5;

// ─── Variant helpers ──────────────────────────────────────────────────────────

type BannerVariant = "known" | "anonymous" | "rs-self";

function classifyTrigger(payload: SSETriggerPayload): BannerVariant {
  const rule = payload.rule_name ?? "";
  if (rule === "realtor_known_high_intent") return "known";
  if (rule === "realtor_anonymous_high_intent") return "anonymous";
  return "rs-self";
}

function variantBg(variant: BannerVariant): string {
  switch (variant) {
    case "known":
      return "bg-blue-50 border-blue-200 text-blue-900";
    case "anonymous":
      return "bg-amber-50 border-amber-200 text-amber-900";
    case "rs-self":
      return "bg-emerald-50 border-emerald-200 text-emerald-900";
  }
}

function dotColor(variant: BannerVariant): string {
  switch (variant) {
    case "known":
      return "bg-blue-400";
    case "anonymous":
      return "bg-amber-400";
    case "rs-self":
      return "bg-emerald-500";
  }
}

// ─── Text builders ────────────────────────────────────────────────────────────

function buildBannerText(payload: SSETriggerPayload): string {
  const variant = classifyTrigger(payload);
  const traits = (payload as unknown as Record<string, unknown>)
    .enriched_traits as Record<string, unknown> | undefined ?? {};
  const realtor = (payload as unknown as Record<string, unknown>)
    .assigned_realtor as Record<string, unknown> | undefined ?? {};
  const llm = payload.llm_parsed ?? {};
  const snap = payload.window_snapshot ?? {};

  if (variant === "known") {
    const realtorName =
      (realtor.name as string | undefined) ?? "—";
    const firstName =
      (traits.first_name as string | undefined) ?? "";
    const lastName =
      (traits.last_name as string | undefined) ?? "";
    const fullName = [firstName, lastName].filter(Boolean).join(" ") || "—";
    const dealValue =
      (llm.estimated_deal_value as string | undefined) ?? "—";
    const urgency =
      (llm.urgency_minutes as string | number | undefined) ??
      (llm.urgency as string | undefined) ??
      "30";
    return `🏡 → Realtor ${realtorName} alerted for ${fullName}   |   Est. deal value: ${dealValue}   |   Response window: ${urgency} min`;
  }

  if (variant === "anonymous") {
    const suburb =
      (snap.dominant_suburb as string | undefined) ?? "—";
    const recommendedAction =
      (llm.recommended_action as string | undefined) ?? "in-app banner";
    const realtorName =
      (realtor.name as string | undefined) ?? "—";
    return `🕵️ → Anonymous high-intent visitor in ${suburb}   |   Recommended action: ${recommendedAction}   |   Standby realtor: ${realtorName}`;
  }

  // rs-self
  const firstName =
    (traits.first_name as string | undefined) ?? "—";
  const company =
    (traits.company as string | undefined) ?? "—";
  const eta =
    (llm.fix_eta_minutes as string | number | undefined) ?? "5";
  return `✉️ → Personalized fix sent to ${firstName} at ${company}   |   ETA to unblock: ${eta} min   |   Track in mock-email outbox →`;
}

// ─── Queue item ───────────────────────────────────────────────────────────────

interface QueueItem {
  key: string;
  payload: SSETriggerPayload;
}

// ─── Component ────────────────────────────────────────────────────────────────

export function OutcomeBanner() {
  const [queue, setQueue] = useState<QueueItem[]>([]);
  const [currentIndex, setCurrentIndex] = useState(0);
  const lingerTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cycleTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Enqueue incoming triggers
  const onMessage = useCallback(
    (msg: { event?: string; data: unknown }) => {
      if (msg.event === "reset") {
        setQueue([]);
        setCurrentIndex(0);
        return;
      }
      if (msg.event === "triggers" || msg.event === undefined) {
        const payload = msg.data as SSETriggerPayload;
        if (!payload?.id) return;
        setQueue((prev) => {
          if (prev.some((q) => q.key === payload.id)) return prev;
          const next = [...prev, { key: payload.id, payload }];
          // Cap at MAX_QUEUE by dropping the oldest entry; the useEffect
          // that depends on queue.length already clamps currentIndex via
          // Math.min(prev, queue.length - 1), so no extra clamp needed here.
          return next.length > MAX_QUEUE ? next.slice(next.length - MAX_QUEUE) : next;
        });
      }
    },
    []
  );

  useSSEStream("triggers", onMessage);

  // Cycle through items in queue, then linger on the last one for LINGER_MS
  useEffect(() => {
    if (queue.length === 0) {
      setCurrentIndex(0);
      return;
    }

    // Clamp currentIndex to the last valid position
    setCurrentIndex((prev) => Math.min(prev, queue.length - 1));

    // Clear existing timers
    if (cycleTimerRef.current) {
      clearInterval(cycleTimerRef.current);
      cycleTimerRef.current = null;
    }
    if (lingerTimerRef.current) {
      clearTimeout(lingerTimerRef.current);
      lingerTimerRef.current = null;
    }

    cycleTimerRef.current = setInterval(() => {
      setCurrentIndex((prev) => {
        if (prev < queue.length - 1) {
          return prev + 1;
        }
        // On last item — stop cycling, schedule linger then clear
        if (cycleTimerRef.current) {
          clearInterval(cycleTimerRef.current);
          cycleTimerRef.current = null;
        }
        lingerTimerRef.current = setTimeout(() => {
          setQueue([]);
          setCurrentIndex(0);
        }, LINGER_MS);
        return prev;
      });
    }, CYCLE_INTERVAL_MS);

    return () => {
      if (cycleTimerRef.current) clearInterval(cycleTimerRef.current);
      if (lingerTimerRef.current) clearTimeout(lingerTimerRef.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [queue.length]);

  if (queue.length === 0) return null;

  const current = queue[currentIndex];
  if (!current) return null;

  const variant = classifyTrigger(current.payload);
  const text = buildBannerText(current.payload);
  const bgCls = variantBg(variant);

  return (
    <div
      className="relative px-4 py-1.5 shrink-0"
      aria-live="polite"
      aria-atomic="true"
    >
      <AnimatePresence mode="wait">
        <motion.div
          key={current.key}
          initial={{ opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 8 }}
          transition={{ duration: 0.25, ease: "easeOut" }}
          className={`flex items-center gap-3 rounded-lg border px-4 py-2 text-sm font-medium shadow-sm ${bgCls}`}
        >
          <span className="flex-1 truncate">{text}</span>

          {/* Dot indicators — one per queued item */}
          {queue.length > 1 && (
            <div
              className="flex items-center gap-1 shrink-0"
              aria-label={`${currentIndex + 1} of ${queue.length}`}
            >
              {queue.map((_, i) => (
                <span
                  key={i}
                  className={`inline-block h-1.5 w-1.5 rounded-full transition-opacity ${
                    dotColor(variant)
                  } ${i === currentIndex ? "opacity-100" : "opacity-30"}`}
                />
              ))}
            </div>
          )}
        </motion.div>
      </AnimatePresence>
    </div>
  );
}
