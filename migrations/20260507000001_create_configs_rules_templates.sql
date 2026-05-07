-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE configs (
  id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id   TEXT         NOT NULL,
  persona     TEXT         NOT NULL,
  config_yaml TEXT         NOT NULL,
  active      BOOLEAN      NOT NULL DEFAULT FALSE,
  created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX configs_active_idx ON configs(tenant_id, persona) WHERE active;

CREATE TABLE rules (
  id        UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  config_id UUID    NOT NULL REFERENCES configs(id) ON DELETE CASCADE,
  name      TEXT    NOT NULL,
  spec      JSONB   NOT NULL,
  enabled   BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE INDEX rules_enabled_idx ON rules(config_id) WHERE enabled;

CREATE TABLE action_templates (
  id               UUID  PRIMARY KEY DEFAULT gen_random_uuid(),
  name             TEXT  UNIQUE NOT NULL,
  system_prompt    TEXT  NOT NULL,
  user_prompt_tmpl TEXT  NOT NULL,
  output_format    TEXT  NOT NULL DEFAULT 'json'
);

-- +goose Down
DROP TABLE IF EXISTS action_templates;
DROP TABLE IF EXISTS rules;
DROP TABLE IF EXISTS configs;
