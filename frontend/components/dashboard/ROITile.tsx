"use client";

/**
 * ROITile — compact stat card pinned above the dashboard columns.
 *
 * Subscribes to the triggers SSE stream and computes three numbers
 * client-side from the running history:
 *   1. Triggers fired: count of SSE messages received since mount
 *   2. Est. revenue protected: sum of deal values + $40K per rs-self trigger
 *   3. Avg. time-to-action: hard-coded "~6s" (v1 simplification per spec §10)
 *
 * Empty state: "Fire a script to see live impact"
 */

import { useState, useCallback, useRef } from "react";
import { useSSEStream } from "@/lib/sse";
import { Card } from "@/components/ui/card";
import type { SSETriggerPayload } from "@/types/api";

// ─── Revenue helpers ──────────────────────────────────────────────────────────

const RS_SELF_FLAT_VALUE = 40_000;

/** Parse "$1,234,567" or "1234567" → number; returns 0 on failure. */
function parseDealValue(raw: unknown): number {
  if (typeof raw !== "string" && typeof raw !== "number") return 0;
  const cleaned = String(raw).replace(/[$,]/g, "").trim();
  const n = parseFloat(cleaned);
  return isFinite(n) ? n : 0;
}

function isRealestatePersona(payload: SSETriggerPayload): boolean {
  return payload.persona === "realestate";
}

/** Format a dollar amount as "$X.XM" (one decimal million). */
function formatMillions(dollars: number): string {
  if (dollars === 0) return "$0";
  if (dollars >= 1_000_000) {
    return `$${(dollars / 1_000_000).toFixed(1)}M`;
  }
  if (dollars >= 1_000) {
    return `$${(dollars / 1_000).toFixed(0)}K`;
  }
  return `$${dollars.toLocaleString()}`;
}

// ─── Stat card sub-component ──────────────────────────────────────────────────

function StatCard({
  label,
  value,
  sub,
}: {
  label: string;
  value: string;
  sub?: string;
}) {
  return (
    <Card className="flex-1 border-slate-800 bg-slate-900/60 px-4 py-3">
      <p className="text-xs uppercase tracking-wide opacity-60 text-slate-300 mb-1">
        {label}
      </p>
      <p className="text-2xl font-bold text-slate-100 leading-none">
        {value}
        {sub && (
          <span className="text-xs font-normal text-slate-400 ml-1">{sub}</span>
        )}
      </p>
    </Card>
  );
}

// ─── Main component ───────────────────────────────────────────────────────────

interface ROIState {
  triggerCount: number;
  totalRevenue: number;
}

export function ROITile() {
  const [roi, setRoi] = useState<ROIState>({ triggerCount: 0, totalRevenue: 0 });
  const seenIds = useRef<Set<string>>(new Set());

  const onMessage = useCallback(
    (msg: { event?: string; data: unknown }) => {
      if (msg.event === "reset") {
        seenIds.current.clear();
        setRoi({ triggerCount: 0, totalRevenue: 0 });
        return;
      }
      if (msg.event === "triggers" || msg.event === undefined) {
        const payload = msg.data as SSETriggerPayload;
        if (!payload?.id) return;
        if (seenIds.current.has(payload.id)) return;
        seenIds.current.add(payload.id);

        setRoi((prev) => {
          let additional = 0;
          if (isRealestatePersona(payload)) {
            const llm = payload.llm_parsed ?? {};
            additional = parseDealValue(llm.estimated_deal_value);
          } else {
            // rs-self: flat $40K per trigger
            additional = RS_SELF_FLAT_VALUE;
          }
          return {
            triggerCount: prev.triggerCount + 1,
            totalRevenue: prev.totalRevenue + additional,
          };
        });
      }
    },
    []
  );

  useSSEStream("triggers", onMessage);

  if (roi.triggerCount === 0) {
    return (
      <div className="px-4 py-2 shrink-0">
        <Card className="border-slate-800 bg-slate-900/40 px-4 py-2.5">
          <p className="text-xs text-slate-500 text-center">
            Fire a script to see live impact
          </p>
        </Card>
      </div>
    );
  }

  return (
    <div className="px-4 py-2 shrink-0" aria-label="ROI summary">
      <div className="flex gap-4">
        <StatCard
          label="Triggers fired"
          value={String(roi.triggerCount)}
        />
        <StatCard
          label="Est. revenue protected"
          value={formatMillions(roi.totalRevenue)}
          sub="(est.)"
        />
        <StatCard
          label="Avg. time-to-action"
          value="~6s"
        />
      </div>
    </div>
  );
}
