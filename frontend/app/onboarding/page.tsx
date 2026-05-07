"use client";

import { useState, useRef } from "react";
import { Stepper } from "@/components/wizard/Stepper";
import { PersonaPicker } from "@/components/wizard/PersonaPicker";
import { QAStep } from "@/components/wizard/QAStep";
import { ConfigPreview } from "@/components/wizard/ConfigPreview";
import { TrackingPlanPanel } from "@/components/wizard/TrackingPlanPanel";
import { BrandHeader } from "@/components/shared/BrandHeader";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { ChevronRight } from "lucide-react";
import { generateConfig } from "@/lib/api-client";
import type { Persona, TrackingPlanSpec, GenerateConfigResponse, WizardAnswers } from "@/types/api";

const STEPS = [
  { id: 1, label: "Choose persona" },
  { id: 2, label: "Configure rules" },
  { id: 3, label: "Preview & activate" },
];

/**
 * Three-step onboarding wizard.
 * Step 1 — PersonaPicker: choose real-estate or rs-self
 * Step 2 — QAStep: edit canned answers, then click Generate
 * Step 3 — ConfigPreview: review YAML, then Activate & continue → /dashboard
 *
 * A double-click guard on Next prevents race conditions: nextEnabled tracks
 * whether the button is allowed, and advancing is guarded by the current step.
 */
export default function OnboardingPage() {
  const [step, setStep] = useState(0);
  const [persona, setPersona] = useState<Persona | null>(null);
  const [trackingPlanSpec, setTrackingPlanSpec] = useState<TrackingPlanSpec | null>(null);
  const [configData, setConfigData] = useState<GenerateConfigResponse | null>(null);
  const [generating, setGenerating] = useState(false);
  const [generateError, setGenerateError] = useState<string | null>(null);

  // Guard against double-clicking Next
  const advancingRef = useRef(false);

  const canAdvanceFromStep0 = persona !== null && trackingPlanSpec !== null;

  const handlePersonaSelected = (p: Persona, spec: TrackingPlanSpec) => {
    setPersona(p);
    setTrackingPlanSpec(spec);
  };

  const handleNext = () => {
    if (advancingRef.current) return;
    if (step === 0 && !canAdvanceFromStep0) return;
    advancingRef.current = true;
    setStep((s) => s + 1);
    // Allow advancing again after a short tick
    setTimeout(() => { advancingRef.current = false; }, 200);
  };

  const handleQASubmit = async (answers: WizardAnswers) => {
    if (!persona || generating) return;
    setGenerating(true);
    setGenerateError(null);
    try {
      const result = await generateConfig({ persona, answers });
      setConfigData(result);
      setStep(2);
    } catch (err) {
      setGenerateError(err instanceof Error ? err.message : "Failed to generate config");
    } finally {
      setGenerating(false);
    }
  };

  const handleBackToQA = () => {
    setStep(1);
    setConfigData(null);
  };

  return (
    <div className="flex flex-col min-h-screen bg-slate-950">
      <BrandHeader />

      <main className="flex-1 flex flex-col p-6 md:p-8 max-w-screen-xl mx-auto w-full">
        {/* Stepper */}
        <div className="mb-8">
          <Stepper steps={STEPS} current={step} />
        </div>

        {/* Main content: left = wizard step, right = tracking plan panel */}
        <div className="flex flex-col md:flex-row gap-6 flex-1">
          {/* Left pane */}
          <section
            className="flex-1 flex flex-col"
            aria-live="polite"
            aria-label={`Step ${step + 1}: ${STEPS[step].label}`}
          >
            {step === 0 && (
              <>
                <PersonaPicker onPersonaSelected={handlePersonaSelected} />
                <Separator className="bg-slate-800 my-6" />
                <div className="flex justify-end">
                  <Button
                    onClick={handleNext}
                    disabled={!canAdvanceFromStep0}
                    className="bg-violet-600 hover:bg-violet-700 text-white disabled:opacity-40 focus-visible:ring-violet-500"
                    aria-disabled={!canAdvanceFromStep0}
                  >
                    Next
                    <ChevronRight className="w-4 h-4 ml-1" aria-hidden="true" />
                  </Button>
                </div>
              </>
            )}

            {step === 1 && persona && (
              <>
                {generateError && (
                  <div role="alert" className="mb-4 rounded-md border border-red-800 bg-red-950/50 px-4 py-3 text-sm text-red-300">
                    {generateError}
                  </div>
                )}
                <QAStep
                  persona={persona}
                  onSubmit={handleQASubmit}
                  isSubmitting={generating}
                />
              </>
            )}

            {step === 2 && configData && persona && (
              <ConfigPreview
                data={configData}
                persona={persona}
                onBack={handleBackToQA}
              />
            )}
          </section>

          {/* Right pane — tracking plan */}
          {step < 2 && (
            <aside
              className="w-full md:w-80 lg:w-96 rounded-lg border border-slate-800 bg-slate-900/50 flex flex-col"
              aria-label="Tracking plan reference"
            >
              <div className="px-4 pt-4 pb-2">
                <p className="text-xs uppercase tracking-wider text-slate-500 font-medium">
                  Tracking Plan
                </p>
              </div>
              <Separator className="bg-slate-800" />
              <div className="flex-1 overflow-y-auto">
                <TrackingPlanPanel spec={trackingPlanSpec} />
              </div>
            </aside>
          )}
        </div>
      </main>
    </div>
  );
}
