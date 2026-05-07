"""Tests for RudderStack v3 wire-format event shape produced by shared/event.py."""
from __future__ import annotations

import re
import time

from shared.event import build_identify, build_page, build_track, make_context, make_page

# ---------------------------------------------------------------------------
# Shared fixtures
# ---------------------------------------------------------------------------

_PAGE = make_page(url="https://example.com/", path="/", title="Home")
_CTX = make_context(page=_PAGE)
_ANON = "anon-test-001"
_UID = "user-test-001"


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

def test_messageId_is_32_hex_chars() -> None:
    ev = build_track(_ANON, _UID, "Test Event", {}, _CTX)
    assert re.fullmatch(r"[0-9a-f]{32}", ev["messageId"]), (
        f"messageId should be 32 hex chars, got: {ev['messageId']!r}"
    )


def test_originalTimestamp_iso8601_with_z() -> None:
    ev = build_track(_ANON, _UID, "Test Event", {}, _CTX)
    ts = ev["originalTimestamp"]
    assert ts.endswith("Z"), f"originalTimestamp should end with Z, got: {ts!r}"
    # millisecond precision: yyyy-mm-ddThh:mm:ss.mmmZ = 24 chars
    assert re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z", ts), (
        f"originalTimestamp not in expected format: {ts!r}"
    )


def test_camelCase_keys_no_snake_case_leak() -> None:
    allowed = {
        "type", "channel", "event", "anonymousId", "userId",
        "messageId", "originalTimestamp", "sentAt", "context",
        "properties", "traits", "integrations",
    }
    for ev in [
        build_identify(_ANON, _UID, {"plan": "free"}, _CTX),
        build_page(_ANON, _UID, _CTX),
        build_track(_ANON, _UID, "Test", {}, _CTX),
    ]:
        unexpected = set(ev.keys()) - allowed
        assert not unexpected, f"Unexpected top-level keys: {unexpected}"


def test_userId_omitted_when_empty() -> None:
    ev = build_identify(_ANON, "", {"plan": "free"}, _CTX)
    assert "userId" not in ev, "userId should be absent when user_id is empty string"


def test_two_consecutive_calls_have_distinct_timestamps() -> None:
    ev1 = build_track(_ANON, _UID, "Test", {}, _CTX)
    time.sleep(0.002)
    ev2 = build_track(_ANON, _UID, "Test", {}, _CTX)
    assert ev1["originalTimestamp"] != ev2["originalTimestamp"], (
        "Two calls 2ms apart should produce distinct timestamps"
    )


def test_integrations_all_true() -> None:
    for ev in [
        build_identify(_ANON, _UID, {}, _CTX),
        build_page(_ANON, _UID, _CTX),
        build_track(_ANON, _UID, "Test", {}, _CTX),
    ]:
        assert ev["integrations"] == {"All": True}, (
            f"integrations should be {{'All': True}}, got: {ev['integrations']}"
        )
