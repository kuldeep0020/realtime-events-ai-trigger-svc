"use client";

import { cn } from "@/lib/utils";
import { Check } from "lucide-react";

export interface StepperStep {
  id: number;
  label: string;
}

interface StepperProps {
  steps: StepperStep[];
  current: number;
  className?: string;
}

/**
 * Linear step indicator. Steps are numbered 1-based; `current` is 0-based index.
 * Completed steps show a check icon; current step is highlighted in brand purple.
 */
export function Stepper({ steps, current, className }: StepperProps) {
  return (
    <nav
      aria-label="Onboarding progress"
      className={cn("flex items-center gap-0", className)}
    >
      {steps.map((step, idx) => {
        const isCompleted = idx < current;
        const isCurrent = idx === current;
        const isUpcoming = idx > current;

        return (
          <div key={step.id} className="flex items-center">
            {/* Step circle */}
            <div className="flex items-center gap-2">
              <div
                className={cn(
                  "w-8 h-8 rounded-full flex items-center justify-center text-sm font-semibold",
                  "border-2 transition-all duration-200",
                  isCompleted && "border-violet-500 bg-violet-500 text-white",
                  isCurrent &&
                    "border-violet-400 bg-transparent text-violet-400",
                  isUpcoming &&
                    "border-slate-600 bg-transparent text-slate-500"
                )}
                aria-current={isCurrent ? "step" : undefined}
              >
                {isCompleted ? (
                  <Check className="w-4 h-4" aria-hidden="true" />
                ) : (
                  <span>{step.id}</span>
                )}
              </div>
              <span
                className={cn(
                  "text-sm font-medium hidden sm:block",
                  isCurrent && "text-slate-100",
                  isCompleted && "text-violet-400",
                  isUpcoming && "text-slate-500"
                )}
              >
                {step.label}
              </span>
            </div>

            {/* Connector line between steps */}
            {idx < steps.length - 1 && (
              <div
                className={cn(
                  "w-12 h-px mx-3",
                  idx < current ? "bg-violet-500" : "bg-slate-700"
                )}
                aria-hidden="true"
              />
            )}
          </div>
        );
      })}
    </nav>
  );
}
