-- +goose Up
CREATE TABLE canned_responses (
  id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  template_name TEXT         NOT NULL,
  persona       TEXT         NOT NULL,
  variant       TEXT         NOT NULL DEFAULT 'default',
  raw_json      JSONB        NOT NULL,
  priority      INT          NOT NULL DEFAULT 100,
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  UNIQUE (template_name, persona, variant)
);
CREATE INDEX canned_responses_lookup_idx ON canned_responses(template_name, persona);

CREATE TABLE canned_kapa_responses (
  id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  query_pattern TEXT         NOT NULL,
  response_json JSONB        NOT NULL,
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX canned_kapa_query_idx ON canned_kapa_responses(query_pattern);

-- +goose Down
DROP TABLE IF EXISTS canned_kapa_responses;
DROP TABLE IF EXISTS canned_responses;
