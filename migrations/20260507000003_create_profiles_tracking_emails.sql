-- +goose Up
CREATE TABLE mock_profiles (
  entity     TEXT         NOT NULL,
  id_type    TEXT         NOT NULL,
  id_value   TEXT         NOT NULL,
  traits     JSONB        NOT NULL,
  updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  PRIMARY KEY (entity, id_type, id_value)
);
CREATE INDEX mock_profiles_value_idx ON mock_profiles(id_value);

CREATE TABLE tracking_plans (
  persona TEXT  PRIMARY KEY,
  spec    JSONB NOT NULL
);

CREATE TABLE mock_emails (
  id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  trigger_id    UUID         REFERENCES triggers(id),
  to_email      TEXT         NOT NULL,
  subject       TEXT         NOT NULL,
  body_markdown TEXT         NOT NULL,
  links         JSONB,
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX mock_emails_created_idx ON mock_emails(created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS mock_emails;
DROP TABLE IF EXISTS tracking_plans;
DROP TABLE IF EXISTS mock_profiles;
