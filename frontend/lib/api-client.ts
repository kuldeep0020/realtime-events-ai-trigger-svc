/**
 * REST API client for the realtime-events-ai-trigger-svc backend.
 *
 * When NEXT_PUBLIC_API_BASE is set, all calls go to that origin.
 * When it is absent (empty string / undefined), mock-mode is active:
 * functions return data from /mocks/*.json files via lib/mocks.ts.
 *
 * Responses are validated with Zod at the boundary so TypeScript consumers
 * receive typed, validated data — never raw unknown.
 */

import { z } from "zod";
import type {
  TrackingPlanResponse,
  GenerateConfigRequest,
  GenerateConfigResponse,
  ActivateConfigRequest,
  ActivateConfigResponse,
  MockEmailsResponse,
  ReplayLastTriggerResponse,
  DemoResetResponse,
  FireScriptRequest,
  FireScriptResponse,
  SSEEventPayload,
  SSEWindowPayload,
  SSETriggerPayload,
} from "@/types/api";
import {
  mockGetTrackingPlan,
  mockGenerateConfig,
  mockActivateConfig,
  mockListMockEmails,
  mockReplayLastTrigger,
  mockFireScript,
  mockDemoReset,
} from "@/lib/mocks";

const BASE = process.env.NEXT_PUBLIC_API_BASE ?? "";
export const isMockMode = BASE === "";

// ──────────────────────────────────────────────────────────────────────────────
// Zod schemas (validate backend responses at the boundary)
// ──────────────────────────────────────────────────────────────────────────────

const trackingPlanPropertySchema = z.object({
  name: z.string(),
  type: z.enum(["string", "number", "integer", "boolean", "array", "object"]),
  description: z.string(),
  required: z.boolean(),
});

const trackingPlanSpecSchema = z.object({
  persona: z.string(),
  version: z.string(),
  description: z.string(),
  events: z.array(
    z.object({
      name: z.string(),
      description: z.string(),
      properties: z.array(trackingPlanPropertySchema),
    })
  ),
});

const trackingPlanResponseSchema = z.object({
  persona: z.string(),
  spec: trackingPlanSpecSchema,
});

const generateConfigResponseSchema = z.object({
  persona: z.string(),
  source: z.string(),
  config_yaml: z.string(),
  description: z.string(),
});

const activateConfigResponseSchema = z.object({
  id: z.string(),
  active: z.boolean(),
  persona: z.string(),
});

const mockEmailSchema = z.object({
  id: z.string(),
  trigger_id: z.string().optional(),
  to_email: z.string(),
  subject: z.string(),
  body_markdown: z.string(),
  links: z
    .array(z.object({ title: z.string(), url: z.string() }))
    .optional(),
  created_at: z.string(),
});

const mockEmailsResponseSchema = z.object({
  emails: z.array(mockEmailSchema),
});

const triggerResponseSchema = z.object({
  id: z.string(),
  rule_name: z.string(),
  persona: z.string(),
  anonymous_id: z.string(),
  fired_at: z.string(),
  window_snapshot: z.record(z.string(), z.unknown()),
  llm_parsed: z.record(z.string(), z.unknown()).optional(),
  destination: z.string(),
  dispatch_status: z.string(),
});

const demoResetResponseSchema = z.object({
  status: z.string(),
});

const fireScriptResponseSchema = z.object({
  persona: z.string(),
  event_count: z.number(),
  status: z.string(),
  count: z.number().optional(),
  speed: z.number().optional(),
});

// ──────────────────────────────────────────────────────────────────────────────
// Internal fetch helper
// ──────────────────────────────────────────────────────────────────────────────

class APIClientError extends Error {
  constructor(
    message: string,
    public readonly status?: number
  ) {
    super(message);
    this.name = "APIClientError";
  }
}

async function apiFetch<T>(
  path: string,
  schema: z.ZodSchema<T>,
  options?: RequestInit
): Promise<T> {
  const url = `${BASE}${path}`;
  const response = await fetch(url, {
    headers: { "Content-Type": "application/json", ...options?.headers },
    ...options,
  });
  if (!response.ok) {
    let message = `HTTP ${response.status}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body.error) message = body.error;
    } catch {
      // ignore parse failure — original status message is sufficient
    }
    throw new APIClientError(message, response.status);
  }
  const data: unknown = await response.json();
  return schema.parse(data);
}

// ──────────────────────────────────────────────────────────────────────────────
// Public API functions
// ──────────────────────────────────────────────────────────────────────────────

export async function getTrackingPlan(
  persona: string
): Promise<TrackingPlanResponse> {
  if (isMockMode) return mockGetTrackingPlan(persona);
  return apiFetch(
    `/api/tracking-plan/${encodeURIComponent(persona)}`,
    trackingPlanResponseSchema
  );
}

export async function generateConfig(
  req: GenerateConfigRequest
): Promise<GenerateConfigResponse> {
  if (isMockMode) return mockGenerateConfig(req.persona, req.answers);
  return apiFetch("/api/onboarding/generate-config", generateConfigResponseSchema, {
    method: "POST",
    body: JSON.stringify(req),
  });
}

export async function activateConfig(
  req: ActivateConfigRequest
): Promise<ActivateConfigResponse> {
  if (isMockMode) return mockActivateConfig(req);
  return apiFetch("/api/onboarding/activate", activateConfigResponseSchema, {
    method: "POST",
    body: JSON.stringify(req),
  });
}

export async function listMockEmails(to?: string): Promise<MockEmailsResponse> {
  if (isMockMode) return mockListMockEmails();
  const qs = to ? `?to=${encodeURIComponent(to)}` : "";
  return apiFetch(`/api/mock-emails${qs}`, mockEmailsResponseSchema);
}

export async function replayLastTrigger(): Promise<ReplayLastTriggerResponse> {
  if (isMockMode) return mockReplayLastTrigger("realestate");
  return apiFetch(
    "/api/demo/replay-last-trigger",
    triggerResponseSchema,
    { method: "POST", body: "{}" }
  );
}

export async function fireScript(
  req: FireScriptRequest
): Promise<FireScriptResponse> {
  if (isMockMode) return mockFireScript(req.persona);
  return apiFetch("/api/demo/fire-script", fireScriptResponseSchema, {
    method: "POST",
    body: JSON.stringify(req),
  });
}

export async function demoReset(): Promise<DemoResetResponse> {
  if (isMockMode) return mockDemoReset();
  return apiFetch("/api/demo/reset", demoResetResponseSchema, {
    method: "POST",
    body: "{}",
  });
}

// ──────────────────────────────────────────────────────────────────────────────
// Dashboard rehydration: recent-events / active-sessions / recent-triggers
// ──────────────────────────────────────────────────────────────────────────────

// Zod schemas are intentionally permissive (passthrough on unknown keys) so
// new backend fields don't break validation. Required keys are asserted at
// the call-site via TypeScript types, not at the schema boundary.

const sseEventPayloadSchema = z
  .object({
    type: z.string(),
    channel: z.string().default(""),
    event: z.string().optional(),
    anonymousId: z.string(),
    userId: z.string().optional(),
    messageId: z.string(),
    originalTimestamp: z.string(),
    properties: z.record(z.string(), z.unknown()).optional(),
    context: z.record(z.string(), z.unknown()).optional(),
    traits: z.record(z.string(), z.unknown()).optional(),
  })
  .passthrough();

const recentEventsResponseSchema = z.object({
  events: z.array(sseEventPayloadSchema),
});

const sseWindowPayloadSchema = z
  .object({
    anonymous_id: z.string(),
    event_count: z.number(),
    event_type_count: z.record(z.string(), z.number()).default({}),
    event_name_count: z.record(z.string(), z.number()).default({}),
    last_seen: z.string().default(""),
    has_error_event: z.boolean().default(false),
    idle_seconds: z.number().default(0),
  })
  .passthrough();

const activeSessionsResponseSchema = z.object({
  sessions: z.array(sseWindowPayloadSchema),
});

const sseTriggerPayloadSchema = z
  .object({
    id: z.string(),
    rule_name: z.string(),
    persona: z.string(),
    anonymous_id: z.string(),
    fired_at: z.string(),
    window_snapshot: z.record(z.string(), z.unknown()).default({}),
    llm_parsed: z.record(z.string(), z.unknown()).optional(),
    destination: z.string(),
    dispatch_status: z.string(),
  })
  .passthrough();

const recentTriggersResponseSchema = z.object({
  triggers: z.array(sseTriggerPayloadSchema),
});

/** Returns the most recent N events from the events table (camelCase payload). */
export async function listRecentEvents(
  limit = 50
): Promise<{ events: SSEEventPayload[] }> {
  if (isMockMode) return { events: [] };
  return apiFetch(
    `/api/recent-events?limit=${limit}`,
    recentEventsResponseSchema
  ) as Promise<{ events: SSEEventPayload[] }>;
}

/** Returns all current in-memory window snapshots (snake_case). */
export async function listActiveSessions(): Promise<{
  sessions: SSEWindowPayload[];
}> {
  if (isMockMode) return { sessions: [] };
  return apiFetch(
    "/api/active-sessions",
    activeSessionsResponseSchema
  ) as Promise<{ sessions: SSEWindowPayload[] }>;
}

/** Returns the most recent N trigger rows (SSETriggerPayload wire-shape). */
export async function listRecentTriggers(
  limit = 20
): Promise<{ triggers: SSETriggerPayload[] }> {
  if (isMockMode) return { triggers: [] };
  return apiFetch(
    `/api/recent-triggers?limit=${limit}`,
    recentTriggersResponseSchema
  ) as Promise<{ triggers: SSETriggerPayload[] }>;
}
