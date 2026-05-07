"use client";

/**
 * Email outbox panel — shows all mock emails from the mock_emails SSE stream
 * plus the initial GET /api/mock-emails poll.
 */

import { useState, useCallback, useEffect } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Mail } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { useSSEStream } from "@/lib/sse";
import { EmailViewer } from "@/components/email-viewer/EmailViewer";
import { listMockEmails } from "@/lib/api-client";
import type { MockEmailPayload } from "@/types/api";

interface EmailOutboxProps {
  /** Emails received via SSE from TriggerStream (rs-self persona) */
  streamEmails?: MockEmailPayload[];
}

export function EmailOutbox({ streamEmails = [] }: EmailOutboxProps) {
  const [emails, setEmails] = useState<MockEmailPayload[]>([]);
  const [selected, setSelected] = useState<MockEmailPayload | null>(null);

  // On mount, fetch existing emails from the API so the outbox is populated
  // even when the user wasn't on this tab when triggers fired.
  // Use a functional update to merge with any SSE emails that may have arrived
  // while the GET was in-flight, de-duplicating by id to avoid dropped messages.
  useEffect(() => {
    listMockEmails()
      .then((resp) => {
        setEmails((prev) => {
          const seen = new Set(prev.map((e) => e.id));
          const merged = [...prev];
          for (const e of resp.emails as MockEmailPayload[]) {
            if (!seen.has(e.id)) merged.push(e);
          }
          // Sort newest first so the merged ordering matches what SSE inserts.
          merged.sort((a, b) => (a.created_at < b.created_at ? 1 : -1));
          return merged;
        });
      })
      .catch(() => {
        // Non-fatal: SSE will still deliver new emails.
      });
  }, []);

  const onMessage = useCallback((msg: { event?: string; data: unknown }) => {
    if (msg.event === "reset") {
      // Server-side demo reset: clear email list.
      setEmails([]);
      return;
    }
    if (msg.event === "mock_emails" || msg.event === undefined) {
      const payload = msg.data as MockEmailPayload;
      if (!payload.id) return;
      // De-duplicate: the initial GET may already have this email.
      setEmails((prev) => {
        if (prev.some((e) => e.id === payload.id)) return prev;
        return [payload, ...prev];
      });
    }
  }, []);

  useSSEStream("mock_emails", onMessage);

  // Merge SSE emails with any passed-in stream emails
  const allEmails = [...emails];
  for (const se of streamEmails) {
    if (!allEmails.some((e) => e.id === se.id)) {
      allEmails.unshift(se);
    }
  }

  return (
    <section className="flex flex-col h-full" aria-label="Mock email outbox">
      <div className="flex items-center gap-2 px-3 py-2 border-b border-slate-800 shrink-0">
        <Mail className="w-3.5 h-3.5 text-slate-400" aria-hidden="true" />
        <span className="text-sm font-medium text-slate-200">Mock Emails</span>
        <Badge
          variant="outline"
          className="text-[10px] border-slate-700 text-slate-400 px-1.5 py-0"
        >
          {allEmails.length}
        </Badge>
      </div>

      <div className="flex-1 overflow-y-auto p-2 space-y-1.5">
        <AnimatePresence initial={false}>
          {allEmails.length === 0 && (
            <motion.p
              key="empty"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="text-xs text-slate-500 text-center pt-8"
            >
              No emails sent yet
            </motion.p>
          )}
          {allEmails.map((email) => (
            <motion.button
              key={email.id}
              layout
              initial={{ opacity: 0, y: -8 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.2 }}
              className="w-full text-left rounded-lg border border-slate-800 bg-slate-900/60 px-3 py-2 text-xs hover:border-slate-700 cursor-pointer"
              onClick={() => setSelected(email)}
              aria-label={`View email: ${email.subject}`}
            >
              <div className="flex items-center gap-2 mb-0.5">
                <Mail className="w-3 h-3 text-violet-400 shrink-0" aria-hidden="true" />
                <span className="font-medium text-slate-200 truncate">
                  {email.subject}
                </span>
              </div>
              <div className="flex items-center justify-between text-slate-500">
                <span>{email.to_email}</span>
                <span>{new Date(email.created_at).toLocaleTimeString()}</span>
              </div>
            </motion.button>
          ))}
        </AnimatePresence>
      </div>

      <EmailViewer
        email={selected}
        open={selected !== null}
        onClose={() => setSelected(null)}
      />
    </section>
  );
}
