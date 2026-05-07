#!/usr/bin/env python3
"""RS-self onboarding persona demo event publisher.

Fires the onboarding_errored + onboarding_stuck trigger flow.
Default: 1 user, fixed anonymousId demo-rs-001.

Usage:
    uv run demo_rs_self.py [OPTIONS]
    uv run demo_rs_self.py --dry-run | jq .
    uv run demo_rs_self.py --cohort-size 2 -v
"""
from __future__ import annotations

import argparse
import json
import logging
import os
import random
import sys
import time
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed
from typing import Any

from faker import Faker

from shared.env import autoload_env
from shared.event import pretty_print
from shared.personas import RSUserConfig, make_rs_user_config, rs_self_script
from shared.pulsar_pub import PulsarPublisher

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------

DEFAULT_WRITE_KEY = "3DNyveG1sfuVHAV598ESyJza3i3"
DEFAULT_ANON_ID = "demo-rs-001"
DEFAULT_PULSAR_URL = "pulsar+ssl://localhost:6651"
DEFAULT_TOPIC = "persistent://public/enterprise/source-events-rudderstacvilo"
DEFAULT_TLS_CERTS = "/Users/kumar/workspace/pulsar-local-ssl/certs/ca.cert.pem"
DEFAULT_INGESTION_URL = "https://rudderstacvilo.dev-rudder.rudderlabs.com"


# ---------------------------------------------------------------------------
# Arg parsing
# ---------------------------------------------------------------------------

def _early_parse_env_file(argv: list[str]) -> tuple[str | None, list[str]]:
    """Pre-extract --env-file BEFORE the full argparse so autoload_env runs first.

    argparse defaults (e.g. os.environ.get("WRITE_KEY_RS_SELF", ...)) resolve
    against os.environ at parse_args() call time. We must call autoload_env()
    before that call so env-file values are visible to the defaults.
    """
    p = argparse.ArgumentParser(add_help=False)
    p.add_argument("--env-file", default=None)
    ns, rest = p.parse_known_args(argv)
    return ns.env_file, rest


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="Publish rs-self onboarding demo events to Pulsar or HTTP.",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    p.add_argument("--cohort-size", type=int, default=1)
    p.add_argument("--anonymous-id", default=DEFAULT_ANON_ID,
                   help="Fixed anonymousId for cohort-size=1")
    p.add_argument("--write-key", default=os.environ.get("WRITE_KEY_RS_SELF", DEFAULT_WRITE_KEY))
    p.add_argument("--target", choices=["pulsar", "http"], default="pulsar")
    p.add_argument("--ingestion-url",
                   default=os.environ.get("INGESTION_URL", DEFAULT_INGESTION_URL))
    p.add_argument("--speed", type=float, default=1.0)
    p.add_argument("--seed", type=int, default=None)
    p.add_argument("--dry-run", action="store_true")
    p.add_argument("--env-file", default=None, metavar="PATH",
                   help="Path to .env file to load (default: auto-detect .env.local)")
    p.add_argument("-v", "--verbose", action="store_true")
    return p.parse_args(argv)


def pulsar_config_from_env() -> dict[str, Any]:
    return {
        "url":                   os.environ.get("PULSAR_URL", DEFAULT_PULSAR_URL),
        "topic":                 os.environ.get("PULSAR_TOPIC", DEFAULT_TOPIC),
        "jwt_token":             os.environ.get("PULSAR_JWT_TOKEN", ""),
        "tls_trust_certs":       os.environ.get("PULSAR_TLS_TRUST_CERTS", DEFAULT_TLS_CERTS),
        "tls_validate_hostname": os.environ.get("PULSAR_TLS_VALIDATE_HOSTNAME", "true").lower() == "true",
    }


def _http_publish(event: dict[str, Any], write_key: str, ingestion_url: str) -> None:
    import base64
    import urllib.request

    body = json.dumps({"batch": [event], "sentAt": event.get("sentAt", "")}).encode()
    token = base64.b64encode(f"{write_key}:".encode()).decode()
    req = urllib.request.Request(
        f"{ingestion_url}/v1/batch",
        data=body,
        headers={"Content-Type": "application/json", "Authorization": f"Basic {token}"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=10) as resp:
        if resp.status >= 400:
            raise RuntimeError(f"HTTP {resp.status}")


def run_user_flow(
    cfg: RSUserConfig,
    write_key: str,
    args: argparse.Namespace,
    pub: PulsarPublisher | None,
    rng: random.Random,
    fk: Faker,
    log: logging.Logger,
) -> int:
    steps = rs_self_script(cfg, rng)
    sent = 0

    for step in steps:
        delay = step.delay_seconds
        if args.speed > 0 and delay > 0:
            time.sleep(delay / args.speed)

        event = step.build(cfg.anonymous_id, cfg.user_id, rng, fk)

        if args.dry_run:
            pretty_print(event)
            sent += 1
            continue

        if args.target == "pulsar":
            assert pub is not None
            msg_id = pub.publish(event, write_key=write_key)
            if args.verbose:
                log.info(
                    "[RS] published: anon=%-16s type=%-8s event=%-28s ts=%s msgId=%s",
                    event["anonymousId"],
                    event["type"],
                    event.get("event", "(page)"),
                    event["originalTimestamp"],
                    msg_id,
                )
        else:
            _http_publish(event, write_key, args.ingestion_url)
            if args.verbose:
                log.info("[RS] http sent: anon=%s type=%s event=%s",
                         event["anonymousId"], event["type"], event.get("event", "(page)"))

        sent += 1

    return sent


def main(argv: list[str] | None = None) -> None:
    if argv is None:
        argv = sys.argv[1:]
    # Phase 1: extract --env-file only, so autoload_env runs before full parse.
    env_file, _ = _early_parse_env_file(argv)
    env_path = autoload_env(env_file)
    # Phase 2: full parse — argparse defaults now see the loaded env.
    args = parse_args(argv)

    log_level = logging.DEBUG if args.verbose else logging.INFO
    logging.basicConfig(format="%(asctime)s %(levelname)s %(message)s", level=log_level)
    log = logging.getLogger(__name__)

    if args.verbose:
        if env_path:
            log.info("Loaded env from %s", env_path)
        else:
            log.info("No .env.local found; using process env")

    seed = args.seed
    master_rng = random.Random(seed)

    if args.cohort_size == 1:
        configs = [make_rs_user_config(args.anonymous_id, rng=master_rng)]
    else:
        configs = [
            make_rs_user_config(
                f"demo_rs_{master_rng.randint(0, 0xFFFFFFFF):08x}",
                rng=random.Random(master_rng.randint(0, 2**31)),
            )
            for _ in range(args.cohort_size)
        ]

    pconf = pulsar_config_from_env()
    write_key = args.write_key

    if args.dry_run:
        cfg = configs[0]
        user_seed = master_rng.randint(0, 2**31)
        user_rng = random.Random(user_seed)
        fk = Faker()
        if seed is not None:
            fk.seed_instance(user_seed)
        run_user_flow(cfg, write_key, args, None, user_rng, fk, log)
        return

    if args.target == "pulsar" and not pconf["jwt_token"]:
        log.error("PULSAR_JWT_TOKEN is not set. Export it from .env.local")
        sys.exit(1)

    def _run_one(cfg: RSUserConfig, user_seed: int) -> int:
        user_rng = random.Random(user_seed)
        fk = Faker()
        if seed is not None:
            fk.seed_instance(user_seed)
        if args.target == "pulsar":
            with PulsarPublisher(
                url=pconf["url"],
                topic=pconf["topic"],
                jwt_token=pconf["jwt_token"],
                tls_trust_certs=pconf["tls_trust_certs"],
                tls_validate_hostname=pconf["tls_validate_hostname"],
            ) as pub:
                return run_user_flow(cfg, write_key, args, pub, user_rng, fk, log)
        else:
            return run_user_flow(cfg, write_key, args, None, user_rng, fk, log)

    if args.cohort_size == 1:
        total = _run_one(configs[0], master_rng.randint(0, 2**31))
        log.info("Done. Events published: %d", total)
    else:
        user_seeds = [master_rng.randint(0, 2**31) for _ in configs]
        total = 0
        with ThreadPoolExecutor(max_workers=args.cohort_size) as executor:
            futures = {executor.submit(_run_one, cfg, s): cfg for cfg, s in zip(configs, user_seeds)}
            for fut in as_completed(futures):
                cfg = futures[fut]
                try:
                    n = fut.result()
                    total += n
                    log.info("User %s finished (%d events)", cfg.anonymous_id, n)
                except Exception as exc:  # noqa: BLE001
                    log.error("User %s failed: %s", cfg.anonymous_id, exc)
        log.info("Cohort done. Total events published: %d", total)


if __name__ == "__main__":
    main()
