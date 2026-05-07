"use client";

import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { useCases } from "@/lib/use-cases";
import type { UseCase, UseCaseIcon } from "@/lib/use-cases";

/** Maps the icon field to an emoji for display. */
const ICON_MAP: Record<UseCaseIcon, string> = {
  rescue: "🚨",
  alert: "📞",
  wrench: "🔧",
  inbox: "✉️",
};

interface UseCaseGalleryProps {
  onSelect: (useCase: UseCase) => void;
  onSkip: () => void;
}

/**
 * Wizard step 0: 2×2 grid of outcome-oriented use-case cards.
 * Clicking a card pre-fills persona + rule_template and advances to QA step.
 * "Or pick by persona →" falls through to the legacy PersonaPicker step.
 */
export function UseCaseGallery({ onSelect, onSkip }: UseCaseGalleryProps) {
  return (
    <div className="flex flex-col gap-6">
      <div>
        <h2 className="text-xl font-semibold text-slate-100 mb-1">
          What do you want to achieve?
        </h2>
        <p className="text-sm text-slate-400">
          Pick an outcome and we will pre-configure the right rules and persona for you.
        </p>
      </div>

      <div
        className="grid grid-cols-1 md:grid-cols-2 gap-4"
        role="list"
        aria-label="Use-case selection"
      >
        {useCases.map((uc) => {
          const accentClass =
            uc.persona === "realestate"
              ? "border-l-4 border-blue-500"
              : "border-l-4 border-emerald-500";

          return (
            <Card
              key={uc.id}
              role="listitem"
              className={cn(
                "cursor-pointer bg-slate-900 border-slate-800 hover:shadow-md transition-shadow",
                accentClass
              )}
              onClick={() => onSelect(uc)}
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  onSelect(uc);
                }
              }}
              aria-label={`${uc.title}: ${uc.subtitle}`}
            >
              <CardContent className="p-5 flex flex-col gap-3">
                {/* Icon + title row */}
                <div className="flex items-start gap-3">
                  <span className="text-2xl leading-none" aria-hidden="true">
                    {ICON_MAP[uc.icon]}
                  </span>
                  <p className="font-semibold text-lg text-slate-100 leading-snug">
                    {uc.title}
                  </p>
                </div>

                {/* Subtitle */}
                <p className="text-sm text-muted-foreground leading-relaxed">
                  {uc.subtitle}
                </p>

                {/* Preview action + outcome metric */}
                <div className="flex flex-col gap-1 pt-1">
                  <p className="text-xs uppercase tracking-wide opacity-60 text-slate-300">
                    {uc.preview_action}
                  </p>
                  <p className="text-xs font-medium text-slate-200">
                    {uc.outcome_metric}
                  </p>
                </div>
              </CardContent>
            </Card>
          );
        })}
      </div>

      {/* Fallback link to the original PersonaPicker */}
      <div className="flex justify-center">
        <button
          type="button"
          onClick={onSkip}
          className="text-xs text-slate-500 hover:text-slate-300 transition-colors underline underline-offset-2"
        >
          Or pick by persona →
        </button>
      </div>
    </div>
  );
}
