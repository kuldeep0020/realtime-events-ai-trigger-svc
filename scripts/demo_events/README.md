# demo_events — Python demo event toolkit

Publishes rich, realistic RudderStack browser-channel events directly to local
Pulsar (or the ingestion-svc HTTP endpoint) for demo and soak-test runs.

This is an alternative to the Go `demofire` path. Events match the v3 JS-SDK
wire shape so the existing Go consumer parses them unchanged.

## Install

```bash
cd scripts/demo_events
uv sync
```

Python 3.11+ required. Dependencies: `pulsar-client>=3.5.0`, `faker>=27.0.0`.

## Quickstart

```bash
# Load env vars (Pulsar URL, JWT token, TLS cert path)
set -a; source ../../.env.local; set +a

# Single-user realestate flow (~30s, fires realtor_session_abandoned)
uv run demo_realestate.py -v

# Single-user rs-self flow (~12s, fires onboarding_errored + onboarding_stuck)
uv run demo_rs_self.py -v
```

## Common recipes

```bash
# Inspect event JSON without publishing
uv run demo_realestate.py --dry-run | jq -r '.event,.anonymousId,.originalTimestamp'

# 3 concurrent realestate users (3 triggers expected)
uv run demo_realestate.py --cohort-size 3 -v

# RS-self with a fixed seed for reproducible event content
uv run demo_rs_self.py --seed 42 --dry-run

# Double speed (events fire at half wall-clock time)
uv run demo_realestate.py --speed 2.0 -v

# Stress test — no sleep between events
uv run demo_realestate.py --speed 0 --cohort-size 10

# Send via HTTP instead of Pulsar
uv run demo_realestate.py --target http --ingestion-url https://rudderstacvilo.dev-rudder.rudderlabs.com -v

# Combined multi-persona run (interleaved, starts spread over 60s)
uv run demo_combined.py --realestate-cohort 2 --rs-self-cohort 2 -v
```

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `PULSAR_URL` | `pulsar+ssl://localhost:6651` | Broker URL |
| `PULSAR_TOPIC` | `persistent://public/enterprise/source-events-rudderstacvilo` | Topic |
| `PULSAR_JWT_TOKEN` | (required) | JWT bearer token |
| `PULSAR_TLS_TRUST_CERTS` | `/Users/kumar/workspace/pulsar-local-ssl/certs/ca.cert.pem` | CA cert path |
| `PULSAR_TLS_VALIDATE_HOSTNAME` | `true` | Hostname verification |
| `INGESTION_URL` | `https://rudderstacvilo.dev-rudder.rudderlabs.com` | HTTP target (--target http) |
| `WRITE_KEY_REALESTATE` | `3DNyjJW7sRSqftUb1UQuMJdxlFw` | Override realestate writeKey |
| `WRITE_KEY_RS_SELF` | `3DNyveG1sfuVHAV598ESyJza3i3` | Override rs-self writeKey |

CLI flags override env vars, which override built-in defaults.

## Troubleshooting

**TLS handshake error / certificate verify failed**
- Check `PULSAR_TLS_TRUST_CERTS` points to your actual CA cert.
- Default path: `/Users/kumar/workspace/pulsar-local-ssl/certs/ca.cert.pem`

**`PULSAR_JWT_TOKEN is not set`**
- Run `set -a; source ../../.env.local; set +a` before the script.

**`pulsar.exceptions.ProducerBusy`**
- Another producer with the same name is connected. Either wait or set a unique `--producer-name` (not exposed as flag; edit `pulsar_pub.py` `producer_name` default).

**Trigger not firing / event_count too low**
- Ensure the persona rules are activated: `curl -X POST http://localhost:8080/api/onboarding/activate -H 'Content-Type: application/json' -d '{"persona":"realestate"}'`
- Check `originalTimestamp` is per-event UTC (not identical): `uv run demo_realestate.py --dry-run | jq .originalTimestamp`

**`Connection refused` on Pulsar**
- Start the local Pulsar stack: see `pulsar-local-ssl/` compose file.
