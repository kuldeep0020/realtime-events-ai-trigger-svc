"use client";

/**
 * Live 3-column dashboard:
 *   Col 1 — EventFeed (live events SSE)
 *   Col 2 — WindowInspector (active sessions SSE)
 *   Col 3 — TriggerStream (trigger fires SSE)
 *
 * Bottom-right floating demo controller and optional email outbox tab.
 */

import { useState, useCallback } from "react";
import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { BrandHeader } from "@/components/shared/BrandHeader";
import { EventFeed } from "@/components/dashboard/EventFeed";
import { WindowInspector } from "@/components/dashboard/WindowInspector";
import { TriggerStream } from "@/components/dashboard/TriggerStream";
import { EmailOutbox } from "@/components/dashboard/EmailOutbox";
import { Controller } from "@/components/demo/Controller";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { Persona } from "@/types/api";

function DashboardContent() {
  const searchParams = useSearchParams();
  const initialTab = searchParams.get("tab") === "emails" ? "emails" : "dashboard";

  const [triggeredIds, setTriggeredIds] = useState<Set<string>>(new Set());
  const [highlightedIds, setHighlightedIds] = useState<Set<string>>(new Set());
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const [activePersona, setActivePersona] = useState<Persona | null>(null);
  const [showController, setShowController] = useState(true);

  const handleTriggerFired = useCallback((anonymousId: string) => {
    // Flash the session card and connected events
    setTriggeredIds((prev) => {
      const next = new Set(prev);
      next.add(anonymousId);
      return next;
    });
    setHighlightedIds((prev) => {
      const next = new Set(prev);
      next.add(anonymousId);
      return next;
    });
    // Clear after animation completes
    setTimeout(() => {
      setTriggeredIds((prev) => {
        const next = new Set(prev);
        next.delete(anonymousId);
        return next;
      });
      setHighlightedIds((prev) => {
        const next = new Set(prev);
        next.delete(anonymousId);
        return next;
      });
    }, 1200);
  }, []);

  return (
    <div className="flex flex-col h-screen overflow-hidden bg-slate-950">
      <BrandHeader />

      <Tabs
        defaultValue={initialTab}
        className="flex-1 flex flex-col overflow-hidden"
      >
        <div className="flex items-center gap-2 px-4 py-1.5 border-b border-slate-800 bg-slate-950 shrink-0">
          <TabsList className="bg-slate-900 border border-slate-800 h-7">
            <TabsTrigger
              value="dashboard"
              className="text-xs h-6 px-3 data-[state=active]:bg-slate-800"
            >
              Dashboard
            </TabsTrigger>
            <TabsTrigger
              value="emails"
              className="text-xs h-6 px-3 data-[state=active]:bg-slate-800"
            >
              Emails
            </TabsTrigger>
          </TabsList>
          <button
            className="ml-auto text-xs text-slate-500 hover:text-slate-300 px-2 py-0.5 rounded border border-slate-800 hover:border-slate-700"
            onClick={() => setShowController((v) => !v)}
            aria-label={showController ? "Hide demo controller" : "Show demo controller"}
            aria-pressed={showController}
          >
            {showController ? "Hide controls" : "Show controls"}
          </button>
        </div>

        {/* Main 3-column dashboard */}
        <TabsContent
          value="dashboard"
          className="flex-1 overflow-hidden m-0"
        >
          <div className="grid h-full grid-cols-3 divide-x divide-slate-800/60">
            {/* Column 1 — Live events */}
            <div className="min-h-0 overflow-hidden">
              <EventFeed highlightedIds={highlightedIds} />
            </div>

            {/* Column 2 — Active sessions */}
            <div className="min-h-0 overflow-hidden">
              <WindowInspector triggeredIds={triggeredIds} />
            </div>

            {/* Column 3 — Trigger fires */}
            <div className="min-h-0 overflow-hidden">
              <TriggerStream onTriggerFired={handleTriggerFired} />
            </div>
          </div>
        </TabsContent>

        {/* Email outbox tab */}
        <TabsContent
          value="emails"
          className="flex-1 overflow-hidden m-0"
        >
          <div className="h-full max-w-2xl mx-auto">
            <EmailOutbox />
          </div>
        </TabsContent>
      </Tabs>

      {/* Floating demo controller — bottom right */}
      {showController && (
        <div
          className="fixed bottom-4 right-4 z-50"
          aria-label="Floating demo controller"
        >
          <Controller compact onPersonaChange={setActivePersona} />
        </div>
      )}
    </div>
  );
}

export default function DashboardPage() {
  return (
    <Suspense
      fallback={
        <div className="flex h-screen items-center justify-center bg-slate-950 text-slate-400 text-sm">
          Loading dashboard…
        </div>
      }
    >
      <DashboardContent />
    </Suspense>
  );
}
