"use client";

import { useForm, Controller } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Loader2, X } from "lucide-react";
import { cn } from "@/lib/utils";
import type { Persona, WizardAnswers } from "@/types/api";
import { PERSONA_QUESTIONS, getDefaultAnswers } from "@/lib/personas";

// Build a Zod schema dynamically from question definitions
function buildSchema(persona: Persona) {
  const questions = PERSONA_QUESTIONS[persona];
  const shape: Record<string, z.ZodTypeAny> = {};
  for (const q of questions) {
    switch (q.type) {
      case "number":
        shape[q.id] = z.coerce
          .number({ error: "Must be a number" })
          .min(1, "Must be at least 1");
        break;
      case "multiselect":
        shape[q.id] = z
          .array(z.string())
          .min(1, "Select at least one option");
        break;
      default:
        shape[q.id] = z.string().min(1, "This field is required");
    }
  }
  return z.object(shape);
}

interface QAStepProps {
  persona: Persona;
  onSubmit: (answers: WizardAnswers) => void;
  isSubmitting: boolean;
}

/**
 * Step 2: pre-filled Q&A form. Each question is rendered based on its type.
 * Uses react-hook-form + Zod for validation. The "Generate" button triggers
 * POST /api/onboarding/generate-config.
 */
export function QAStep({ persona, onSubmit, isSubmitting }: QAStepProps) {
  const questions = PERSONA_QUESTIONS[persona];
  const schema = buildSchema(persona);
  type FormValues = z.infer<typeof schema>;

  const defaults = getDefaultAnswers(persona) as FormValues;

  const {
    register,
    handleSubmit,
    control,
    formState: { errors },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: defaults,
  });

  const handleFormSubmit = (data: FormValues) => {
    onSubmit(data as WizardAnswers);
  };

  return (
    <form
      onSubmit={handleSubmit(handleFormSubmit)}
      noValidate
      className="flex flex-col gap-6"
      aria-label="Configuration questions"
    >
      <div>
        <h2 className="text-xl font-semibold text-slate-100 mb-1">
          Configure your rules
        </h2>
        <p className="text-sm text-slate-400">
          Pre-filled with recommended defaults. Edit to match your setup.
        </p>
      </div>

      <div className="flex flex-col gap-5">
        {questions.map((q) => {
          const fieldError = errors[q.id as keyof typeof errors];
          const errorMsg = fieldError?.message as string | undefined;

          return (
            <div key={q.id} className="flex flex-col gap-1.5">
              <Label
                htmlFor={q.id}
                className="text-slate-200 text-sm font-medium"
              >
                {q.label}
                {q.type !== "multiselect" && (
                  <span className="sr-only"> (required)</span>
                )}
              </Label>

              {q.helpText && (
                <p id={`${q.id}-help`} className="text-xs text-slate-500">
                  {q.helpText}
                </p>
              )}

              {/* textarea */}
              {q.type === "textarea" && (
                <textarea
                  id={q.id}
                  rows={3}
                  placeholder={q.placeholder}
                  aria-describedby={
                    [q.helpText ? `${q.id}-help` : null, errorMsg ? `${q.id}-error` : null]
                      .filter(Boolean)
                      .join(" ") || undefined
                  }
                  aria-invalid={!!errorMsg}
                  className={cn(
                    "flex w-full rounded-md border bg-slate-900 px-3 py-2 text-sm text-slate-100",
                    "placeholder:text-slate-500 focus-visible:outline-none focus-visible:ring-2",
                    "focus-visible:ring-violet-500 focus-visible:ring-offset-2 focus-visible:ring-offset-slate-950",
                    "disabled:cursor-not-allowed disabled:opacity-50",
                    "font-mono resize-none",
                    errorMsg ? "border-red-600" : "border-slate-700"
                  )}
                  {...register(q.id as keyof FormValues)}
                />
              )}

              {/* number input */}
              {q.type === "number" && (
                <Input
                  id={q.id}
                  type="number"
                  min={1}
                  aria-describedby={
                    [q.helpText ? `${q.id}-help` : null, errorMsg ? `${q.id}-error` : null]
                      .filter(Boolean)
                      .join(" ") || undefined
                  }
                  aria-invalid={!!errorMsg}
                  className={cn(
                    "bg-slate-900 border-slate-700 text-slate-100",
                    "focus-visible:ring-violet-500",
                    errorMsg && "border-red-600"
                  )}
                  {...register(q.id as keyof FormValues)}
                />
              )}

              {/* text input */}
              {q.type === "text" && (
                <Input
                  id={q.id}
                  type="text"
                  placeholder={q.placeholder}
                  aria-describedby={
                    [q.helpText ? `${q.id}-help` : null, errorMsg ? `${q.id}-error` : null]
                      .filter(Boolean)
                      .join(" ") || undefined
                  }
                  aria-invalid={!!errorMsg}
                  className={cn(
                    "bg-slate-900 border-slate-700 text-slate-100",
                    "focus-visible:ring-violet-500",
                    errorMsg && "border-red-600"
                  )}
                  {...register(q.id as keyof FormValues)}
                />
              )}

              {/* select */}
              {q.type === "select" && (
                <Controller
                  name={q.id as keyof FormValues}
                  control={control}
                  render={({ field }) => (
                    <Select
                      onValueChange={field.onChange}
                      defaultValue={field.value as string}
                    >
                      <SelectTrigger
                        id={q.id}
                        aria-describedby={
                          [q.helpText ? `${q.id}-help` : null, errorMsg ? `${q.id}-error` : null]
                            .filter(Boolean)
                            .join(" ") || undefined
                        }
                        aria-invalid={!!errorMsg}
                        className={cn(
                          "bg-slate-900 border-slate-700 text-slate-100",
                          "focus:ring-violet-500",
                          errorMsg && "border-red-600"
                        )}
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent className="bg-slate-900 border-slate-700">
                        {q.options?.map((opt) => (
                          <SelectItem
                            key={opt}
                            value={opt}
                            className="text-slate-100 focus:bg-slate-800"
                          >
                            {opt}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                />
              )}

              {/* multiselect — rendered as toggleable badges */}
              {q.type === "multiselect" && (
                <Controller
                  name={q.id as keyof FormValues}
                  control={control}
                  render={({ field }) => {
                    const selected = (field.value as string[]) ?? [];
                    const toggle = (opt: string) => {
                      const next = selected.includes(opt)
                        ? selected.filter((s) => s !== opt)
                        : [...selected, opt];
                      field.onChange(next);
                    };
                    return (
                      <div
                        id={q.id}
                        role="group"
                        aria-label={q.label}
                        aria-describedby={
                          [q.helpText ? `${q.id}-help` : null, errorMsg ? `${q.id}-error` : null]
                            .filter(Boolean)
                            .join(" ") || undefined
                        }
                        className="flex flex-wrap gap-2"
                      >
                        {q.options?.map((opt) => {
                          const active = selected.includes(opt);
                          return (
                            <button
                              key={opt}
                              type="button"
                              aria-pressed={active}
                              onClick={() => toggle(opt)}
                              className={cn(
                                "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium",
                                "transition-all duration-150 focus-visible:outline-none focus-visible:ring-2",
                                "focus-visible:ring-violet-500 focus-visible:ring-offset-2 focus-visible:ring-offset-slate-950",
                                active
                                  ? "border-violet-500 bg-violet-500/20 text-violet-300"
                                  : "border-slate-700 bg-transparent text-slate-400 hover:border-slate-500"
                              )}
                            >
                              {opt}
                              {active && (
                                <X
                                  className="w-3 h-3"
                                  aria-hidden="true"
                                />
                              )}
                            </button>
                          );
                        })}
                      </div>
                    );
                  }}
                />
              )}

              {errorMsg && (
                <p
                  id={`${q.id}-error`}
                  role="alert"
                  className="text-xs text-red-400"
                >
                  {errorMsg}
                </p>
              )}
            </div>
          );
        })}
      </div>

      <Button
        type="submit"
        disabled={isSubmitting}
        className="self-start bg-violet-600 hover:bg-violet-700 text-white focus-visible:ring-violet-500"
        aria-busy={isSubmitting}
      >
        {isSubmitting && (
          <Loader2 className="w-4 h-4 mr-2 animate-spin" aria-hidden="true" />
        )}
        {isSubmitting ? "Generating..." : "Generate config"}
      </Button>
    </form>
  );
}
