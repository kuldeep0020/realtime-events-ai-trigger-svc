"use client";

/**
 * Column 3 — Trigger fire stream.
 * Shows trigger cards (newest on top) with three expandable subsections:
 * Why / Decision / Delivered. The "Decision" section renders differently
 * based on persona (real-estate Slack card vs rs-self email card).
 */

import { useState, useCallback } from "react";
import { motion, AnimatePresence } from "framer-motion";
import ReactMarkdown from "react-markdown";
import { ExternalLink, Target, CheckCircle, Clock, AlertCircle, Mail } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useSSEStream } from "@/lib/sse";
import { EmailViewer } from "@/components/email-viewer/EmailViewer";
import type { SSETriggerPayload, MockEmailPayload } from "@/types/api";

interface TriggerEntry {
  id: string;
  payload: SSETriggerPayload;
  receivedAt: number;
}

// ─── Real-estate LLM output shape ───────────────────────────────────────────

interface RealestateAction extends Record<string, unknown> {
  headline: string;
  talking_points: string[];
  best_cta: string;
  urgency: "high" | "medium" | "low";
  assigned_realtor?: string;
}

function isRealestateAction(obj: Record<string, unknown>): obj is RealestateAction {
  return typeof obj.headline === "string" && Array.isArray(obj.talking_points);
}

// ─── RS-self LLM output shape ────────────────────────────────────────────────

interface RsSelfAction extends Record<string, unknown> {
  subject: string;
  body_markdown: string;
  doc_links?: Array<{ title: string; url: string }>;
  next_step_cta?: string;
}

function isRsSelfAction(obj: Record<string, unknown>): obj is RsSelfAction {
  return typeof obj.subject === "string" && typeof obj.body_markdown === "string";
}

// ─── Status badge ─────────────────────────────────────────────────────────────

function StatusBadge({ status }: { status: string }) {
  const map: Record<string, { label: string; cls: string; icon: React.ReactNode }> = {
    pending: {
      label: "pending",
      cls: "bg-amber-900/40 text-amber-300 border-amber-800",
      icon: <Clock className="w-3 h-3" aria-hidden="true" />,
    },
    sent: {
      label: "sent",
      cls: "bg-emerald-900/40 text-emerald-300 border-emerald-800",
      icon: <CheckCircle className="w-3 h-3" aria-hidden="true" />,
    },
    delivered: {
      label: "delivered",
      cls: "bg-emerald-900/40 text-emerald-300 border-emerald-800",
      icon: <CheckCircle className="w-3 h-3" aria-hidden="true" />,
    },
    failed: {
      label: "failed",
      cls: "bg-red-900/40 text-red-300 border-red-800",
      icon: <AlertCircle className="w-3 h-3" aria-hidden="true" />,
    },
  };
  const { label, cls, icon } = map[status] ?? {
    label: status,
    cls: "bg-slate-800 text-slate-400 border-slate-700",
    icon: null,
  };
  return (
    <span
      className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full border text-[10px] font-medium ${cls}`}
    >
      {icon}
      {label}
    </span>
  );
}

// ─── Real-estate Slack-style card ─────────────────────────────────────────────

function RealestateDecision({ action }: { action: RealestateAction }) {
  const urgencyMap = {
    high: "bg-red-900/40 text-red-300 border-red-700",
    medium: "bg-amber-900/40 text-amber-300 border-amber-700",
    low: "bg-slate-800 text-slate-400 border-slate-700",
  };
  return (
    <div className="rounded-lg border border-slate-700 overflow-hidden">
      {/* Slack-style left accent bar + header */}
      <div className="flex">
        <div className="w-1 bg-green-500 shrink-0" aria-hidden="true" />
        <div className="flex-1 px-3 py-2.5">
          <div className="flex items-start justify-between gap-2 mb-2">
            <p className="text-sm font-semibold text-slate-100 leading-snug">
              {action.headline}
            </p>
            <span
              className={`inline-flex items-center px-2 py-0.5 rounded-full border text-[10px] font-medium shrink-0 ${
                urgencyMap[action.urgency] ?? urgencyMap.low
              }`}
            >
              {action.urgency}
            </span>
          </div>

          {/* Talking points as bullets */}
          <ul className="space-y-1 mb-2" aria-label="Talking points">
            {action.talking_points.map((pt, i) => (
              <li key={i} className="flex items-start gap-1.5 text-xs text-slate-300">
                <span className="text-green-400 mt-0.5 shrink-0" aria-hidden="true">
                  •
                </span>
                {pt}
              </li>
            ))}
          </ul>

          {/* CTA */}
          <div className="flex items-center justify-between gap-2 pt-2 border-t border-slate-800">
            <p className="text-xs text-violet-300 font-medium">{action.best_cta}</p>
            {action.assigned_realtor && (
              <Badge
                variant="outline"
                className="text-[10px] border-slate-600 text-slate-400 shrink-0"
              >
                {action.assigned_realtor}
              </Badge>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── RS-self email preview ────────────────────────────────────────────────────

function RsSelfDecision({
  action,
  onViewEmail,
  emailPayload,
}: {
  action: RsSelfAction;
  onViewEmail: () => void;
  emailPayload: MockEmailPayload | null;
}) {
  return (
    <div className="rounded-lg border border-slate-700 overflow-hidden">
      {/* Subject line */}
      <div className="px-3 py-2 border-b border-slate-800 bg-slate-800/40">
        <p className="text-xs text-slate-400 mb-0.5">Subject</p>
        <p className="text-sm font-medium text-slate-100">{action.subject}</p>
      </div>

      {/* Body preview (first 200 chars) */}
      <div className="px-3 py-2.5">
        <div className="text-xs text-slate-300 leading-relaxed line-clamp-4 prose prose-invert prose-xs max-w-none">
          <ReactMarkdown
            components={{
              p: ({ children }) => <p className="mb-1 last:mb-0">{children}</p>,
              strong: ({ children }) => (
                <strong className="text-slate-100">{children}</strong>
              ),
              code: ({ children }) => (
                <code className="bg-slate-800 text-violet-300 font-mono px-1 rounded text-[10px]">
                  {children}
                </code>
              ),
              h3: ({ children }) => (
                <strong className="text-slate-200 block mb-0.5">{children}</strong>
              ),
            }}
          >
            {action.body_markdown.split("\n").slice(0, 8).join("\n")}
          </ReactMarkdown>
        </div>

        {/* Doc links */}
        {action.doc_links && action.doc_links.length > 0 && (
          <div className="mt-2 pt-2 border-t border-slate-800">
            <ul className="space-y-1" aria-label="Documentation links">
              {action.doc_links.map((link) => (
                <li key={link.url}>
                  <a
                    href={link.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-1 text-[10px] text-violet-400 hover:text-violet-300"
                  >
                    <ExternalLink className="w-2.5 h-2.5 shrink-0" aria-hidden="true" />
                    {link.title}
                  </a>
                </li>
              ))}
            </ul>
          </div>
        )}

        {/* View full email button */}
        <Button
          variant="outline"
          size="sm"
          className="mt-2.5 border-slate-700 text-violet-400 hover:text-violet-300 text-xs h-7 w-full"
          onClick={onViewEmail}
          aria-label="View full email"
        >
          <Mail className="w-3 h-3 mr-1.5" aria-hidden="true" />
          View email
        </Button>
      </div>

      {/* Hidden full email for dialog */}
      {emailPayload && <span className="hidden">{emailPayload.id}</span>}
    </div>
  );
}

// ─── Individual trigger card ──────────────────────────────────────────────────

function TriggerCard({ entry }: { entry: TriggerEntry }) {
  const { payload } = entry;
  const [emailOpen, setEmailOpen] = useState(false);

  const llm = payload.llm_parsed ?? {};
  const isRealestate = isRealestateAction(llm);
  const isRsSelf = !isRealestate && isRsSelfAction(llm);

  // Build a mock email payload from trigger for the email viewer
  const emailPayload: MockEmailPayload | null = isRsSelf
    ? {
        id: payload.id,
        trigger_id: payload.id,
        to_email: `${payload.anonymous_id}@example.com`,
        subject: (llm as RsSelfAction).subject,
        body_markdown: (llm as RsSelfAction).body_markdown,
        links: (llm as RsSelfAction).doc_links,
        created_at: payload.fired_at,
      }
    : null;

  const snapshot = payload.window_snapshot;

  return (
    <motion.div
      layout
      initial={{ opacity: 0, y: -12 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, scale: 0.97 }}
      transition={{ duration: 0.25 }}
      className="rounded-lg border border-slate-800 bg-slate-900/60 overflow-hidden"
    >
      {/* Trigger header */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-slate-800/60">
        <Target className="w-3.5 h-3.5 text-violet-400 shrink-0" aria-hidden="true" />
        <span className="text-xs font-medium text-slate-200 truncate">
          {payload.rule_name}
        </span>
        <Badge
          variant="outline"
          className="text-[10px] border-slate-700 text-slate-400 ml-auto shrink-0 px-1.5 py-0"
        >
          {payload.persona}
        </Badge>
      </div>

      <div className="divide-y divide-slate-800/60">
        {/* Why */}
        <details className="group">
          <summary className="flex items-center gap-1.5 px-3 py-2 cursor-pointer list-none select-none text-xs text-slate-400 hover:text-slate-300">
            <span className="font-medium">Why</span>
            <span className="ml-auto text-[10px] text-slate-600 group-open:rotate-180 transition-transform">
              ▾
            </span>
          </summary>
          <div className="px-3 pb-2.5 text-xs text-slate-400 space-y-1">
            <div>
              <span className="text-slate-500">Events: </span>
              <span className="text-slate-300">
                {typeof snapshot.event_count === "number" ? snapshot.event_count : "—"}
              </span>
            </div>
            {typeof snapshot.idle_seconds === "number" && (
              <div>
                <span className="text-slate-500">Idle: </span>
                <span className="text-slate-300">{snapshot.idle_seconds}s</span>
              </div>
            )}
            {typeof snapshot.event_name_count === "object" &&
              snapshot.event_name_count !== null && (
                <div>
                  <span className="text-slate-500">Top events: </span>
                  <span className="text-slate-300">
                    {Object.entries(snapshot.event_name_count as Record<string, number>)
                      .sort(([, a], [, b]) => (b as number) - (a as number))
                      .slice(0, 3)
                      .map(([k, v]) => `${k} (${v})`)
                      .join(", ")}
                  </span>
                </div>
              )}
          </div>
        </details>

        {/* Decision — expanded by default */}
        <details open>
          <summary className="flex items-center gap-1.5 px-3 py-2 cursor-pointer list-none select-none text-xs text-slate-200 hover:text-slate-100 font-medium">
            Decision
            <span className="ml-auto text-[10px] text-slate-600">▾</span>
          </summary>
          <div className="px-3 pb-3">
            {isRealestate && (
              <RealestateDecision action={llm as RealestateAction} />
            )}
            {isRsSelf && (
              <RsSelfDecision
                action={llm as RsSelfAction}
                onViewEmail={() => setEmailOpen(true)}
                emailPayload={emailPayload}
              />
            )}
            {!isRealestate && !isRsSelf && (
              <pre className="text-[10px] font-mono text-slate-400 whitespace-pre-wrap break-all">
                {JSON.stringify(llm, null, 2)}
              </pre>
            )}
          </div>
        </details>

        {/* Delivered */}
        <details>
          <summary className="flex items-center gap-1.5 px-3 py-2 cursor-pointer list-none select-none text-xs text-slate-400 hover:text-slate-300">
            <span className="font-medium">Delivered</span>
            <span className="ml-auto text-[10px] text-slate-600">▾</span>
          </summary>
          <div className="px-3 pb-3 text-xs space-y-1.5">
            <div className="flex items-center gap-2">
              <StatusBadge status={payload.dispatch_status} />
              <span className="text-slate-400">{payload.destination}</span>
            </div>
            <div className="text-slate-500">
              {new Date(payload.fired_at).toLocaleTimeString()}
            </div>
          </div>
        </details>
      </div>

      {/* Email viewer dialog for rs-self */}
      {emailPayload && (
        <EmailViewer
          email={emailPayload}
          open={emailOpen}
          onClose={() => setEmailOpen(false)}
        />
      )}
    </motion.div>
  );
}

// ─── Main export ──────────────────────────────────────────────────────────────

interface TriggerStreamProps {
  /** Called when a new trigger fires so other columns can react */
  onTriggerFired?: (anonymousId: string) => void;
}

export function TriggerStream({ onTriggerFired }: TriggerStreamProps) {
  const [triggers, setTriggers] = useState<TriggerEntry[]>([]);

  const onMessage = useCallback(
    (msg: { event?: string; data: unknown }) => {
      if (msg.event === "reset") {
        // Server-side demo reset: clear trigger list.
        setTriggers([]);
        return;
      }
      if (msg.event === "triggers" || msg.event === undefined) {
        const payload = msg.data as SSETriggerPayload;
        if (!payload.id) return;
        setTriggers((prev) => {
          // Avoid duplicates
          if (prev.some((t) => t.id === payload.id)) return prev;
          const next = [
            { id: payload.id, payload, receivedAt: Date.now() },
            ...prev,
          ];
          return next;
        });
        onTriggerFired?.(payload.anonymous_id);
      }
    },
    [onTriggerFired]
  );

  useSSEStream("triggers", onMessage);

  return (
    <section className="flex flex-col h-full" aria-label="Triggers Fired">
      <div className="flex items-center gap-2 px-3 py-2 border-b border-slate-800 shrink-0">
        <span className="text-sm font-medium text-slate-200">Triggers Fired</span>
        <Badge
          variant="outline"
          className="text-[10px] border-slate-700 text-slate-400 px-1.5 py-0"
        >
          {triggers.length}
        </Badge>
      </div>

      <div className="flex-1 overflow-y-auto p-2 space-y-2">
        <AnimatePresence initial={false}>
          {triggers.length === 0 && (
            <motion.p
              key="empty"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="text-xs text-slate-500 text-center pt-8"
            >
              No triggers fired yet
            </motion.p>
          )}
          {triggers.map((entry) => (
            <TriggerCard key={entry.id} entry={entry} />
          ))}
        </AnimatePresence>
      </div>
    </section>
  );
}
