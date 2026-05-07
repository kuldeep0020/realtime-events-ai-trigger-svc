"""RudderStack v3 JS-SDK on-the-wire event builders.

Each builder calls datetime.now(UTC) at invocation time so timestamps are
stamped at actual publish time — never cached or pre-built.

Wire-format requirements (must match internal/event/event.go JSON tags):
- originalTimestamp / sentAt: ISO8601 with millisecond precision + trailing Z
- messageId: 32-char hex string (uuid4 hex, no dashes) matching Go crypto/rand
- integrations: {"All": true}
- channel: "browser"
- traits: top-level field on identify events only
- properties: present on track and page events
"""
from __future__ import annotations

import json
import uuid
from datetime import datetime, timezone
from typing import Any


# ---------------------------------------------------------------------------
# Timestamp helpers
# ---------------------------------------------------------------------------

def _now_iso() -> str:
    """Return current UTC time as ISO8601 with millisecond precision + Z."""
    now = datetime.now(timezone.utc)
    # strftime gives 6-digit microseconds; slice to 3 for milliseconds
    return now.strftime("%Y-%m-%dT%H:%M:%S.") + f"{now.microsecond // 1000:03d}Z"


def _new_message_id() -> str:
    """Return a 32-char hex string matching Go's crypto/rand 16-byte hex."""
    return uuid.uuid4().hex  # 32 hex chars, no dashes


# ---------------------------------------------------------------------------
# Context sub-dict builders
# ---------------------------------------------------------------------------

def make_library() -> dict[str, Any]:
    return {"name": "analytics-js", "version": "3.5.0"}


def make_os(name: str = "macOS", version: str = "13.0") -> dict[str, Any]:
    return {"name": name, "version": version}


def make_screen(
    width: int = 1440,
    height: int = 900,
    density: int = 2,
    inner_width: int = 1440,
    inner_height: int = 874,
) -> dict[str, Any]:
    return {
        "width": width,
        "height": height,
        "density": density,
        "innerWidth": inner_width,
        "innerHeight": inner_height,
    }


def make_page(
    url: str,
    path: str,
    title: str,
    referrer: str = "",
    search: str = "",
    initial_referrer: str = "",
    initial_referring_domain: str = "",
) -> dict[str, Any]:
    return {
        "url": url,
        "path": path,
        "referrer": referrer,
        "search": search,
        "title": title,
        "initial_referrer": initial_referrer,
        "initial_referring_domain": initial_referring_domain,
    }


def make_context(
    page: dict[str, Any],
    campaign: dict[str, str] | None = None,
    locale: str = "en-IN",
    timezone: str = "Asia/Kolkata",
    user_agent: str = (
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
        "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
    ),
    session_id: int = 1714978800000,
    session_start: bool = False,
    traits: dict[str, Any] | None = None,
    screen: dict[str, Any] | None = None,
    os_info: dict[str, Any] | None = None,
) -> dict[str, Any]:
    ctx: dict[str, Any] = {
        "library": make_library(),
        "page": page,
        "os": os_info or make_os(),
        "screen": screen or make_screen(),
        "userAgent": user_agent,
        "locale": locale,
        "timezone": timezone,
        "sessionId": session_id,
        "sessionStart": session_start,
    }
    if campaign:
        ctx["campaign"] = campaign
    if traits:
        ctx["traits"] = traits
    return ctx


# ---------------------------------------------------------------------------
# Top-level event builders
# All stamp timestamps at call time.
# ---------------------------------------------------------------------------

def build_identify(
    anonymous_id: str,
    user_id: str,
    traits: dict[str, Any],
    context: dict[str, Any],
) -> dict[str, Any]:
    """Build an identify event dict matching the v3 SDK wire shape."""
    ts = _now_iso()
    ev: dict[str, Any] = {
        "type": "identify",
        "channel": "browser",
        "anonymousId": anonymous_id,
        "messageId": _new_message_id(),
        "originalTimestamp": ts,
        "sentAt": ts,
        "context": context,
        "traits": traits,
        "integrations": {"All": True},
    }
    if user_id:
        ev["userId"] = user_id
    return ev


def build_page(
    anonymous_id: str,
    user_id: str,
    context: dict[str, Any],
    properties: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Build a page event dict."""
    ts = _now_iso()
    ev: dict[str, Any] = {
        "type": "page",
        "channel": "browser",
        "anonymousId": anonymous_id,
        "messageId": _new_message_id(),
        "originalTimestamp": ts,
        "sentAt": ts,
        "context": context,
        "properties": properties or {},
        "integrations": {"All": True},
    }
    if user_id:
        ev["userId"] = user_id
    return ev


def build_track(
    anonymous_id: str,
    user_id: str,
    event_name: str,
    properties: dict[str, Any],
    context: dict[str, Any],
) -> dict[str, Any]:
    """Build a track event dict."""
    ts = _now_iso()
    ev: dict[str, Any] = {
        "type": "track",
        "channel": "browser",
        "event": event_name,
        "anonymousId": anonymous_id,
        "messageId": _new_message_id(),
        "originalTimestamp": ts,
        "sentAt": ts,
        "context": context,
        "properties": properties,
        "integrations": {"All": True},
    }
    if user_id:
        ev["userId"] = user_id
    return ev


def pretty_print(event: dict[str, Any]) -> None:
    """Print a single event as formatted JSON to stdout."""
    print(json.dumps(event, indent=2, default=str))
