"use client";

/**
 * Demo controller — two big fire buttons, reset, replay-last-trigger, persona badge.
 * Shared between /demo (full page) and a floating panel on /dashboard.
 */

import { useState, useCallback, useRef, useEffect } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Zap, RotateCcw, RefreshCw, Building2, Code2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { fireScript, demoReset, replayLastTrigger } from "@/lib/api-client";
import type { Persona } from "@/types/api";

// Script fire durations (approximate)
const SCRIPT_DURATIONS: Record<Persona, number> = {
  realestate: 22_000,
  "rs-self": 12_000,
};

interface FiringState {
  persona: Persona;
  startedAt: number;
  duration: number;
}

interface ControllerProps {
  /** Whether to render in compact floating-panel mode */
  compact?: boolean;
  /** Called when a script fires (for dashboard to know active persona) */
  onPersonaChange?: (persona: Persona) => void;
}

export function Controller({ compact = false, onPersonaChange }: ControllerProps) {
  const [firing, setFiring] = useState<FiringState | null>(null);
  const [progress, setProgress] = useState(0);
  const [resetDialogOpen, setResetDialogOpen] = useState(false);
  const [activePersona, setActivePersona] = useState<Persona | null>(null);
  const [statusMessage, setStatusMessage] = useState<string | null>(null);
  const rafRef = useRef<number | null>(null);

  // Cancel any pending RAF on unmount
  useEffect(() => {
    return () => {
      if (rafRef.current !== null) cancelAnimationFrame(rafRef.current);
    };
  }, []);

  // Animate progress bar while script fires
  const startProgressAnimation = useCallback((persona: Persona) => {
    const duration = SCRIPT_DURATIONS[persona];
    const started = Date.now();
    setProgress(0);

    const tick = () => {
      const elapsed = Date.now() - started;
      const pct = Math.min(100, (elapsed / duration) * 100);
      setProgress(pct);
      if (pct < 100) {
        rafRef.current = requestAnimationFrame(tick);
      }
    };
    rafRef.current = requestAnimationFrame(tick);
  }, []);

  const handleFireScript = useCallback(
    async (persona: Persona) => {
      if (firing) return;
      setFiring({
        persona,
        startedAt: Date.now(),
        duration: SCRIPT_DURATIONS[persona],
      });
      setActivePersona(persona);
      setStatusMessage("Resetting state…");
      onPersonaChange?.(persona);

      try {
        await demoReset();
      } catch (err) {
        // Non-fatal — failed reset should not block the fire.
        console.warn("auto-reset failed; proceeding with fire", err);
      }

      startProgressAnimation(persona);

      try {
        const result = await fireScript({ persona });
        setStatusMessage(`Fired ${result.event_count} events — ${result.status}`);
      } catch (err) {
        setStatusMessage(`Error: ${err instanceof Error ? err.message : "unknown"}`);
      } finally {
        setFiring(null);
        setProgress(100);
        setTimeout(() => setProgress(0), 1000);
      }
    },
    [firing, onPersonaChange, startProgressAnimation]
  );

  const handleReset = useCallback(async () => {
    setResetDialogOpen(false);
    try {
      await demoReset();
      setStatusMessage("Demo reset — events, triggers, and cooldowns cleared");
      setActivePersona(null);
    } catch (err) {
      setStatusMessage(`Reset failed: ${err instanceof Error ? err.message : "unknown"}`);
    }
  }, []);

  const handleReplay = useCallback(async () => {
    try {
      await replayLastTrigger();
      setStatusMessage("Last trigger replayed — see Triggers Fired column");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "unknown";
      // 404 means no triggers have fired yet — present a friendly hint rather
      // than a scary "failed" message.
      if (msg.toLowerCase().includes("no triggers")) {
        setStatusMessage("No triggers to replay yet — fire a script first");
      } else {
        setStatusMessage(`Replay failed: ${msg}`);
      }
    }
  }, []);

  if (compact) {
    return (
      <div
        className="flex flex-col gap-2 p-3 bg-slate-900/95 border border-slate-700 rounded-xl backdrop-blur-sm shadow-xl w-56"
        role="region"
        aria-label="Demo controller"
      >
        <div className="flex items-center justify-between mb-1">
          <span className="text-xs font-medium text-slate-300">Demo Controls</span>
          {activePersona && (
            <Badge
              variant="outline"
              className="text-[10px] border-slate-700 text-violet-400 px-1.5 py-0"
            >
              {activePersona}
            </Badge>
          )}
        </div>

        <Button
          size="sm"
          disabled={firing !== null}
          onClick={() => handleFireScript("realestate")}
          className="bg-violet-700 hover:bg-violet-600 text-white h-8 text-xs w-full"
          aria-label="Fire real-estate demo script"
        >
          <Building2 className="w-3 h-3 mr-1.5" aria-hidden="true" />
          Real-estate
        </Button>
        <Button
          size="sm"
          disabled={firing !== null}
          onClick={() => handleFireScript("rs-self")}
          className="bg-blue-700 hover:bg-blue-600 text-white h-8 text-xs w-full"
          aria-label="Fire RS-self demo script"
        >
          <Code2 className="w-3 h-3 mr-1.5" aria-hidden="true" />
          RS-self
        </Button>

        {firing && (
          <Progress
            value={progress}
            className="h-1 bg-slate-800"
            aria-label="Script progress"
          />
        )}

        <div className="flex gap-1.5">
          <Button
            size="sm"
            variant="outline"
            className="border-slate-700 text-slate-400 hover:text-slate-200 text-[10px] h-7 flex-1"
            onClick={handleReplay}
            aria-label="Replay last trigger"
          >
            <RefreshCw className="w-3 h-3" aria-hidden="true" />
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="border-red-900/60 text-red-400 hover:text-red-300 text-[10px] h-7 flex-1"
            onClick={() => setResetDialogOpen(true)}
            aria-label="Reset demo"
          >
            <RotateCcw className="w-3 h-3" aria-hidden="true" />
          </Button>
        </div>

        {statusMessage && (
          <p className="text-[10px] text-slate-400 text-center">{statusMessage}</p>
        )}

        <ResetDialog
          open={resetDialogOpen}
          onConfirm={handleReset}
          onCancel={() => setResetDialogOpen(false)}
        />
      </div>
    );
  }

  // Full-page layout
  return (
    <div
      className="flex flex-col gap-6 max-w-lg w-full mx-auto"
      role="region"
      aria-label="Demo controller"
    >
      {/* Persona indicator */}
      <div className="flex items-center gap-3">
        <span className="text-sm text-slate-400">Active persona:</span>
        {activePersona ? (
          <Badge
            variant="outline"
            className="border-violet-700 text-violet-300 text-sm px-3 py-1"
          >
            {activePersona}
          </Badge>
        ) : (
          <Badge
            variant="outline"
            className="border-slate-700 text-slate-500 text-sm px-3 py-1"
          >
            none selected
          </Badge>
        )}
      </div>

      {/* Big fire buttons */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Button
          size="lg"
          disabled={firing !== null}
          onClick={() => handleFireScript("realestate")}
          className="h-20 text-base bg-violet-700 hover:bg-violet-600 text-white flex-col gap-1.5 disabled:opacity-50"
          aria-label="Fire Real-Estate demo script"
          aria-busy={firing?.persona === "realestate"}
        >
          <Building2 className="w-5 h-5" aria-hidden="true" />
          Fire Real-Estate Script
          <span className="text-xs font-normal opacity-70">~22s</span>
        </Button>

        <Button
          size="lg"
          disabled={firing !== null}
          onClick={() => handleFireScript("rs-self")}
          className="h-20 text-base bg-blue-700 hover:bg-blue-600 text-white flex-col gap-1.5 disabled:opacity-50"
          aria-label="Fire RS-Self demo script"
          aria-busy={firing?.persona === "rs-self"}
        >
          <Code2 className="w-5 h-5" aria-hidden="true" />
          Fire RS-Self Script
          <span className="text-xs font-normal opacity-70">~12s</span>
        </Button>
      </div>

      {/* Progress bar */}
      <AnimatePresence>
        {firing && (
          <motion.div
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: "auto" }}
            exit={{ opacity: 0, height: 0 }}
            className="space-y-1.5"
          >
            <div className="flex justify-between text-xs text-slate-400">
              <span>
                <Zap className="w-3 h-3 inline mr-1" aria-hidden="true" />
                Firing {firing.persona} script…
              </span>
              <span>{Math.round(progress)}%</span>
            </div>
            <Progress
              value={progress}
              className="h-2 bg-slate-800"
              aria-label={`${firing.persona} script progress: ${Math.round(progress)}%`}
            />
          </motion.div>
        )}
      </AnimatePresence>

      {/* Status message */}
      <AnimatePresence>
        {statusMessage && (
          <motion.p
            initial={{ opacity: 0, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            className="text-sm text-slate-400 text-center"
            role="status"
            aria-live="polite"
          >
            {statusMessage}
          </motion.p>
        )}
      </AnimatePresence>

      {/* Secondary actions */}
      <div className="flex gap-3 pt-2 border-t border-slate-800">
        <Button
          variant="outline"
          className="border-slate-700 text-slate-300 hover:text-slate-100 flex-1"
          onClick={handleReplay}
          aria-label="Replay last trigger"
        >
          <RefreshCw className="w-4 h-4 mr-2" aria-hidden="true" />
          Replay last trigger
        </Button>
        <Button
          variant="outline"
          className="border-red-900/60 text-red-400 hover:text-red-300 hover:border-red-800"
          onClick={() => setResetDialogOpen(true)}
          aria-label="Reset demo"
        >
          <RotateCcw className="w-4 h-4 mr-2" aria-hidden="true" />
          Reset
        </Button>
      </div>

      <ResetDialog
        open={resetDialogOpen}
        onConfirm={handleReset}
        onCancel={() => setResetDialogOpen(false)}
      />
    </div>
  );
}

// ─── Reset confirmation dialog ────────────────────────────────────────────────

function ResetDialog({
  open,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onCancel()}>
      <DialogContent className="bg-slate-900 border-slate-700 max-w-md">
        <DialogHeader>
          <DialogTitle className="text-slate-100">Reset demo?</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-slate-400 leading-relaxed">
          This will wipe events, triggers, mock emails, and cooldowns from the
          database. Continue?
        </p>
        <div className="flex justify-end gap-3 pt-2">
          <Button
            variant="outline"
            className="border-slate-700 text-slate-300"
            onClick={onCancel}
          >
            Cancel
          </Button>
          <Button
            className="bg-red-700 hover:bg-red-600 text-white"
            onClick={onConfirm}
          >
            Reset
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
