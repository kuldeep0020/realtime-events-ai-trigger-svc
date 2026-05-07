/**
 * Canned wizard answers per persona.
 * These pre-fill the Q&A step so the demo works out of the box.
 * All values are editable by the user before generating config.
 */

import type { Persona, WizardAnswers } from "@/types/api";

export interface WizardQuestion {
  id: string;
  label: string;
  type: "text" | "textarea" | "number" | "multiselect" | "select";
  placeholder?: string;
  options?: string[]; // for select / multiselect
  defaultValue: string | string[] | number;
  helpText?: string;
}

export const PERSONA_QUESTIONS: Record<Persona, WizardQuestion[]> = {
  realestate: [
    {
      id: "realtors",
      label: "Who are your realtors per suburb?",
      type: "textarea",
      placeholder:
        "Priya N. → suburb-1, suburb-2\nArjun M. → suburb-3\nMira K. → countryside-1, countryside-2",
      defaultValue:
        "Priya N. → suburb-1, suburb-2\nArjun M. → suburb-3\nMira K. → countryside-1, countryside-2",
      helpText:
        "One realtor per line, format: Name → suburb1, suburb2. These match the suburb slugs in your tracking plan.",
    },
    {
      id: "price_range",
      label: "What price range and bedroom count are typical hot leads?",
      type: "text",
      placeholder: "$1M-$1.8M, 3+ BR",
      defaultValue: "$1M-$1.8M, 3+ BR",
      helpText:
        "Used to calibrate the high-intent filter rule (e.g. listings viewed above minimum price).",
    },
    {
      id: "idle_seconds",
      label:
        "After how many seconds of idle should we treat the session as abandoned?",
      type: "number",
      defaultValue: 30,
      helpText:
        "The window manager fires the realtor ping rule after this many idle seconds.",
    },
  ],
  "rs-self": [
    {
      id: "error_events",
      label: "Which error events should trigger help?",
      type: "multiselect",
      options: [
        "Source Setup Error",
        "Destination Setup Error",
        "Webhook Send Error",
      ],
      defaultValue: [
        "Source Setup Error",
        "Destination Setup Error",
        "Webhook Send Error",
      ],
      helpText:
        "Select all error event names that should fire the onboarding_errored rule.",
    },
    {
      id: "idle_seconds",
      label: "After how many seconds idle in setup do we nudge?",
      type: "number",
      defaultValue: 15,
      helpText:
        "Triggers the onboarding_stuck rule when no progress events fire within this window.",
    },
    {
      id: "help_channel",
      label: "Help channel?",
      type: "select",
      options: ["Email", "In-app banner"],
      defaultValue: "Email",
      helpText:
        "Where to deliver the onboarding nudge. Email uses the mock email viewer; in-app banner is a future extension.",
    },
  ],
};

export function getDefaultAnswers(persona: Persona): WizardAnswers {
  const questions = PERSONA_QUESTIONS[persona];
  return Object.fromEntries(
    questions.map((q) => [q.id, q.defaultValue])
  ) as WizardAnswers;
}
