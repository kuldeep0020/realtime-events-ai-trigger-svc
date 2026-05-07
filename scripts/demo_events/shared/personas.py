"""Cohort data and event-flow generators for each demo persona.

Realestate and RS-self flows are each a list of (delay_seconds, callable)
pairs. The callable receives (anonymous_id, user_id, rng, faker_instance)
and returns an event dict. Callers sleep delay_seconds between steps and
invoke the callable at publish time so timestamps are stamped correctly.
"""
from __future__ import annotations

import random
from dataclasses import dataclass, field
from typing import Any, Callable

from faker import Faker

from shared.event import (
    build_identify,
    build_page,
    build_track,
    make_context,
    make_page,
    make_screen,
)

# ---------------------------------------------------------------------------
# Type alias for a step callable
# ---------------------------------------------------------------------------

# StepFn(anonymous_id, user_id, rng, fk) -> event dict
StepFn = Callable[[str, str, random.Random, Faker], dict[str, Any]]


@dataclass
class ScriptStep:
    delay_seconds: float
    build: StepFn


# ---------------------------------------------------------------------------
# Realestate cohort data
# ---------------------------------------------------------------------------

REALESTATE_LISTINGS: list[dict[str, Any]] = [
    {"id": "L101", "suburb": "suburb-1",     "price": 1_200_000, "bedrooms": 3, "bathrooms": 2, "sq_ft": 2100, "year_built": 2015, "agent": "Priya N.",  "listed_days_ago": 12, "view_count": 145, "amenities": ["garage_2", "garden"]},
    {"id": "L102", "suburb": "suburb-1",     "price": 1_550_000, "bedrooms": 4, "bathrooms": 2, "sq_ft": 2600, "year_built": 2018, "agent": "Priya N.",  "listed_days_ago": 5,  "view_count": 212, "amenities": ["pool", "garage_2", "garden"]},
    {"id": "L103", "suburb": "suburb-1",     "price": 1_050_000, "bedrooms": 2, "bathrooms": 1, "sq_ft": 1650, "year_built": 1998, "agent": "Priya N.",  "listed_days_ago": 30, "view_count": 88,  "amenities": ["garage_2"]},
    {"id": "L104", "suburb": "suburb-2",     "price": 2_100_000, "bedrooms": 5, "bathrooms": 3, "sq_ft": 3400, "year_built": 2020, "agent": "Priya N.",  "listed_days_ago": 8,  "view_count": 341, "amenities": ["pool", "garage_3", "garden", "balcony"]},
    {"id": "L105", "suburb": "suburb-2",     "price": 1_750_000, "bedrooms": 4, "bathrooms": 2, "sq_ft": 2900, "year_built": 2017, "agent": "Priya N.",  "listed_days_ago": 15, "view_count": 176, "amenities": ["garage_2", "solar"]},
    {"id": "L106", "suburb": "suburb-2",     "price": 1_350_000, "bedrooms": 3, "bathrooms": 2, "sq_ft": 2200, "year_built": 2010, "agent": "Priya N.",  "listed_days_ago": 22, "view_count": 103, "amenities": ["garden", "fireplace"]},
    {"id": "L107", "suburb": "suburb-3",     "price": 1_500_000, "bedrooms": 4, "bathrooms": 3, "sq_ft": 2400, "year_built": 2019, "agent": "Arjun M.",  "listed_days_ago": 7,  "view_count": 198, "amenities": ["pool", "garage_2", "garden"]},
    {"id": "L108", "suburb": "suburb-3",     "price": 2_450_000, "bedrooms": 5, "bathrooms": 4, "sq_ft": 3800, "year_built": 2022, "agent": "Arjun M.",  "listed_days_ago": 3,  "view_count": 412, "amenities": ["pool", "garage_3", "garden", "solar", "balcony"]},
    {"id": "L109", "suburb": "suburb-3",     "price": 1_180_000, "bedrooms": 3, "bathrooms": 2, "sq_ft": 1950, "year_built": 2005, "agent": "Arjun M.",  "listed_days_ago": 45, "view_count": 67,  "amenities": ["garage_2"]},
    {"id": "L110", "suburb": "suburb-3",     "price": 1_900_000, "bedrooms": 4, "bathrooms": 3, "sq_ft": 3100, "year_built": 2021, "agent": "Arjun M.",  "listed_days_ago": 18, "view_count": 256, "amenities": ["garage_2", "garden", "fireplace"]},
    {"id": "L111", "suburb": "countryside-1","price": 2_800_000, "bedrooms": 5, "bathrooms": 3, "sq_ft": 4100, "year_built": 2016, "agent": "Mira K.",   "listed_days_ago": 60, "view_count": 134, "amenities": ["pool", "garage_3", "garden", "fireplace"]},
    {"id": "L112", "suburb": "suburb-1",     "price": 1_350_000, "bedrooms": 3, "bathrooms": 2, "sq_ft": 2200, "year_built": 2014, "agent": "Priya N.",  "listed_days_ago": 4,  "view_count": 125, "amenities": ["garden", "garage_2"]},
    {"id": "L113", "suburb": "countryside-1","price": 3_200_000, "bedrooms": 6, "bathrooms": 4, "sq_ft": 4500, "year_built": 2023, "agent": "Mira K.",   "listed_days_ago": 2,  "view_count": 487, "amenities": ["pool", "garage_3", "garden", "solar", "balcony", "fireplace"]},
    {"id": "L114", "suburb": "countryside-2","price": 2_200_000, "bedrooms": 4, "bathrooms": 3, "sq_ft": 3600, "year_built": 2011, "agent": "Mira K.",   "listed_days_ago": 35, "view_count": 92,  "amenities": ["garage_2", "garden", "solar"]},
    {"id": "L115", "suburb": "countryside-2","price": 3_500_000, "bedrooms": 5, "bathrooms": 4, "sq_ft": 4200, "year_built": 2024, "agent": "Mira K.",   "listed_days_ago": 1,  "view_count": 500, "amenities": ["pool", "garage_3", "garden", "solar", "fireplace", "balcony"]},
]

REALESTATE_REALTORS: list[dict[str, Any]] = [
    {"name": "Priya N.",  "suburbs": ["suburb-1", "suburb-2"]},
    {"name": "Arjun M.",  "suburbs": ["suburb-3"]},
    {"name": "Mira K.",   "suburbs": ["countryside-1", "countryside-2"]},
]

RE_UTM_CAMPAIGNS = [
    {"utm_source": "google",    "utm_medium": "cpc",   "utm_campaign": "spring-2026-suburb1"},
    {"utm_source": "facebook",  "utm_medium": "social","utm_campaign": "family-homes-q2"},
    {"utm_source": "instagram", "utm_medium": "social","utm_campaign": "luxury-launch"},
    {"utm_source": "google",    "utm_medium": "cpc",   "utm_campaign": "first-time-buyer"},
    {"utm_source": "google",    "utm_medium": "cpc",   "utm_campaign": "investor-suburb3"},
]

RE_REFERRERS = ["https://google.com", "https://facebook.com", "https://instagram.com", ""]

RE_LOCALES: list[tuple[str, str]] = [
    ("en-IN", "Asia/Kolkata"),
    ("en-AU", "Australia/Sydney"),
    ("en-US", "America/Los_Angeles"),
]

RE_MEMBERSHIP_TIERS = ["browse", "saved_search", "premium"]

RE_BASE_URL = "https://realestate-demo.example"

# Extra realistic property details keyed by listing id
_SCHOOL_DISTRICTS = ["Northfield USD", "Hillcrest USD", "Greenwood USD", "Lakeside USD"]
_PARKS = ["Centennial Park", "Riverside Reserve", "Botanic Gardens", "Memorial Park"]

# Per-listing enrichment (walk_score, crime_rate etc.) deterministically mapped
def _listing_extras(listing_id: str, rng: random.Random) -> dict[str, Any]:
    seed = int(listing_id[1:])  # numeric part of L101..L115
    local = random.Random(seed)  # deterministic per listing regardless of user rng
    return {
        "school_district": local.choice(_SCHOOL_DISTRICTS),
        "walk_score": local.randint(45, 98),
        "crime_rate": round(local.uniform(0.8, 4.5), 1),
        "nearest_park_meters": local.randint(120, 2400),
        "est_monthly_emi": round(listing_id and 0, 0),  # computed below
        "lot_size_sqft": local.randint(3000, 12000),
        "parking_spaces": local.randint(1, 3),
    }


def _listing_emi(price: int) -> int:
    """Rough monthly EMI at 8.5% for 30-year loan (80% LTV)."""
    principal = price * 0.80
    r = 0.085 / 12
    n = 360
    emi = principal * r * (1 + r) ** n / ((1 + r) ** n - 1)
    return int(emi)


# ---------------------------------------------------------------------------
# RS-Self cohort data
# ---------------------------------------------------------------------------

RS_DESTINATIONS: list[dict[str, Any]] = [
    {"name": "Amplitude",  "error_codes": ["AMP_INVALID_API_KEY", "AMP_PROJECT_NOT_FOUND"]},
    {"name": "Mixpanel",   "error_codes": ["MIXP_AUTH_FAILED", "MIXP_RATE_LIMITED"]},
    {"name": "Segment",    "error_codes": ["SEG_WRITEKEY_INVALID"]},
    {"name": "Snowflake",  "error_codes": ["SF_CONN_TIMEOUT", "SF_AUTH_FAILED"]},
    {"name": "Postgres",   "error_codes": ["PG_AUTH_FAILED", "PG_HOST_UNREACHABLE"]},
    {"name": "BigQuery",   "error_codes": ["BQ_PERMISSION_DENIED"]},
]

RS_PLANS = ["free", "starter", "pro", "growth", "enterprise"]
RS_ROLES = ["engineer", "analyst", "pm", "designer", "data-eng"]
RS_COMPANY_SIZES = ["1-10", "11-50", "51-200", "201-1000", "1000+"]
RS_SOURCE_TYPES = ["javascript", "server", "mobile"]

RS_BASE_URL = "https://app.rudderstack.com"


# ---------------------------------------------------------------------------
# Helper: build a realestate context dict for a given user config
# ---------------------------------------------------------------------------

@dataclass
class REUserConfig:
    """Per-user configuration for the realestate flow."""
    anonymous_id: str
    user_id: str = ""
    listing_pool: list[dict[str, Any]] = field(default_factory=list)
    campaign: dict[str, str] = field(default_factory=dict)
    referrer: str = "https://google.com"
    locale: str = "en-IN"
    tz: str = "Asia/Kolkata"
    membership_tier: str = "browse"
    session_id: int = 1714978800000


def _re_listings_page(cfg: REUserConfig) -> dict[str, Any]:
    return make_page(
        url=f"{RE_BASE_URL}/listings",
        path="/listings",
        title="All listings",
        referrer=cfg.referrer,
        initial_referrer=cfg.referrer,
        initial_referring_domain=cfg.referrer.split("/")[2] if cfg.referrer else "",
    )


def _re_detail_page(listing_id: str, suburb: str, cfg: REUserConfig) -> dict[str, Any]:
    return make_page(
        url=f"{RE_BASE_URL}/listings/{listing_id}",
        path=f"/listings/{listing_id}",
        title=f"{listing_id} - Park Avenue, {suburb.replace('-', ' ').title()}",
        referrer=f"{RE_BASE_URL}/listings",
        initial_referrer=cfg.referrer,
        initial_referring_domain=cfg.referrer.split("/")[2] if cfg.referrer else "",
    )


def _re_context(page: dict[str, Any], cfg: REUserConfig, traits: dict[str, Any] | None = None) -> dict[str, Any]:
    return make_context(
        page=page,
        campaign=cfg.campaign or None,
        locale=cfg.locale,
        timezone=cfg.tz,
        session_id=cfg.session_id,
        traits=traits,
    )


def _listing_track_props(listing: dict[str, Any], rng: random.Random) -> dict[str, Any]:
    extras = _listing_extras(listing["id"], rng)
    extras["est_monthly_emi"] = _listing_emi(listing["price"])
    return {
        "listing_id":       listing["id"],
        "suburb":           listing["suburb"],
        "price":            listing["price"],
        "bedrooms":         listing["bedrooms"],
        "bathrooms":        listing["bathrooms"],
        "sq_ft":            listing["sq_ft"],
        "year_built":       listing["year_built"],
        "agent":            listing["agent"],
        "listed_days_ago":  listing["listed_days_ago"],
        "view_count":       listing["view_count"],
        "amenities":        listing["amenities"],
        **extras,
    }


# ---------------------------------------------------------------------------
# Realestate script factory
# Returns a list of ScriptSteps for one user given their config.
# ---------------------------------------------------------------------------

def realestate_script(cfg: REUserConfig, rng: random.Random) -> list[ScriptStep]:
    """Build the realestate event flow for one user."""
    # Pick 3 distinct listings from the user's pool
    pool = cfg.listing_pool or REALESTATE_LISTINGS
    listings = rng.sample(pool, min(3, len(pool)))
    l1, l2, l3 = listings[0], listings[1 % len(listings)], listings[2 % len(listings)]

    lp = _re_listings_page(cfg)
    dp = _re_detail_page(l3["id"], l3["suburb"], cfg)

    def step_identify(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        traits = {"membership_tier": cfg.membership_tier}
        ctx = _re_context(lp, cfg, traits=traits)
        return build_identify(anon, uid, traits, ctx)

    def step_page_listings(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        return build_page(anon, uid, _re_context(lp, cfg))

    def step_listing_viewed_1(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        return build_track(anon, uid, "Listing Viewed", _listing_track_props(l1, r), _re_context(lp, cfg))

    def step_filter_applied_1(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        props = {
            "filter_type":   "search",
            "beds_min":      l1["bedrooms"],
            "suburb":        l1["suburb"],
            "price_min":     int(l1["price"] * 0.8),
            "price_max":     int(l1["price"] * 1.3),
            "results_count": r.randint(18, 40),
        }
        return build_track(anon, uid, "Filter Applied", props, _re_context(lp, cfg))

    def step_listing_viewed_2(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        return build_track(anon, uid, "Listing Viewed", _listing_track_props(l2, r), _re_context(lp, cfg))

    def step_listing_viewed_3(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        return build_track(anon, uid, "Listing Viewed", _listing_track_props(l3, r), _re_context(lp, cfg))

    def step_page_detail(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        return build_page(anon, uid, _re_context(dp, cfg))

    def step_filter_applied_2(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        props = {
            "filter_type":   "search",
            "beds_min":      l3["bedrooms"],
            "suburb":        l3["suburb"],
            "price_min":     int(l3["price"] * 0.9),
            "price_max":     int(l3["price"] * 1.1),
            "results_count": r.randint(8, 18),
        }
        return build_track(anon, uid, "Filter Applied", props, _re_context(dp, cfg))

    def step_page_detail_dwell(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        return build_page(anon, uid, _re_context(dp, cfg))

    return [
        ScriptStep(0.0,  step_identify),
        ScriptStep(2.5,  step_page_listings),
        ScriptStep(3.0,  step_listing_viewed_1),
        ScriptStep(4.0,  step_filter_applied_1),
        ScriptStep(4.5,  step_listing_viewed_2),
        ScriptStep(4.0,  step_listing_viewed_3),
        ScriptStep(3.0,  step_page_detail),
        ScriptStep(2.0,  step_filter_applied_2),
        ScriptStep(2.5,  step_page_detail_dwell),
        # idle ≥ 10s starts here — trigger fires after another 10s
    ]


def make_re_user_config(
    anonymous_id: str = "anon_demo-re-001",
    rng: random.Random | None = None,
) -> REUserConfig:
    """Build a REUserConfig, randomizing campaign/locale/tier."""
    r = rng or random.Random()
    campaign = r.choice(RE_UTM_CAMPAIGNS)
    referrer = r.choice(RE_REFERRERS)
    locale, tz = r.choice(RE_LOCALES)
    tier = r.choice(RE_MEMBERSHIP_TIERS)
    session_id = 1714978800000 + r.randint(0, 86400) * 1000
    # Assign a listing pool matching one suburb cluster
    realtor = r.choice(REALESTATE_REALTORS)
    pool = [l for l in REALESTATE_LISTINGS if l["suburb"] in realtor["suburbs"]]
    if len(pool) < 3:
        pool = REALESTATE_LISTINGS  # fallback to full set

    return REUserConfig(
        anonymous_id=anonymous_id,
        listing_pool=pool,
        campaign=campaign,
        referrer=referrer,
        locale=locale,
        tz=tz,
        membership_tier=tier,
        session_id=session_id,
    )


# ---------------------------------------------------------------------------
# RS-Self script factory
# ---------------------------------------------------------------------------

@dataclass
class RSUserConfig:
    """Per-user configuration for the rs-self flow."""
    anonymous_id: str
    user_id: str = ""
    plan: str = "free"
    role: str = "engineer"
    company_size: str = "1-10"


def rs_self_script(cfg: RSUserConfig, rng: random.Random) -> list[ScriptStep]:
    """Build the rs-self onboarding event flow for one user."""
    destination = rng.choice(RS_DESTINATIONS)
    error_code = rng.choice(destination["error_codes"])
    source_type = rng.choice(RS_SOURCE_TYPES)

    signup_page = make_page(
        url=f"{RS_BASE_URL}/signup",
        path="/signup",
        title="RudderStack — Sign up",
    )
    setup_page = make_page(
        url=f"{RS_BASE_URL}/setup/destinations",
        path="/setup/destinations",
        title="Set up your destinations",
    )
    dash_page = make_page(
        url=f"{RS_BASE_URL}/dashboard",
        path="/dashboard",
        title="Dashboard",
    )

    rs_screen = make_screen(width=1512, height=982, density=2, inner_width=1512, inner_height=870)

    def rs_ctx(page: dict[str, Any]) -> dict[str, Any]:
        return make_context(
            page=page,
            locale="en-US",
            timezone="America/Los_Angeles",
            session_id=1714978800000,
            screen=rs_screen,
        )

    def step_identify(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        traits: dict[str, Any] = {
            "plan":         cfg.plan,
            "company":      fk.company(),
            "role":         cfg.role,
            "company_size": cfg.company_size,
        }
        return build_identify(anon, uid, traits, rs_ctx(dash_page))

    def step_account_created(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        return build_track(anon, uid, "Account Created", {
            "plan":   cfg.plan,
            "source": "signup_page",
        }, rs_ctx(signup_page))

    def step_source_created(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        return build_track(anon, uid, "Source Created", {
            "source_type":              source_type,
            "source_name":              fk.bs().title()[:40],
            "elapsed_seconds_in_setup": r.randint(60, 180),
        }, rs_ctx(setup_page))

    def step_dest_setup_error(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        return build_track(anon, uid, "Destination Setup Error", {
            "destination_type":         destination["name"],
            "step":                     "credentials_validation",
            "error_code":               error_code,
            "error_message":            f"Provided credentials rejected by {destination['name']} (HTTP 401)",
            "elapsed_seconds_in_step":  r.randint(60, 200),
        }, rs_ctx(setup_page))

    def step_page_setup(anon: str, uid: str, r: random.Random, fk: Faker) -> dict[str, Any]:
        return build_page(anon, uid, rs_ctx(setup_page))

    return [
        ScriptStep(0.0, step_identify),
        ScriptStep(3.0, step_account_created),
        ScriptStep(3.0, step_source_created),
        ScriptStep(4.0, step_dest_setup_error),
        ScriptStep(2.0, step_page_setup),
        # idle ≥ 15s triggers onboarding_stuck; onboarding_errored fires immediately
    ]


def make_rs_user_config(
    anonymous_id: str = "demo-rs-001",
    rng: random.Random | None = None,
) -> RSUserConfig:
    r = rng or random.Random()
    return RSUserConfig(
        anonymous_id=anonymous_id,
        user_id=anonymous_id,
        plan=r.choice(RS_PLANS),
        role=r.choice(RS_ROLES),
        company_size=r.choice(RS_COMPANY_SIZES),
    )
