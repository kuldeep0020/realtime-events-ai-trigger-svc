"use client";

import { useState, useRef } from "react";
import { Stepper } from "@/components/wizard/Stepper";
import { UseCaseGallery } from "@/components/wizard/UseCaseGallery";
import { PersonaPicker } from "@/components/wizard/PersonaPicker";
import { QAStep } from "@/components/wizard/QAStep";
import { ConfigPreview } from "@/components/wizard/ConfigPreview";
import { TrackingPlanPanel } from "@/components/wizard/TrackingPlanPanel";
import { BrandHeader } from "@/components/shared/BrandHeader";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { ChevronRight } from "lucide-react";
import { generateConfig, getTrackingPlan } from "@/lib/api-client";
import type { Persona, TrackingPlanSpec, GenerateConfigResponse, WizardAnswers } from "@/types/api";
import type { UseCase } from "@/lib/use-cases";

/**
 * Internal step constants — decoupled from the stepper's display index.
 *
 * STEP_GALLERY (0)   — UseCaseGallery: pick an outcome card
 * STEP_PERSONA (1)   — PersonaPicker: fallback if user clicks "Or pick by persona →"
 * STEP_QA     (2)    — QAStep: configure rules (pre-filled when arriving from gallery)
 * STEP_PREVIEW (3)   — ConfigPreview: review YAML + activate
 */
const STEP_GALLERY = 0;
const STEP_PERSONA = 1;
const STEP_QA = 2;
const STEP_PREVIEW = 3;

/**
 * Visible stepper labels. The gallery and persona-picker share the first step
 * ("Choose"), so the stepper always shows 3 positions regardless of path.
 */
const STEPS = [
  { id: 1, label: "Choose" },
  { id: 2, label: "Configure rules" },
  { id: 3, label: "Preview & activate" },
];

/** Map internal step → stepper display index (0-based). */
function stepperIndex(step: number): number {
  if (step <= STEP_PERSONA) return 0;
  if (step === STEP_QA) return 1;
  return 2; // STEP_PREVIEW
}

/**
 * Four-step onboarding wizard.
 * Step 0 — UseCaseGallery: pick a use-case outcome (new first step)
 * Step 1 — PersonaPicker: fallback path from gallery's "Or pick by persona →"
 * Step 2 — QAStep: edit canned answers, then click Generate
 * Step 3 — ConfigPreview: review YAML, then Activate & continue → /dashboard
 *
 * A double-click guard on Next prevents race conditions.
 */
export default function OnboardingPage() {
  const [step, setStep] = useState(STEP_GALLERY);
  const [persona, setPersona] = useState<Persona | null>(null);
  const [trackingPlanSpec, setTrackingPlanSpec] = useState<TrackingPlanSpec | null>(null);
  const [configData, setConfigData] = useState<GenerateConfigResponse | null>(null);
  const [generating, setGenerating] = useState(false);
  const [generateError, setGenerateError] = useState<string | null>(null);

  // Guard against double-clicking Next
  const advancingRef = useRef(false);

  // ── Gallery handlers ──────────────────────────────────────────────────────

  /**
   * User clicked a use-case card. Pre-fill persona from the card, fetch the
   * tracking plan spec for the right-hand reference panel (so the audience
   * sees the events the rule listens for), and jump directly to QAStep.
   *
   * Tracking plan fetch is fire-and-forget — failure leaves the panel in its
   * empty state ("Select a persona...") rather than blocking the wizard.
   */
  const handleUseCaseSelect = (useCase: UseCase) => {
    setPersona(useCase.persona);
    setTrackingPlanSpec(null); // clear stale spec while fetching
    setStep(STEP_QA);
    void getTrackingPlan(useCase.persona)
      .then((plan) => setTrackingPlanSpec(plan.spec))
      .catch((err) => {
        // Non-fatal: panel stays empty but wizard continues.
        console.warn("[onboarding] tracking plan fetch failed:", err);
      });
  };

  /** User clicked "Or pick by persona →" — fall through to PersonaPicker. */
  const handleGallerySkip = () => {
    setStep(STEP_PERSONA);
  };

  // ── PersonaPicker handlers ────────────────────────────────────────────────

  const canAdvanceFromPersona = persona !== null && trackingPlanSpec !== null;

  const handlePersonaSelected = (p: Persona, spec: TrackingPlanSpec) => {
    setPersona(p);
    setTrackingPlanSpec(spec);
  };

  const handleNext = () => {
    if (advancingRef.current) return;
    if (step === STEP_PERSONA && !canAdvanceFromPersona) return;
    advancingRef.current = true;
    setStep(STEP_QA);
    setTimeout(() => { advancingRef.current = false; }, 200);
  };

  // ── QAStep handlers ───────────────────────────────────────────────────────

  const handleQASubmit = async (answers: WizardAnswers) => {
    if (!persona || generating) return;
    setGenerating(true);
    setGenerateError(null);
    try {
      const result = await generateConfig({ persona, answers });
      setConfigData(result);
      setStep(STEP_PREVIEW);
    } catch (err) {
      setGenerateError(err instanceof Error ? err.message : "Failed to generate config");
    } finally {
      setGenerating(false);
    }
  };

  const handleBackToQA = () => {
    setStep(STEP_QA);
    setConfigData(null);
  };

  // ── Render ────────────────────────────────────────────────────────────────

  const currentStepperIndex = stepperIndex(step);
  const stepLabel = STEPS[currentStepperIndex]?.label ?? "";

  return (
    <div className="flex flex-col min-h-screen bg-slate-950">
      <BrandHeader />

      <main className="flex-1 flex flex-col p-6 md:p-8 max-w-screen-xl mx-auto w-full">
        {/* Stepper */}
        <div className="mb-8">
          <Stepper steps={STEPS} current={currentStepperIndex} />
        </div>

        {/* Main content: left = wizard step, right = tracking plan panel */}
        <div className="flex flex-col md:flex-row gap-6 flex-1">
          {/* Left pane */}
          <section
            className="flex-1 flex flex-col"
            aria-live="polite"
            aria-label={`Step ${currentStepperIndex + 1}: ${stepLabel}`}
          >
            {/* Step 0 — Use-case gallery (new entry point) */}
            {step === STEP_GALLERY && (
              <UseCaseGallery
                onSelect={handleUseCaseSelect}
                onSkip={handleGallerySkip}
              />
            )}

            {/* Step 1 — PersonaPicker (fallback path) */}
            {step === STEP_PERSONA && (
              <>
                <PersonaPicker onPersonaSelected={handlePersonaSelected} />
                <Separator className="bg-slate-800 my-6" />
                <div className="flex justify-end">
                  <Button
                    onClick={handleNext}
                    disabled={!canAdvanceFromPersona}
                    className="bg-violet-600 hover:bg-violet-700 text-white disabled:opacity-40 focus-visible:ring-violet-500"
                    aria-disabled={!canAdvanceFromPersona}
                  >
                    Next
                    <ChevronRight className="w-4 h-4 ml-1" aria-hidden="true" />
                  </Button>
                </div>
              </>
            )}

            {/* Step 2 — QAStep */}
            {step === STEP_QA && persona && (
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

            {/* Step 3 — ConfigPreview */}
            {step === STEP_PREVIEW && configData && persona && (
              <ConfigPreview
                data={configData}
                persona={persona}
                onBack={handleBackToQA}
              />
            )}
          </section>

          {/* Right pane — tracking plan (only visible before preview step) */}
          {step < STEP_PREVIEW && (
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
