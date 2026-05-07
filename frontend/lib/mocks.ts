/**
 * Mock-mode helpers. When NEXT_PUBLIC_API_BASE is unset (empty string),
 * the api-client calls these instead of making real HTTP requests.
 *
 * JSON files under /mocks/ are imported statically so they bundle at build
 * time — no runtime filesystem access needed in the browser.
 */

import type {
  TrackingPlanResponse,
  GenerateConfigResponse,
  ActivateConfigResponse,
  MockEmailsResponse,
  ReplayLastTriggerResponse,
  DemoResetResponse,
  FireScriptResponse,
  Persona,
  WizardAnswers,
} from "@/types/api";

import trackingPlanRealestate from "@/mocks/tracking-plan-realestate.json";
import trackingPlanRsSelf from "@/mocks/tracking-plan-rs-self.json";
import generatedConfigRealestate from "@/mocks/generated-config-realestate.yaml.json";
import generatedConfigRsSelf from "@/mocks/generated-config-rs-self.yaml.json";
import triggerRealestate from "@/mocks/trigger-realestate.json";
import triggerRsSelf from "@/mocks/trigger-rs-self.json";

/** Simulate network latency for a realistic mock experience. */
function delay(ms = 400): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export async function mockGetTrackingPlan(
  persona: string
): Promise<TrackingPlanResponse> {
  await delay();
  if (persona === "realestate") {
    return trackingPlanRealestate as TrackingPlanResponse;
  }
  if (persona === "rs-self") {
    return trackingPlanRsSelf as TrackingPlanResponse;
  }
  throw new Error(`Mock: no tracking plan for persona "${persona}"`);
}

export async function mockGenerateConfig(
  persona: Persona,
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  _answers: WizardAnswers
): Promise<GenerateConfigResponse> {
  await delay(800);
  if (persona === "realestate") {
    return generatedConfigRealestate as GenerateConfigResponse;
  }
  if (persona === "rs-self") {
    return generatedConfigRsSelf as GenerateConfigResponse;
  }
  throw new Error(`Mock: no config for persona "${persona}"`);
}

export async function mockActivateConfig(
  _req: { id?: string; persona?: string; config_yaml?: string }
): Promise<ActivateConfigResponse> {
  await delay(300);
  return {
    id: "mock-config-id-00000000",
    active: true,
    persona: _req.persona ?? "realestate",
  };
}

export async function mockListMockEmails(): Promise<MockEmailsResponse> {
  await delay(200);
  return { emails: [] };
}

export async function mockReplayLastTrigger(
  persona: Persona
): Promise<ReplayLastTriggerResponse> {
  await delay(200);
  const data =
    persona === "rs-self"
      ? triggerRsSelf
      : triggerRealestate;
  return data as ReplayLastTriggerResponse;
}

export async function mockFireScript(
  persona: Persona
): Promise<FireScriptResponse> {
  await delay(300);
  return {
    persona,
    event_count: persona === "realestate" ? 7 : 4,
    status: "fired",
  };
}

export async function mockDemoReset(): Promise<DemoResetResponse> {
  await delay(200);
  return { status: "reset" };
}
