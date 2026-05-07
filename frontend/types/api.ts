/**
 * API type contracts mirroring backend §3.10 handler shapes.
 * Every type here corresponds 1:1 with a Go struct in internal/api/.
 */

// ──────────────────────────────────────────────────────────────────────────────
// Tracking plan
// ──────────────────────────────────────────────────────────────────────────────

export interface TrackingPlanProperty {
  name: string;
  type: "string" | "number" | "integer" | "boolean" | "array" | "object";
  description: string;
  required: boolean;
}

export interface TrackingPlanEvent {
  name: string;
  description: string;
  properties: TrackingPlanProperty[];
}

export interface TrackingPlanSpec {
  persona: string;
  version: string;
  description: string;
  events: TrackingPlanEvent[];
}

/** Response from GET /api/tracking-plan/{persona} */
export interface TrackingPlanResponse {
  persona: string;
  spec: TrackingPlanSpec;
}

// ──────────────────────────────────────────────────────────────────────────────
// Onboarding: generate-config
// ──────────────────────────────────────────────────────────────────────────────

export type Persona = "realestate" | "rs-self";

export type WizardAnswers = Record<string, string | string[] | number>;

/** Request body for POST /api/onboarding/generate-config */
export interface GenerateConfigRequest {
  persona: Persona;
  answers: WizardAnswers;
}

/** Response from POST /api/onboarding/generate-config */
export interface GenerateConfigResponse {
  persona: string;
  source: string;
  config_yaml: string;
  description: string;
}

// ──────────────────────────────────────────────────────────────────────────────
// Onboarding: activate
// ──────────────────────────────────────────────────────────────────────────────

/** Request body for POST /api/onboarding/activate */
export interface ActivateConfigRequest {
  /** UUID of an existing config row */
  id?: string;
  /** Persona name — required when id is absent */
  persona?: string;
  tenant_id?: string;
  /** YAML body — required when id is absent */
  config_yaml?: string;
}

/** Response from POST /api/onboarding/activate */
export interface ActivateConfigResponse {
  id: string;
  active: boolean;
  persona: string;
}

// ──────────────────────────────────────────────────────────────────────────────
// SSE streams
// ──────────────────────────────────────────────────────────────────────────────

export type StreamName = "events" | "windows" | "triggers" | "mock_emails";

export interface SSEEventPayload {
  type: string;
  channel: string;
  event?: string;
  anonymousId: string;
  userId?: string;
  messageId: string;
  originalTimestamp: string;
  properties?: Record<string, unknown>;
  context?: Record<string, unknown>;
  traits?: Record<string, unknown>;
}

export interface SSEWindowPayload {
  anonymous_id: string;
  event_count: number;
  event_type_count: Record<string, number>;
  event_name_count: Record<string, number>;
  last_seen: string;
  has_error_event: boolean;
  idle_seconds: number;
}

export interface SSETriggerPayload {
  id: string;
  rule_name: string;
  persona: string;
  anonymous_id: string;
  fired_at: string;
  window_snapshot: Record<string, unknown>;
  llm_parsed?: Record<string, unknown>;
  destination: string;
  dispatch_status: string;
}

export interface MockEmailPayload {
  id: string;
  trigger_id?: string;
  to_email: string;
  subject: string;
  body_markdown: string;
  links?: Array<{ title: string; url: string }>;
  created_at: string;
}

/** Response from GET /api/mock-emails */
export interface MockEmailsResponse {
  emails: MockEmailPayload[];
}

// ──────────────────────────────────────────────────────────────────────────────
// Demo controller
// ──────────────────────────────────────────────────────────────────────────────

export interface FireScriptRequest {
  persona: Persona;
  speed_factor?: number;
}

export interface FireScriptResponse {
  persona: string;
  event_count: number;
  status: string;
}

export interface DemoResetResponse {
  status: string;
}

export interface ReplayLastTriggerResponse {
  id: string;
  rule_name: string;
  persona: string;
  anonymous_id: string;
  fired_at: string;
  window_snapshot: Record<string, unknown>;
  llm_parsed?: Record<string, unknown>;
  destination: string;
  dispatch_status: string;
}

// ──────────────────────────────────────────────────────────────────────────────
// API error envelope
// ──────────────────────────────────────────────────────────────────────────────

export interface APIError {
  error: string;
  status?: string;
}
