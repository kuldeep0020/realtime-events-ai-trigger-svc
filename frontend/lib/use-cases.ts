/**
 * Use-case definitions for the wizard gallery (§5.7 of the demo-maturity spec).
 * These are hardcoded — no server-driven content needed for the demo.
 * The rule_template IDs map to backend persona-config rule names.
 */

import type { Persona } from "@/types/api";

export type UseCaseIcon = "rescue" | "alert" | "wrench" | "inbox";

export type UseCase = {
  id: string;
  title: string;
  subtitle: string;
  persona: Persona;
  rule_template: string;
  preview_action: string;
  outcome_metric: string;
  icon: UseCaseIcon;
};

export const useCases: UseCase[] = [
  {
    id: "rescue_anonymous_high_intent",
    title: "Win back high-intent anonymous visitors",
    subtitle:
      "When someone behaves like a buyer but has not signed up, alert your team to capture them with a personalized in-app prompt before they leave.",
    persona: "realestate",
    rule_template: "realtor_anonymous_high_intent",
    preview_action: "In-app banner + Slack to standby realtor",
    outcome_metric: "Avg. capture rate lift: +38%",
    icon: "rescue",
  },
  {
    id: "alert_known_high_value",
    title: "Alert realtors to known high-value leads",
    subtitle:
      "When a known prospect is actively browsing in their target suburb, page the right realtor with their full context — name, budget, intent, and the listing in their cart.",
    persona: "realestate",
    rule_template: "realtor_known_high_intent",
    preview_action: "Slack with full visitor profile + assigned realtor",
    outcome_metric: "Avg. response time: 6 seconds",
    icon: "alert",
  },
  {
    id: "rescue_destination_error",
    title: "Rescue stuck destination setup",
    subtitle:
      "When a customer hits an integration error during onboarding, send a tailored fix guide with the exact steps for their specific error before they churn out.",
    persona: "rs-self",
    rule_template: "rs_destination_error",
    preview_action: "Personalized email with 3-step fix",
    outcome_metric: "Avg. recovery rate: 71%",
    icon: "wrench",
  },
  {
    id: "reengage_stuck_onboarding",
    title: "Re-engage abandoned onboarding",
    subtitle:
      "When a customer creates a source but never connects a destination, nudge them with the next step within 24 hours — with their progress and tech stack as context.",
    persona: "rs-self",
    rule_template: "rs_onboarding_stuck",
    preview_action: "Personalized email with their setup status",
    outcome_metric: "Avg. completion rate: +52%",
    icon: "inbox",
  },
];
