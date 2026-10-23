---
name: Local backup
description: Household binder UI for trustworthy local backups on this machine.
colors:
  paper: "#f2efe8"
  ink: "#1e2430"
  rail: "#1e2430"
  rail-muted: "#c8cdd6"
  accent: "#0f5c57"
  accent-hover: "#0c4a46"
  brass: "#c4a574"
  ok: "#1f6b3a"
  bad: "#8c2f2f"
  muted: "#5c6570"
  card: "#ffffff"
  line: "#ddd6c8"
  focus: "#1a4f8b"
  field-border: "#cfc7b8"
  selection: "#c5e4df"
  secondary-hover: "#e7f2ef"
  pre-bg: "#ebe6dc"
  bar-track: "#e3ddd2"
typography:
  headline:
    fontFamily: "system-ui, -apple-system, \"Segoe UI\", sans-serif"
    fontSize: "1.65rem"
    fontWeight: 650
    lineHeight: 1.45
    letterSpacing: "-0.02em"
  title:
    fontFamily: "system-ui, -apple-system, \"Segoe UI\", sans-serif"
    fontSize: "1.05rem"
    fontWeight: 650
    lineHeight: 1.45
  body:
    fontFamily: "system-ui, -apple-system, \"Segoe UI\", sans-serif"
    fontSize: "16px"
    fontWeight: 400
    lineHeight: 1.45
  label:
    fontFamily: "system-ui, -apple-system, \"Segoe UI\", sans-serif"
    fontSize: "0.8rem"
    fontWeight: 650
    letterSpacing: "0.03em"
  brand:
    fontFamily: "system-ui, -apple-system, \"Segoe UI\", sans-serif"
    fontSize: "1.05rem"
    fontWeight: 650
    letterSpacing: "-0.01em"
rounded:
  xs: "3px"
  sm: "6px"
  md: "8px"
spacing:
  xs: "0.35rem"
  sm: "0.5rem"
  md: "0.85rem"
  lg: "1.25rem"
  xl: "1.75rem"
  rail-width: "13.5rem"
  content-max: "44rem"
  lede-max: "38rem"
components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "#ffffff"
    rounded: "{rounded.sm}"
    padding: "0.65rem 0.9rem"
  button-primary-hover:
    backgroundColor: "{colors.accent-hover}"
    textColor: "#ffffff"
  button-secondary:
    backgroundColor: "transparent"
    textColor: "{colors.accent}"
    rounded: "{rounded.sm}"
    padding: "0.65rem 0.9rem"
  button-secondary-hover:
    backgroundColor: "{colors.secondary-hover}"
    textColor: "{colors.accent}"
  tab-cta:
    backgroundColor: "{colors.accent}"
    textColor: "#ffffff"
    rounded: "{rounded.sm}"
    padding: "0.75rem 0.85rem"
  input:
    backgroundColor: "#ffffff"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
    padding: "0.55rem 0.65rem"
  card:
    backgroundColor: "{colors.card}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "1.15rem 1.25rem"
  trust-ok:
    backgroundColor: "transparent"
    textColor: "{colors.ok}"
    rounded: "{rounded.xs}"
    padding: "0.15rem 0.45rem"
---

# Design System: Local backup

## Overview

**Creative North Star: "Household Binder"**

The localhost UI reads like a labeled household binder: a dark graphite spine (rail) with Setup and Sites as tabbed sections, and a warm paper page for the active job. It is dense, practical, and quiet — forms for decisions, a calm list for trust — not a cloud status dashboard.

Typography stays on the system UI sans stack so the surface feels like a local tool, not a marketing site. Depth comes from paper-on-rail contrast, a brass binder edge, and hairline paper lines; shadows stay whisper-thin. Accent teal is reserved for primary actions and links so it stays rare and decisive.

**Key Characteristics:**
- Graphite left rail with brass inset edge; paper main pane
- Setup / Sites tabs; primary CTA pinned in the rail
- Dense labeled forms in white cards with brass section rules
- Quiet site list: one open row, trust chip at a glance
- System UI sans; weight 650 for brand, titles, and active chrome

## Colors

Warm paper, graphite chrome, one teal accent, and a brass binder cue. Status greens and reds are text/border signals, not fills that dominate the page.

### Primary
- **Binder Teal** (`accent`): Primary buttons, rail CTA, links, progress fill, and notice borders. The action color.
- **Deep Teal** (`accent-hover`): Hover for primary buttons and CTA.

### Secondary
- **Binder Brass** (`brass`): Rail inset edge, active tab accent stripe, CTA inset ring, and card `h2` underlines. Structural binder metal, not a fill flood.

### Neutral
- **Warm Paper** (`paper`): Page and active-tab background.
- **Graphite Ink** (`ink` / `rail`): Body text and rail ground (same hex).
- **Rail Mist** (`rail-muted`): Inactive tab and brand subtitle on the rail.
- **Slate Mute** (`muted`): Lede, meta, hints, tools line.
- **Card White** (`card`): Form and section surfaces on paper.
- **Paper Line** (`line`): Card borders and list dividers.
- **Field Edge** (`field-border`): Input/select/textarea strokes.
- **Focus Blue** (`focus`): `:focus-visible` outline only.

### Status
- **Restore Green** (`ok`): Success copy and healthy trust chips.
- **Fail Red** (`bad`): Errors, failed trust chips, bad notices.

### Named Rules
**The One Accent Rule.** Teal is for decisive actions and links. Brass marks binder structure (edge, active tab, section rule) — do not swap their jobs.

**The Paper Ground Rule.** The main pane stays paper; white is for cards and fields sitting on that paper, not a full-bleed white app shell.

## Typography

**Display Font:** none (no marketing display face)
**Body Font:** system-ui, -apple-system, "Segoe UI", sans-serif
**Label/Mono Font:** same stack (no separate mono face in the UI)

**Character:** Practical system UI sans at 16px / 1.45. Hierarchy is weight and size, not a second family. Semibold-leaning 650 marks brand, headings, active tabs, and trust chips.

### Hierarchy
- **Headline** (650, 1.65rem, −0.02em): Page `h1`.
- **Title** (650, 1.05rem): Card `h2` and brand wordmark size.
- **Body** (400, 16px / 1.45): Forms, list copy, lede (lede max-width 38rem).
- **Label** (650, 0.8rem, 0.03em): Trust chips.
- **Hint** (400, 0.88rem, muted): Supporting hints under forms.
- **Brand subtitle** (400, 0.75rem, rail-muted): “on this machine” under the wordmark.

### Named Rules
**The System Face Rule.** Stay on the system UI stack. Do not introduce Inter, Roboto, or a display serif for this surface.

## Layout

Binder shell: sticky rail (13.5rem) + fluid content column. Content pads 1.75rem 2rem 2.5rem with max-width 44rem. Forms are full-width inside cards; action rows wrap with 0.5rem gaps. Field stack uses 0.35rem vertical rhythm between label/control.

Below 720px the rail becomes a top binder strip (brass bottom edge), tabs sit in a horizontal row, and the CTA shares the brand row. Content padding tightens to 1.25rem 1rem 2rem.

### Named Rules
**The One Job Pane Rule.** The main column shows the active tab’s single job — Setup walkthrough or Sites trust list — not a multi-widget dashboard.

## Elevation & Depth

Mostly tonal and structural: graphite rail against paper, brass inset edge, 1px paper lines. Shadows are minimal paper lift, not floating panels.

### Shadow Vocabulary
- **Card lift** (`box-shadow: 0 1px 0 rgba(30, 36, 48, 0.04)`): Cards at rest.
- **Active tab** (`inset 0 1px 0 #fff8, 0 1px 0 rgba(30, 36, 48, 0.06)`): Paper tab peeling onto the page (desktop).
- **Rail brass edge** (`inset -6px 0 0` brass; mobile `inset 0 -4px 0`): Binder spine cue.
- **CTA brass ring** (`inset 0 0 0 1px` brass): Primary rail CTA only.

### Named Rules
**The Flat Paper Rule.** No large drop shadows or glow stacks. Depth is rail/paper/brass, not floating chrome.

## Shapes

Soft tool radii: 6px on controls, tabs, and buttons; 8px on cards and notices; 3px on trust chips and progress track. Active desktop tabs use left-only radius (`6px 0 0 6px`) so they meet the paper pane; mobile tabs flip to top radius with a brass bottom stripe.

## Components

### Buttons
- **Shape:** Gently rounded (6px).
- **Primary:** Teal fill, white text, 0.65rem 0.9rem, weight 600; hover deep teal.
- **Secondary:** Transparent with teal border and text; hover wash `secondary-hover`.
- **Rail CTA:** Teal fill, brass inset ring, weight 650; pinned to rail bottom (desktop).
- **Focus:** 2px focus blue outline, 2px offset.

### Cards / Containers
- **Corner Style:** 8px.
- **Background:** Card white on paper; 1px paper line.
- **Shadow Strategy:** Hairline card lift only.
- **Border:** Section titles get a brass bottom rule (not a heavy divider stack).
- **Internal Padding:** 1.15rem 1.25rem.

### Inputs / Fields
- **Style:** White field, 1px field-edge border, 6px radius, 0.55rem 0.65rem padding.
- **Focus:** Shared focus-visible ring (focus blue).
- **Density:** Labels and controls stack full width; checkboxes sit inline with 0.5rem gap.

### Navigation
- **Rail:** Graphite, sticky, brass left-edge inset; brand + Setup/Sites + CTA.
- **Tabs:** Rail-muted by default; white text on hover; active tab is paper + ink with brass left stripe.
- **Mobile:** Top strip; tabs horizontal; active uses brass bottom stripe.

### Site list (signature)
Quiet binder index: hairline-separated rows, one `<details>` open by default (first or running). Name weight 650; meta muted; trust chip right-aligned (ok / muted never / bad). Expanded body holds last-run facts and an inline “Open site” button.

### Progress bar
3px-radius track (`bar-track`); accent fill; width eases 180ms; indeterminate slides when percent unknown.

### Notices
Teal-bordered wash for status; bad variant uses fail red on a light red wash. Used for form errors and run state — not decorative callouts.

## Do's and Don'ts

### Do:
- **Do** keep the graphite rail + paper pane + brass edge as the shell chrome.
- **Do** land first-run attention on Setup and the “Set up a backup” CTA.
- **Do** treat local-only as a complete backup in copy and hierarchy; destinations stay optional.
- **Do** use the quiet site list with trust chips for restore confidence after backups exist.
- **Do** keep forms dense, labeled, and card-contained.

### Don't:
- **Don't** restyle this as a cloud ops dashboard of status cards, KPI strips, or pill clusters.
- **Don't** present WordPress (or any CMS) as the product default — site types are addons; unmatched paths stay files-only.
- **Don't** invent marketing claims, testimonials, or logos not in PRODUCT.md evidence.
- **Don't** flood brass or teal; brass is structure, teal is action.
- **Don't** introduce a second display typeface or purple/glow marketing chrome.
