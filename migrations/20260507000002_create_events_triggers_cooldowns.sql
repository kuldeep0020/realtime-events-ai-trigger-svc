-- +goose Up
CREATE TABLE events (
  id                BIGSERIAL    PRIMARY KEY,
  pulsar_message_id TEXT,
  anonymous_id      TEXT         NOT NULL,
  user_id           TEXT,
  write_key         TEXT         NOT NULL,
  event_type        TEXT         NOT NULL,
  event_name        TEXT,
  page_path         TEXT,
  payload           JSONB        NOT NULL,
  received_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX events_anon_idx     ON events(anonymous_id, received_at DESC);
CREATE INDEX events_received_idx ON events(received_at DESC);

CREATE TABLE triggers (
  id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  rule_id         UUID         REFERENCES rules(id),
  rule_name       TEXT         NOT NULL,
  persona         TEXT         NOT NULL,
  anonymous_id    TEXT         NOT NULL,
  fired_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  window_snapshot JSONB        NOT NULL,
  full_events     JSONB        NOT NULL,
  enriched_traits JSONB,
  kapa_result     JSONB,
  llm_raw         TEXT,
  llm_parsed      JSONB,
  llm_source      TEXT,
  destination     TEXT         NOT NULL,
  dispatch_status TEXT         NOT NULL DEFAULT 'pending',
  dispatched_at   TIMESTAMPTZ,
  error           TEXT
);
CREATE INDEX triggers_fired_idx ON triggers(fired_at DESC);

CREATE TABLE cooldowns (
  rule_id      UUID         NOT NULL,
  anonymous_id TEXT         NOT NULL,
  expires_at   TIMESTAMPTZ  NOT NULL,
  PRIMARY KEY (rule_id, anonymous_id)
);
CREATE INDEX cooldowns_expires_idx ON cooldowns(expires_at);

-- +goose Down
DROP TABLE IF EXISTS cooldowns;
DROP TABLE IF EXISTS triggers;
DROP TABLE IF EXISTS events;
