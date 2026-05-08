/* eslint-disable */
// Realtime Events AI Triggers — leadership pitch deck (rev 2)
// 6 slides, ~3-4 min slide time + ~4 min demo = 6-8 min total

const pptxgen = require("pptxgenjs");
const React = require("react");
const ReactDOMServer = require("react-dom/server");
const sharp = require("sharp");
const {
  FaUserSecret,
  FaPhoneVolume,
  FaWrench,
  FaInbox,
  FaBoltLightning,
  FaCheck,
  FaXmark,
  FaCircleNodes,
  FaArrowRight,
  FaCircle,
  FaCircleHalfStroke,
  FaCircleDot,
} = require("react-icons/fa6");
const { HiSparkles } = require("react-icons/hi2");

// ─── Color palette ────────────────────────────────────────────────────────────
const C = {
  bg: "0B1020",
  surface: "13182B",
  surfaceLight: "1B2238",
  brand: "7C3AED",
  brandSoft: "A78BFA",     // brighter violet for eyebrows / labels
  accentGreen: "34D399",
  accentGreenSoft: "6EE7B7",
  accentAmber: "F59E0B",
  accentAmberSoft: "FCD34D",
  accentBlue: "60A5FA",
  accentBlueSoft: "93C5FD",
  accentRose: "FB7185",
  accentTeal: "5EEAD4",
  text: "F1F5F9",
  textBright: "F8FAFC",
  textMuted: "CBD5E1",      // brighter than slate-400 for readability
  textSubtle: "94A3B8",
  border: "334155",
  white: "FFFFFF",
};

// ─── Icon helpers ─────────────────────────────────────────────────────────────
function renderIconSvg(IconComponent, color = "#000000", size = 256) {
  return ReactDOMServer.renderToStaticMarkup(
    React.createElement(IconComponent, { color, size: String(size) })
  );
}
async function iconPng(IconComponent, hex, size = 256) {
  const svg = renderIconSvg(IconComponent, "#" + hex, size);
  const png = await sharp(Buffer.from(svg)).png().toBuffer();
  return "image/png;base64," + png.toString("base64");
}

const card_shadow = () => ({
  type: "outer", blur: 12, offset: 3, angle: 90, color: "000000", opacity: 0.35,
});

// ─── Build deck ───────────────────────────────────────────────────────────────
async function build() {
  const pres = new pptxgen();
  pres.layout = "LAYOUT_WIDE"; // 13.3" × 7.5"
  pres.author = "Kuldeep Kumar";
  pres.title = "Realtime Events AI Triggers";

  const ICONS = {
    secret: await iconPng(FaUserSecret, C.accentAmber),
    phone: await iconPng(FaPhoneVolume, C.accentGreen),
    wrench: await iconPng(FaWrench, C.accentBlue),
    inbox: await iconPng(FaInbox, C.brandSoft),
    bolt: await iconPng(FaBoltLightning, C.white),
    boltBrand: await iconPng(FaBoltLightning, C.brand),
    check: await iconPng(FaCheck, C.accentGreen),
    checkBig: await iconPng(FaCheck, C.accentGreen, 384),
    cross: await iconPng(FaXmark, C.accentRose),
    crossDim: await iconPng(FaXmark, C.textSubtle),
    nodes: await iconPng(FaCircleNodes, C.brand),
    arrow: await iconPng(FaArrowRight, C.brandSoft),
    arrowBig: await iconPng(FaArrowRight, C.text, 384),
    circle: await iconPng(FaCircle, C.accentGreen),
    halfCircle: await iconPng(FaCircleHalfStroke, C.accentAmber),
    dot: await iconPng(FaCircleDot, C.brand),
    sparkles: await iconPng(HiSparkles, C.white),
  };

  // ═══════════════════════════════════════════════════════════════════════════
  // SLIDE 1 — Title
  // ═══════════════════════════════════════════════════════════════════════════
  {
    const s = pres.addSlide();
    s.background = { color: C.bg };

    // Brand mark + label
    s.addShape(pres.shapes.RECTANGLE, {
      x: 0.6, y: 0.6, w: 0.32, h: 0.32, fill: { color: C.brand }, line: { color: C.brand },
    });
    s.addText("RUDDERSTACK", {
      x: 1.0, y: 0.55, w: 4, h: 0.4,
      fontSize: 11, fontFace: "Arial", color: C.text, bold: true, charSpacing: 4, margin: 0,
    });

    // Decorative pulse motif (right side) — moved further right + smaller so it
    // never collides with the title text.
    for (let i = 0; i < 5; i++) {
      const size = 0.35 + i * 0.25;
      s.addShape(pres.shapes.OVAL, {
        x: 12.0 - size / 2, y: 4.0 - size / 2, w: size, h: size,
        fill: { color: C.brand, transparency: 70 + i * 5 },
        line: { color: C.brand, width: 1, transparency: 50 },
      });
    }
    s.addShape(pres.shapes.OVAL, {
      x: 11.88, y: 3.88, w: 0.24, h: 0.24, fill: { color: C.brandSoft }, line: { color: C.brandSoft },
    });

    // Title — narrowed width so it ends well before the pulse
    s.addText("Realtime Events AI Triggers", {
      x: 0.6, y: 2.6, w: 10.5, h: 1.3,
      fontSize: 56, fontFace: "Arial", color: C.text, bold: true, margin: 0,
    });

    // Subtitle — brighter contrast
    s.addText(
      [
        { text: "From event streams ", options: { color: C.text } },
        { text: "→", options: { color: C.brandSoft, bold: true } },
        { text: " in-the-moment outcomes", options: { color: C.text } },
      ],
      { x: 0.6, y: 3.95, w: 10.5, h: 0.65, fontSize: 24, fontFace: "Arial", margin: 0 }
    );

    // One-liner positioning the pitch
    s.addText(
      "A real-time, AI-personalized action layer for the streaming events you already collect.",
      { x: 0.6, y: 4.7, w: 10.5, h: 0.55, fontSize: 14, fontFace: "Arial", color: C.textMuted, italic: true, margin: 0 }
    );

    // Tag pill + byline pulled up so vertical rhythm is balanced
    s.addShape(pres.shapes.RECTANGLE, {
      x: 0.6, y: 6.55, w: 1.7, h: 0.42, fill: { color: C.surface }, line: { color: C.brand, width: 1 },
    });
    s.addText("HACKATHON 2026", {
      x: 0.6, y: 6.55, w: 1.7, h: 0.42,
      fontSize: 11, fontFace: "Arial", color: C.brandSoft, bold: true, charSpacing: 2,
      align: "center", valign: "middle", margin: 0,
    });
    s.addText("Kuldeep Kumar  ·  RudderStack", {
      x: 2.5, y: 6.55, w: 5, h: 0.42, fontSize: 13, fontFace: "Arial", color: C.textMuted,
      valign: "middle", margin: 0,
    });
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // SLIDE 2 — The Gap
  // ═══════════════════════════════════════════════════════════════════════════
  {
    const s = pres.addSlide();
    s.background = { color: C.bg };

    s.addText("THE GAP", {
      x: 0.6, y: 0.4, w: 6, h: 0.35,
      fontSize: 11, fontFace: "Arial", color: C.brandSoft, bold: true, charSpacing: 4, margin: 0,
    });
    s.addText("Customers stream events. They can't reason over the last few minutes of them.", {
      x: 0.6, y: 0.85, w: 12.2, h: 1.1,
      fontSize: 28, fontFace: "Arial", color: C.text, bold: true, margin: 0,
    });
    s.addText(
      "Real-time pipelines deliver events. Warehouse audiences segment customers over weeks. There's nothing in between that holds short-term memory of a session, watches for live patterns, and acts in seconds — so teams stitch it together with brittle scripts and miss the moment.",
      {
        x: 0.6, y: 2.05, w: 12.2, h: 0.85,
        fontSize: 14, fontFace: "Arial", color: C.textMuted, margin: 0,
      }
    );

    // Three pain cards with persona-differentiated accent colors
    const cards = [
      {
        x: 0.6,
        role: "MARKETING",
        roleColor: C.accentRose,
        accentColor: C.accentRose,
        quote: "We see anonymous high-intent visitors leave every day. By the time tomorrow's report runs, they're gone.",
        cost: "Missed conversions you can never re-attribute.",
      },
      {
        x: 5.0,
        role: "CUSTOMER SUCCESS",
        roleColor: C.accentAmber,
        accentColor: C.accentAmber,
        quote: "A whale browsed our pricing page yesterday. We found out at next week's QBR — too late to call.",
        cost: "Deals sized in the room, not in real time.",
      },
      {
        x: 9.4,
        role: "ONBOARDING",
        roleColor: C.accentTeal,
        accentColor: C.accentTeal,
        quote: "Customers churn at the destination-connect step. Every error needs a different fix; we can't follow up on each one.",
        cost: "Drop-off the moment users hit friction.",
      },
    ];
    for (const c of cards) {
      // Card background — taller now to give quote + cost natural breathing
      // room without an empty middle band; bigger quote text fills most of it.
      const cardY = 3.1;
      const cardH = 3.7;
      s.addShape(pres.shapes.RECTANGLE, {
        x: c.x, y: cardY, w: 3.3, h: cardH,
        fill: { color: C.surface }, line: { color: C.border, width: 1 },
        shadow: card_shadow(),
      });
      // Top accent bar
      s.addShape(pres.shapes.RECTANGLE, {
        x: c.x, y: cardY, w: 3.3, h: 0.08,
        fill: { color: c.accentColor }, line: { color: c.accentColor },
      });
      // Role label
      s.addText(c.role, {
        x: c.x + 0.3, y: cardY + 0.25, w: 2.7, h: 0.32,
        fontSize: 11, fontFace: "Arial", color: c.roleColor, bold: true, charSpacing: 2, margin: 0,
      });
      // Quote — bigger so it fills the middle naturally; opening glyph inline
      s.addText(
        [
          { text: "“", options: { color: c.accentColor, fontFace: "Georgia", fontSize: 36, italic: false } },
          { text: c.quote + "”", options: { color: C.textBright, fontSize: 16, italic: true } },
        ],
        {
          x: c.x + 0.3, y: cardY + 0.7, w: 2.75, h: 2.0,
          fontFace: "Arial", margin: 0, valign: "top",
        }
      );
      // Cost block at the bottom of the card
      const costY = cardY + cardH - 0.95;
      s.addShape(pres.shapes.LINE, {
        x: c.x + 0.3, y: costY, w: 2.7, h: 0,
        line: { color: C.border, width: 0.75 },
      });
      s.addText("Cost", {
        x: c.x + 0.3, y: costY + 0.08, w: 2.7, h: 0.25,
        fontSize: 9, fontFace: "Arial", color: C.textSubtle, bold: true, charSpacing: 2, margin: 0,
      });
      s.addText(c.cost, {
        x: c.x + 0.3, y: costY + 0.35, w: 2.7, h: 0.5,
        fontSize: 12, fontFace: "Arial", color: C.textMuted, margin: 0,
      });
    }
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // SLIDE 3 — Use cases
  // ═══════════════════════════════════════════════════════════════════════════
  {
    const s = pres.addSlide();
    s.background = { color: C.bg };

    s.addText("USE CASES", {
      x: 0.6, y: 0.4, w: 6, h: 0.35,
      fontSize: 11, fontFace: "Arial", color: C.brandSoft, bold: true, charSpacing: 4, margin: 0,
    });
    s.addText("Six moments worth catching. One engine catches them all.", {
      x: 0.6, y: 0.85, w: 12.2, h: 0.8,
      fontSize: 30, fontFace: "Arial", color: C.text, bold: true, margin: 0,
    });

    // 3×2 grid (6 cards). No invented %ages — capability claims only.
    const cardW = 4.05;
    const cardH = 2.45;
    const gapX = 0.10;
    const startX = 0.55;
    const row1Y = 1.95;
    const row2Y = 4.50;
    const cards = [
      {
        col: 0, row: 0,
        icon: ICONS.secret, accent: C.accentRose,
        title: "Capture anonymous high-intent",
        body: "Visitor narrows filters, opens 3 listings, idles. Fire an in-app banner offering a tour or chat — before they leave the site, while they're still on it.",
        capability: "In-app capture moment",
        pain: "Marketing pain",
      },
      {
        col: 1, row: 0,
        icon: ICONS.phone, accent: C.accentAmber,
        title: "Alert sales / CS on known engagement",
        body: "Logged-in customer revisits pricing. Slack the right account owner with full context: name, ARR, prior interactions, the page they're dwelling on.",
        capability: "Real-time human handoff",
        pain: "Customer Success pain",
      },
      {
        col: 2, row: 0,
        icon: ICONS.wrench, accent: C.accentTeal,
        title: "Rescue stuck onboarding errors",
        body: "Setup error fires. We send a personalized fix email referencing the exact error code, the destination type, and the customer's stack — not a generic FAQ.",
        capability: "Personalized per error",
        pain: "Onboarding pain",
      },
      {
        col: 0, row: 1,
        icon: ICONS.inbox, accent: C.accentTeal,
        title: "Re-engage stalled multi-step flows",
        body: "Customer created a source but never connected a destination. After idle, an email referencing their progress and tech stack — not a generic nudge.",
        capability: "Mid-funnel context-aware",
        pain: "Onboarding pain",
      },
      {
        col: 1, row: 1,
        icon: ICONS.nodes, accent: C.brand,
        title: "Synthetic events from event patterns",
        body: "Emit a new derived event when a pattern matches — e.g., \"high_intent_session\" when 3 listings + a filter + 8s idle. Feeds back into the same SDK / warehouse.",
        capability: "Custom hooks on patterns",
        pain: "Platform extensibility",
      },
      {
        col: 2, row: 1,
        icon: ICONS.dot, accent: C.accentBlue,
        title: "Temporal context API",
        body: "Ask \"what did this user do in the last 10 minutes?\" — short-term memory exposed as a query, so agents and apps can reason over live behavior, not yesterday's batch.",
        capability: "Live session lookup",
        pain: "Agent / app context",
      },
    ];
    for (const c of cards) {
      const x = startX + c.col * (cardW + gapX);
      const y = c.row === 0 ? row1Y : row2Y;
      s.addShape(pres.shapes.RECTANGLE, {
        x, y, w: cardW, h: cardH,
        fill: { color: C.surface }, line: { color: C.border, width: 1 },
        shadow: card_shadow(),
      });
      s.addShape(pres.shapes.RECTANGLE, {
        x, y, w: 0.07, h: cardH, fill: { color: c.accent }, line: { color: c.accent },
      });
      s.addImage({ data: c.icon, x: x + 0.25, y: y + 0.25, w: 0.42, h: 0.42 });
      s.addText(c.title, {
        x: x + 0.85, y: y + 0.18, w: cardW - 0.95, h: 0.6,
        fontSize: 14, fontFace: "Arial", color: C.text, bold: true, margin: 0, valign: "middle",
      });
      s.addText(c.body, {
        x: x + 0.25, y: y + 0.85, w: cardW - 0.4, h: 1.15,
        fontSize: 11, fontFace: "Arial", color: C.textMuted, margin: 0,
      });
      // Bottom row: capability (left) + pain mapping (right)
      s.addText(c.capability, {
        x: x + 0.25, y: y + 2.05, w: cardW * 0.55, h: 0.3,
        fontSize: 11, fontFace: "Arial", color: c.accent, bold: true, margin: 0,
      });
      s.addText("→ " + c.pain, {
        x: x + cardW * 0.55 + 0.15, y: y + 2.05, w: cardW * 0.40 - 0.2, h: 0.3,
        fontSize: 10, fontFace: "Arial", color: C.textMuted, italic: true, align: "right", margin: 0,
      });
    }

    // Footer with proper gap
    s.addShape(pres.shapes.RECTANGLE, {
      x: 0.6, y: 7.15, w: 12.1, h: 0.32,
      fill: { color: C.surfaceLight }, line: { color: C.surfaceLight },
    });
    s.addText(
      [
        { text: "+ ", options: { color: C.brandSoft, bold: true } },
        {
          text: "All single-event \"webhook on event X\" patterns you'd otherwise wire by hand in Transformer — same engine, no new plumbing.",
          options: { color: C.textMuted },
        },
      ],
      { x: 0.85, y: 7.15, w: 11.6, h: 0.32, fontSize: 11, fontFace: "Arial", italic: true, valign: "middle", margin: 0 }
    );
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // SLIDE 4 — How it works
  // ═══════════════════════════════════════════════════════════════════════════
  {
    const s = pres.addSlide();
    s.background = { color: C.bg };

    s.addText("HOW IT WORKS", {
      x: 0.6, y: 0.4, w: 6, h: 0.35,
      fontSize: 11, fontFace: "Arial", color: C.brandSoft, bold: true, charSpacing: 4, margin: 0,
    });
    s.addText("A 5-stage pipeline. Stateful. Plug-in.", {
      x: 0.6, y: 0.85, w: 12.2, h: 0.8,
      fontSize: 30, fontFace: "Arial", color: C.text, bold: true, margin: 0,
    });

    // Pipeline blocks — equal widths, 1-line subs, 5 distinct colors
    const blockY = 2.2;
    const blockW = 2.10;
    const arrowW = 0.20;
    const startX = 0.65;
    const blocks = [
      { title: "Events",         sub: "Pulsar / SDK",      color: C.accentBlue },
      { title: "Window",         sub: "Per-user state",    color: C.accentGreen },
      { title: "Rules",          sub: "DSL: all/any/not",  color: C.brand },
      { title: "Enrichment+LLM", sub: "Profiles + canned", color: C.accentAmber },
      { title: "Action",         sub: "Slack / Email / API", color: C.accentRose },
    ];
    for (let i = 0; i < blocks.length; i++) {
      const b = blocks[i];
      const x = startX + i * (blockW + arrowW);
      s.addShape(pres.shapes.RECTANGLE, {
        x, y: blockY, w: blockW, h: 1.7,
        fill: { color: C.surface }, line: { color: b.color, width: 2 },
        shadow: card_shadow(),
      });
      s.addText(b.title, {
        x: x + 0.05, y: blockY + 0.35, w: blockW - 0.1, h: 0.5,
        fontSize: 14, fontFace: "Arial", color: C.text, bold: true, align: "center", margin: 0,
      });
      s.addText(b.sub, {
        x: x + 0.05, y: blockY + 0.95, w: blockW - 0.1, h: 0.4,
        fontSize: 11, fontFace: "Arial", color: b.color, align: "center", margin: 0,
      });
      // Arrow between blocks — bigger, brighter
      if (i < blocks.length - 1) {
        s.addImage({
          data: ICONS.arrowBig,
          x: x + blockW - 0.02, y: blockY + 0.78, w: 0.30, h: 0.30,
        });
      }
    }

    // Three highlight callouts — distributed evenly under the 5-block row
    // Total available: 0.6 to 12.7 = 12.1" wide. 3 callouts, each ~3.7" wide
    const callouts = [
      {
        x: 0.65, w: 3.85,
        head: "12 composable predicates",
        body: "window.idle_seconds, traits.known, has_event_name, AND/OR/NOT — fits any pattern.",
      },
      {
        x: 4.75, w: 3.85,
        head: "Templated AI actions",
        body: "Slack and email fill {{trait.first_name}} and {{window.last_listing}} at fire time.",
      },
      {
        x: 8.85, w: 3.85,
        head: "Pluggable destinations",
        body: "Slack + email today. Swap LLM_MODE=live, plug your destination registry tomorrow.",
      },
    ];
    for (const co of callouts) {
      s.addShape(pres.shapes.OVAL, {
        x: co.x, y: 4.4, w: 0.5, h: 0.5,
        fill: { color: C.brand }, line: { color: C.brand },
      });
      s.addImage({ data: ICONS.checkBig, x: co.x + 0.13, y: 4.53, w: 0.24, h: 0.24 });
      s.addText(co.head, {
        x: co.x + 0.6, y: 4.38, w: co.w - 0.6, h: 0.42,
        fontSize: 14, fontFace: "Arial", color: C.text, bold: true, margin: 0,
      });
      s.addText(co.body, {
        x: co.x + 0.6, y: 4.82, w: co.w - 0.6, h: 1.3,
        fontSize: 11, fontFace: "Arial", color: C.textMuted, margin: 0,
      });
    }

    // DEMO CTA bar — bigger bolt icon
    s.addShape(pres.shapes.RECTANGLE, {
      x: 0.6, y: 6.55, w: 12.1, h: 0.85,
      fill: { color: C.brand }, line: { color: C.brand },
      shadow: card_shadow(),
    });
    s.addImage({ data: ICONS.bolt, x: 0.95, y: 6.7, w: 0.55, h: 0.55 });
    s.addText("LIVE DEMO  →  3 concurrent visitors, two Slack pings, one personalized email, ~30 seconds", {
      x: 1.7, y: 6.55, w: 10.9, h: 0.85,
      fontSize: 16, fontFace: "Arial", color: C.white, bold: true, valign: "middle", margin: 0,
    });
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // SLIDE 5 — Competitive landscape
  // ═══════════════════════════════════════════════════════════════════════════
  {
    const s = pres.addSlide();
    s.background = { color: C.bg };

    s.addText("WHAT EXISTS TODAY", {
      x: 0.6, y: 0.4, w: 6, h: 0.35,
      fontSize: 11, fontFace: "Arial", color: C.brandSoft, bold: true, charSpacing: 4, margin: 0,
    });
    s.addText("No one composes all six. We're the gap-filler.", {
      x: 0.6, y: 0.85, w: 12.2, h: 0.8,
      fontSize: 30, fontFace: "Arial", color: C.text, bold: true, margin: 0,
    });

    // Comparison table — abbreviated competitor names so all headers fit on one line
    const headers = ["Capability", "Segment", "Tealium", "Customer.io", "Braze", "This"];
    const rows = [
      ["Single-event webhooks",     "yes", "yes",     "yes",     "yes",     "yes"],
      ["Stateful windowing",        "no",  "partial", "yes",     "yes",     "yes"],
      ["Idle / dwell predicates",   "no",  "no",      "partial", "partial", "yes"],
      ["Anonymous-intent action",   "no",  "partial", "no",      "no",      "yes"],
      ["LLM-personalized content",  "no",  "no",      "no",      "no",      "yes"],
      ["Pluggable destinations",    "yes", "yes",     "no",      "no",      "yes"],
    ];

    const tableX = 0.6;
    const tableY = 2.2;
    // Tighter first column → more room for comparison columns
    const colW = [3.05, 1.65, 1.65, 1.85, 1.65, 2.25];
    const rowH = 0.55;

    // Header row
    let cx = tableX;
    for (let i = 0; i < headers.length; i++) {
      const isOurs = i === headers.length - 1;
      s.addShape(pres.shapes.RECTANGLE, {
        x: cx, y: tableY, w: colW[i], h: rowH,
        fill: { color: isOurs ? C.brand : C.surfaceLight },
        line: { color: isOurs ? C.brand : C.surfaceLight },
      });
      s.addText(headers[i], {
        x: cx + 0.1, y: tableY, w: colW[i] - 0.2, h: rowH,
        fontSize: 13, fontFace: "Arial", color: isOurs ? C.white : C.text,
        bold: true, align: i === 0 ? "left" : "center", valign: "middle", margin: 0,
      });
      cx += colW[i];
    }

    // Data rows
    for (let r = 0; r < rows.length; r++) {
      const rowY = tableY + rowH + r * rowH;
      const rowFill = r % 2 === 0 ? C.surface : C.bg;
      cx = tableX;
      for (let i = 0; i < rows[r].length; i++) {
        const isOurs = i === rows[r].length - 1;
        s.addShape(pres.shapes.RECTANGLE, {
          x: cx, y: rowY, w: colW[i], h: rowH,
          fill: { color: isOurs ? C.surfaceLight : rowFill },
          line: { color: C.border, width: 0.5 },
        });
        if (i === 0) {
          s.addText(rows[r][i], {
            x: cx + 0.2, y: rowY, w: colW[i] - 0.3, h: rowH,
            fontSize: 13, fontFace: "Arial", color: C.text, valign: "middle", margin: 0,
          });
        } else {
          const cell = rows[r][i];
          if (cell === "yes") {
            s.addImage({
              data: ICONS.circle,
              x: cx + colW[i] / 2 - 0.13, y: rowY + 0.13, w: 0.28, h: 0.28,
            });
          } else if (cell === "no") {
            s.addImage({
              data: ICONS.crossDim,
              x: cx + colW[i] / 2 - 0.13, y: rowY + 0.13, w: 0.28, h: 0.28,
            });
          } else {
            // "partial" → half-circle icon for visual consistency
            s.addImage({
              data: ICONS.halfCircle,
              x: cx + colW[i] / 2 - 0.13, y: rowY + 0.13, w: 0.28, h: 0.28,
            });
          }
        }
        cx += colW[i];
      }
    }

    // Legend
    s.addShape(pres.shapes.LINE, {
      x: tableX, y: 6.05, w: 12.1, h: 0,
      line: { color: C.border, width: 0.5 },
    });
    const legend = [
      { x: 0.6, icon: ICONS.circle, label: "Full" },
      { x: 1.6, icon: ICONS.halfCircle, label: "Partial" },
      { x: 2.7, icon: ICONS.crossDim, label: "Not supported" },
    ];
    for (const l of legend) {
      s.addImage({ data: l.icon, x: l.x, y: 6.18, w: 0.22, h: 0.22 });
      s.addText(l.label, {
        x: l.x + 0.3, y: 6.13, w: 1.5, h: 0.32,
        fontSize: 11, fontFace: "Arial", color: C.textMuted, margin: 0, valign: "middle",
      });
    }

    // Take-away with proper gutter
    s.addText(
      [
        { text: "Bottom line:  ", options: { color: C.brandSoft, bold: true } },
        {
          text: "Other tools each solve one or two of these. None compose all six into a single substrate — which is why customers DIY this with cron jobs and Looker.",
          options: { color: C.text },
        },
      ],
      { x: 0.6, y: 6.7, w: 12.1, h: 0.6, fontSize: 13, fontFace: "Arial", italic: true, margin: 0 }
    );
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // SLIDE 6 — Production roadmap & close
  // ═══════════════════════════════════════════════════════════════════════════
  {
    const s = pres.addSlide();
    s.background = { color: C.bg };

    s.addText("WHAT'S NEXT", {
      x: 0.6, y: 0.4, w: 6, h: 0.35,
      fontSize: 11, fontFace: "Arial", color: C.brandSoft, bold: true, charSpacing: 4, margin: 0,
    });
    s.addText("Hackathon code today. Production substrate tomorrow.", {
      x: 0.6, y: 0.85, w: 12.2, h: 0.8,
      fontSize: 30, fontFace: "Arial", color: C.text, bold: true, margin: 0,
    });

    // ── TODAY column ──
    s.addShape(pres.shapes.RECTANGLE, {
      x: 0.6, y: 2.0, w: 6.0, h: 4.45,
      fill: { color: C.surface }, line: { color: C.border, width: 1 }, shadow: card_shadow(),
    });
    s.addShape(pres.shapes.RECTANGLE, {
      x: 0.6, y: 2.0, w: 6.0, h: 0.45, fill: { color: C.accentGreen }, line: { color: C.accentGreen },
    });
    s.addText("SHIPPED THIS WEEK", {
      x: 0.85, y: 2.0, w: 5.5, h: 0.45,
      fontSize: 12, fontFace: "Arial", color: "0B1020", bold: true, charSpacing: 3, valign: "middle", margin: 0,
    });
    const todayItems = [
      "Rules engine: 12 predicates, AND/OR/NOT, hot-reloadable from YAML",
      "Window manager: idle/dwell/counters, sharded, 15-min retention",
      "Cooldowns + dedup so the same realtor isn't paged twice",
      "Mock Activation API (real-shape compatible — single flag swap)",
      "Templated canned LLM responses + live local-agent fallback",
      "Slack webhook + email outbox + onboarding wizard + dashboard",
    ];
    let ty = 2.7;
    for (const it of todayItems) {
      s.addImage({ data: ICONS.checkBig, x: 0.9, y: ty + 0.07, w: 0.24, h: 0.24 });
      s.addText(it, {
        x: 1.25, y: ty - 0.02, w: 5.2, h: 0.55,
        fontSize: 12, fontFace: "Arial", color: C.text, margin: 0, valign: "top",
      });
      ty += 0.6;
    }

    // ── PRODUCTION column ──
    s.addShape(pres.shapes.RECTANGLE, {
      x: 6.85, y: 2.0, w: 6.0, h: 4.45,
      fill: { color: C.surface }, line: { color: C.border, width: 1 }, shadow: card_shadow(),
    });
    s.addShape(pres.shapes.RECTANGLE, {
      x: 6.85, y: 2.0, w: 6.0, h: 0.45, fill: { color: C.brand }, line: { color: C.brand },
    });
    s.addText("PRODUCTION ROADMAP", {
      x: 7.10, y: 2.0, w: 5.5, h: 0.45,
      fontSize: 12, fontFace: "Arial", color: C.white, bold: true, charSpacing: 3, valign: "middle", margin: 0,
    });
    // Right-column items trimmed to roughly match left column line counts
    const prodItems = [
      "Multi-tenant + horizontal scaling (consumer per tenant)",
      "Live LLM mode — BYOK or platform-managed, with caching",
      "Real Activation API integration (already wire-compatible)",
      "ClickHouse for events archive + replay",
      "CEL or Drools for richer rule expressions (sandbox-safe)",
      "Pluggable destination registry: any webhook or in-app SDK",
    ];
    ty = 2.7;
    for (const it of prodItems) {
      // Use same check style as left column for consistency, in brand color
      s.addShape(pres.shapes.OVAL, {
        x: 7.15, y: ty + 0.07, w: 0.24, h: 0.24,
        fill: { color: C.brand }, line: { color: C.brand },
      });
      s.addImage({ data: ICONS.arrowBig, x: 7.20, y: ty + 0.13, w: 0.14, h: 0.14 });
      s.addText(it, {
        x: 7.50, y: ty - 0.02, w: 5.2, h: 0.55,
        fontSize: 12, fontFace: "Arial", color: C.text, margin: 0, valign: "top",
      });
      ty += 0.6;
    }

    // Closing CTA — pulled down with a clear gutter, larger sparkle icon
    s.addShape(pres.shapes.RECTANGLE, {
      x: 0.6, y: 6.7, w: 12.25, h: 0.65,
      fill: { color: C.brand }, line: { color: C.brand },
    });
    s.addImage({ data: ICONS.sparkles, x: 0.85, y: 6.78, w: 0.5, h: 0.5 });
    s.addText("The AI-native automation substrate. Same engine, every event, every customer.", {
      x: 1.5, y: 6.7, w: 11.3, h: 0.65,
      fontSize: 16, fontFace: "Arial", color: C.white, bold: true, valign: "middle", margin: 0,
    });
  }

  const outPath = process.argv[2] || "/tmp/pitch-deck/realtime-events-ai-triggers.pptx";
  await pres.writeFile({ fileName: outPath });
  console.log("WROTE:", outPath);
}

build().catch((e) => {
  console.error(e);
  process.exit(1);
});
