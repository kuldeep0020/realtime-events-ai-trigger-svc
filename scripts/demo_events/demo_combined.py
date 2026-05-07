#!/usr/bin/env python3
"""Multi-persona orchestrator — interleaves realestate and rs-self cohorts.

Spreads cohort starts across --total-duration seconds, then each user runs
their persona flow independently in parallel threads.

Usage:
    uv run demo_combined.py --realestate-cohort 3 --rs-self-cohort 2 -v
    uv run demo_combined.py --realestate-cohort 1 --rs-self-cohort 1 --dry-run
"""
from __future__ import annotations

import argparse
import logging
import os
import random
import sys
import time
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed
from typing import Any

from faker import Faker

from shared.personas import make_re_user_config, make_rs_user_config, realestate_script, rs_self_script
from shared.pulsar_pub import PulsarPublisher

DEFAULT_PULSAR_URL = "pulsar+ssl://localhost:6651"
DEFAULT_TOPIC = "persistent://public/enterprise/source-events-rudderstacvilo"
DEFAULT_TLS_CERTS = "/Users/kumar/workspace/pulsar-local-ssl/certs/ca.cert.pem"
DEFAULT_INGESTION_URL = "https://rudderstacvilo.dev-rudder.rudderlabs.com"
DEFAULT_RE_WRITE_KEY = "3DNyjJW7sRSqftUb1UQuMJdxlFw"
DEFAULT_RS_WRITE_KEY = "3DNyveG1sfuVHAV598ESyJza3i3"


# ---------------------------------------------------------------------------
# Arg parsing
# ---------------------------------------------------------------------------

def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="Multi-persona demo event orchestrator.",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    p.add_argument("--realestate-cohort", type=int, default=0)
    p.add_argument("--rs-self-cohort", type=int, default=0)
    p.add_argument("--total-duration", type=float, default=60.0,
                   help="Spread cohort user starts over this many seconds")
    p.add_argument("--write-key-re", default=os.environ.get("WRITE_KEY_REALESTATE", DEFAULT_RE_WRITE_KEY))
    p.add_argument("--write-key-rs", default=os.environ.get("WRITE_KEY_RS_SELF", DEFAULT_RS_WRITE_KEY))
    p.add_argument("--target", choices=["pulsar", "http"], default="pulsar")
    p.add_argument("--ingestion-url",
                   default=os.environ.get("INGESTION_URL", DEFAULT_INGESTION_URL))
    p.add_argument("--speed", type=float, default=1.0)
    p.add_argument("--seed", type=int, default=None)
    p.add_argument("--dry-run", action="store_true")
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


# ---------------------------------------------------------------------------
# Per-user worker
# ---------------------------------------------------------------------------

def _user_worker(
    persona: str,
    cfg: Any,
    write_key: str,
    steps_fn: Any,
    args: argparse.Namespace,
    pub: PulsarPublisher | None,
    rng: random.Random,
    fk: Faker,
    start_delay: float,
    log: logging.Logger,
) -> int:
    """Sleep start_delay, then run the persona flow. Returns events sent."""
    import json
    import base64
    import urllib.request

    if start_delay > 0 and args.speed > 0:
        time.sleep(start_delay / args.speed)

    steps = steps_fn(cfg, rng)
    sent = 0

    for step in steps:
        delay = step.delay_seconds
        if args.speed > 0 and delay > 0:
            time.sleep(delay / args.speed)

        event = step.build(cfg.anonymous_id, cfg.user_id, rng, fk)

        if args.dry_run:
            print(json.dumps(event, default=str))
            sent += 1
            continue

        if args.target == "pulsar":
            assert pub is not None
            msg_id = pub.publish(event, write_key=write_key)
            if args.verbose:
                log.info(
                    "[%s] published: anon=%-22s type=%-8s event=%s ts=%s",
                    persona.upper(),
                    event["anonymousId"],
                    event["type"],
                    event.get("event", "(page)"),
                    event["originalTimestamp"],
                )
        else:
            body = json.dumps({"batch": [event], "sentAt": event.get("sentAt", "")}).encode()
            token = base64.b64encode(f"{write_key}:".encode()).decode()
            req = urllib.request.Request(
                f"{args.ingestion_url}/v1/batch",
                data=body,
                headers={"Content-Type": "application/json", "Authorization": f"Basic {token}"},
                method="POST",
            )
            with urllib.request.urlopen(req, timeout=10) as resp:
                if resp.status >= 400:
                    raise RuntimeError(f"HTTP {resp.status}")
            if args.verbose:
                log.info("[%s] http: anon=%s type=%s event=%s",
                         persona.upper(), event["anonymousId"],
                         event["type"], event.get("event", "(page)"))
        sent += 1

    return sent


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main(argv: list[str] | None = None) -> None:
    args = parse_args(argv)

    if args.realestate_cohort == 0 and args.rs_self_cohort == 0:
        print("Specify at least --realestate-cohort N or --rs-self-cohort N", file=sys.stderr)
        sys.exit(1)

    log_level = logging.DEBUG if args.verbose else logging.INFO
    logging.basicConfig(format="%(asctime)s %(levelname)s %(message)s", level=log_level)
    log = logging.getLogger(__name__)

    seed = args.seed
    master_rng = random.Random(seed)

    pconf = pulsar_config_from_env()

    if not args.dry_run and args.target == "pulsar" and not pconf["jwt_token"]:
        log.error("PULSAR_JWT_TOKEN is not set. Export it from .env.local")
        sys.exit(1)

    total_users = args.realestate_cohort + args.rs_self_cohort
    # Spread start offsets uniformly across total_duration
    spread = args.total_duration / max(total_users, 1)

    # Build work items: (persona, cfg, write_key, steps_fn, start_delay)
    work_items = []
    for i in range(args.realestate_cohort):
        anon_id = (
            "anon_demo-re-001"
            if args.realestate_cohort == 1
            else f"anon_demo_re_{master_rng.randint(0, 0xFFFFFFFF):08x}"
        )
        cfg = make_re_user_config(anon_id, rng=random.Random(master_rng.randint(0, 2**31)))
        start_delay = i * spread
        work_items.append(("realestate", cfg, args.write_key_re, realestate_script, start_delay))

    for i in range(args.rs_self_cohort):
        anon_id = (
            "demo-rs-001"
            if args.rs_self_cohort == 1
            else f"demo_rs_{master_rng.randint(0, 0xFFFFFFFF):08x}"
        )
        cfg = make_rs_user_config(anon_id, rng=random.Random(master_rng.randint(0, 2**31)))
        start_delay = (args.realestate_cohort + i) * spread
        work_items.append(("rs-self", cfg, args.write_key_rs, rs_self_script, start_delay))

    user_seeds = [master_rng.randint(0, 2**31) for _ in work_items]

    def _run(item: tuple, user_seed: int) -> tuple[str, str, int]:
        persona, cfg, write_key, steps_fn, start_delay = item
        user_rng = random.Random(user_seed)
        fk = Faker()
        if seed is not None:
            fk.seed_instance(user_seed)
        if args.target == "pulsar" and not args.dry_run:
            with PulsarPublisher(
                url=pconf["url"],
                topic=pconf["topic"],
                jwt_token=pconf["jwt_token"],
                tls_trust_certs=pconf["tls_trust_certs"],
                tls_validate_hostname=pconf["tls_validate_hostname"],
            ) as pub:
                n = _user_worker(persona, cfg, write_key, steps_fn, args, pub,
                                 user_rng, fk, start_delay, log)
        else:
            n = _user_worker(persona, cfg, write_key, steps_fn, args, None,
                             user_rng, fk, start_delay, log)
        return persona, cfg.anonymous_id, n

    grand_total = 0
    with ThreadPoolExecutor(max_workers=total_users) as executor:
        futures = {
            executor.submit(_run, item, s): item
            for item, s in zip(work_items, user_seeds)
        }
        for fut in as_completed(futures):
            try:
                persona, anon_id, n = fut.result()
                grand_total += n
                log.info("[%s] user=%s done (%d events)", persona.upper(), anon_id, n)
            except Exception as exc:  # noqa: BLE001
                log.error("Worker failed: %s", exc)

    log.info("Combined run complete. Total events: %d", grand_total)


if __name__ == "__main__":
    main()
