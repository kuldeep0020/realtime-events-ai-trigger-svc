#!/usr/bin/env bash
# Start the realtime-events-ai-trigger-svc backend with local env.
#
# Works from any shell (bash, zsh, fish) because this script itself
# uses bash, sourcing .env.local in bash semantics regardless of the
# caller's shell.
#
# Usage:
#   bash scripts/start-backend-local.sh        # run from repo root
#   bash scripts/start-backend-local.sh --foreground   # don't nohup
set -euo pipefail

# Resolve repo root (the parent of this script's parent).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ENV_FILE="${REPO_ROOT}/.env.local"
LOG_DIR="/tmp/rt-svc-logs"
LOG_FILE="${LOG_DIR}/serve.log"
BIN="/tmp/realtime-trigger"

if [ ! -f "$ENV_FILE" ]; then
  echo "ERROR: $ENV_FILE not found." >&2
  echo "Create it with the secret env vars (see .env.example). Required keys:" >&2
  echo "  PULSAR_URL, PULSAR_TOPIC, PULSAR_JWT_TOKEN, PULSAR_TLS_TRUST_CERTS, SLACK_WEBHOOK_URL" >&2
  exit 1
fi

if [ ! -x "$BIN" ]; then
  echo "Building $BIN ..." >&2
  (cd "$REPO_ROOT" && go build -o "$BIN" ./cmd/realtime-trigger)
fi

# Stop any prior instance.
pkill -f "$BIN" 2>/dev/null || true
sleep 1

mkdir -p "$LOG_DIR"

# Load .env.local — bash sourcing handles `KEY=value` form regardless of
# the caller's shell. set -a auto-exports each VAR=value assignment.
set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

# Local-only defaults (override-able by anything already in env).
: "${POSTGRES_DSN:=postgresql://postuser:postpass@localhost:5432/postdb?sslmode=disable}"
: "${INGESTION_URL:=https://rudderstacvilo.dev-rudder.rudderlabs.com}"
: "${ALLOWED_WRITE_KEYS:=3DNyjJW7sRSqftUb1UQuMJdxlFw,3DNyveG1sfuVHAV598ESyJza3i3}"
: "${LLM_MODE:=canned}"
: "${KAPA_MODE:=canned}"
: "${ACTIVATION_MODE:=mock}"
: "${LOG_LEVEL:=info}"
: "${DEMO_FIRE_TARGET:=pulsar}"

export POSTGRES_DSN INGESTION_URL ALLOWED_WRITE_KEYS LLM_MODE KAPA_MODE ACTIVATION_MODE LOG_LEVEL DEMO_FIRE_TARGET

# Sanity: must have a Slack webhook for realestate Slack delivery.
if [ -z "${SLACK_WEBHOOK_URL:-}" ]; then
  echo "WARN: SLACK_WEBHOOK_URL is not set. Realestate triggers will record dispatch_status=failed." >&2
fi
if [ -z "${PULSAR_JWT_TOKEN:-}" ]; then
  echo "ERROR: PULSAR_JWT_TOKEN is not set in $ENV_FILE." >&2
  exit 1
fi

# Run from REPO_ROOT so the binary's DiskSeedFS finds ./seed/.
cd "$REPO_ROOT"

if [ "${1:-}" = "--foreground" ]; then
  echo "starting $BIN in foreground (Ctrl-C to stop)"
  exec "$BIN" serve
else
  echo "starting $BIN in background; logs → $LOG_FILE"
  nohup "$BIN" serve > "$LOG_FILE" 2>&1 &
  PID=$!
  sleep 3
  if ! kill -0 "$PID" 2>/dev/null; then
    echo "ERROR: backend died on startup. Tail of log:" >&2
    tail -30 "$LOG_FILE" >&2
    exit 1
  fi
  HEALTH=$(curl -s --max-time 3 http://localhost:8080/healthz 2>/dev/null || true)
  if [ -z "$HEALTH" ]; then
    echo "WARN: /healthz did not respond yet. Check $LOG_FILE." >&2
  else
    echo "ok pid=$PID  health=$HEALTH"
  fi
fi
