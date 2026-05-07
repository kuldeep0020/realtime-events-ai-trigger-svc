"use client";

import { useState, useTransition } from "react";
import { Building2, BarChart3, CheckCircle2 } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { getTrackingPlan } from "@/lib/api-client";
import type { Persona, TrackingPlanSpec } from "@/types/api";

interface PersonaCardConfig {
  id: Persona;
  label: string;
  subtitle: string;
  description: string;
  icon: React.ComponentType<{ className?: string }>;
  accentClass: string;
}

const PERSONAS: PersonaCardConfig[] = [
  {
    id: "realestate",
    label: "Real-estate Portal",
    subtitle: "Realtor session abandonment",
    description:
      "Fires Slack pings to assigned realtors when high-intent visitors abandon a listing session.",
    icon: Building2,
    accentClass: "border-emerald-600/50 hover:border-emerald-500",
  },
  {
    id: "rs-self",
    label: "RudderStack Onboarding",
    subtitle: "Self-serve stuck detection",
    description:
      "Fires proactive support emails when users encounter errors during source/destination setup.",
    icon: BarChart3,
    accentClass: "border-violet-600/50 hover:border-violet-500",
  },
];

interface PersonaPickerProps {
  onPersonaSelected: (persona: Persona, spec: TrackingPlanSpec) => void;
}

/**
 * Step 1: two persona cards side-by-side. Clicking a card fetches the
 * tracking plan and surfaces it in the side panel. The "Next" button
 * is enabled only after a persona is selected.
 */
export function PersonaPicker({ onPersonaSelected }: PersonaPickerProps) {
  const [selected, setSelected] = useState<Persona | null>(null);
  const [loading, setLoading] = useState<Persona | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [, startTransition] = useTransition();

  const handleSelect = (persona: Persona) => {
    if (loading) return; // prevent double-click
    setLoading(persona);
    setError(null);

    startTransition(async () => {
      try {
        const plan = await getTrackingPlan(persona);
        setSelected(persona);
        onPersonaSelected(persona, plan.spec);
      } catch (err) {
        const msg =
          err instanceof Error ? err.message : "Failed to load tracking plan";
        setError(msg);
      } finally {
        setLoading(null);
      }
    });
  };

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h2 className="text-xl font-semibold text-slate-100 mb-1">
          Choose your demo persona
        </h2>
        <p className="text-sm text-slate-400">
          Each persona has a pre-configured tracking plan and trigger rules.
        </p>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-800 bg-red-950/50 px-4 py-3 text-sm text-red-300"
        >
          {error}
        </div>
      )}

      <div
        className="grid grid-cols-1 sm:grid-cols-2 gap-4"
        role="radiogroup"
        aria-label="Persona selection"
      >
        {PERSONAS.map((p) => {
          const Icon = p.icon;
          const isSelected = selected === p.id;
          const isLoading = loading === p.id;

          return (
            <Card
              key={p.id}
              className={cn(
                "cursor-pointer border-2 bg-slate-900 transition-all duration-200",
                "focus-within:ring-2 focus-within:ring-violet-500 focus-within:ring-offset-2 focus-within:ring-offset-slate-950",
                isSelected
                  ? "border-violet-500 bg-slate-900/80"
                  : p.accentClass + " border-slate-800 bg-slate-900",
                isLoading && "opacity-75"
              )}
              role="radio"
              aria-checked={isSelected}
              onClick={() => handleSelect(p.id)}
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  handleSelect(p.id);
                }
              }}
              aria-label={`${p.label}: ${p.description}`}
            >
              <CardContent className="p-5 flex flex-col gap-3">
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-3">
                    <div
                      className={cn(
                        "w-10 h-10 rounded-lg flex items-center justify-center",
                        isSelected ? "bg-violet-600/20" : "bg-slate-800"
                      )}
                      aria-hidden="true"
                    >
                      <Icon
                        className={cn(
                          "w-5 h-5",
                          isSelected ? "text-violet-400" : "text-slate-400"
                        )}
                      />
                    </div>
                    <div>
                      <p className="text-slate-100 font-semibold text-sm">
                        {p.label}
                      </p>
                      <p className="text-xs text-slate-400">{p.subtitle}</p>
                    </div>
                  </div>
                  {isSelected && (
                    <CheckCircle2
                      className="w-5 h-5 text-violet-400 shrink-0"
                      aria-hidden="true"
                    />
                  )}
                  {isLoading && (
                    <div
                      className="w-5 h-5 border-2 border-violet-400 border-t-transparent rounded-full animate-spin"
                      aria-label="Loading..."
                      role="status"
                    />
                  )}
                </div>
                <p className="text-xs text-slate-400 leading-relaxed">
                  {p.description}
                </p>
              </CardContent>
            </Card>
          );
        })}
      </div>

      {selected && (
        <p className="text-xs text-slate-500">
          Tracking plan loaded. Click{" "}
          <span className="text-slate-300">Next</span> to configure your rules.
        </p>
      )}
    </div>
  );
}
