---
name: BackupBinder
description: Filing-cabinet marketing page for local-first site backups on this machine.
colors:
  metal: "#2a2e35"
  metal-deep: "#1a1c20"
  metal-edge: "#3a3f48"
  manila: "#e8dcc8"
  manila-deep: "#d9cbb0"
  kraft: "#c9a66b"
  cream: "#f7f1e6"
  stamp: "#0d6b5c"
  stamp-hover: "#0a5448"
  ink: "#1a1c20"
  ink-soft: "#3a3530"
  muted: "#5a5448"
  ok: "#1f6b3a"
  focus: "#1a4f8b"
  selection: "#c5e4df"
  handle: "#4a505a"
  foot: "#e2e6ed"
  foot-muted: "#c5cad3"
typography:
  display:
    fontFamily: "\"Archivo\", system-ui, sans-serif"
    fontSize: "clamp(2.6rem, 7vw, 4.25rem)"
    fontWeight: 800
    lineHeight: 1
    letterSpacing: "-0.03em"
  headline:
    fontFamily: "\"Archivo\", system-ui, sans-serif"
    fontSize: "clamp(1.75rem, 4.2vw, 2.65rem)"
    fontWeight: 800
    lineHeight: 1.12
    letterSpacing: "-0.025em"
  title:
    fontFamily: "\"Archivo\", system-ui, sans-serif"
    fontSize: "clamp(1.45rem, 3vw, 2rem)"
    fontWeight: 800
    lineHeight: 1.2
    letterSpacing: "-0.02em"
  body:
    fontFamily: "\"Source Sans 3\", system-ui, sans-serif"
    fontSize: "1.0625rem"
    fontWeight: 400
    lineHeight: 1.5
  lede:
    fontFamily: "\"Source Sans 3\", system-ui, sans-serif"
    fontSize: "1.125rem"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "\"Archivo\", system-ui, sans-serif"
    fontSize: "0.78rem"
    fontWeight: 800
    letterSpacing: "0.06em"
  tab:
    fontFamily: "\"Archivo\", system-ui, sans-serif"
    fontSize: "0.875rem"
    fontWeight: 700
    letterSpacing: "0.04em"
  mono:
    fontFamily: "ui-monospace, \"Cascadia Code\", \"SF Mono\", Menlo, monospace"
    fontSize: "0.88rem"
    lineHeight: 1.55
rounded:
  tab: "5px 5px 0 0"
  folder-tab: "4px 4px 0 0"
  folder: "0 10px 4px 4px"
  peek: "7px 7px 0 0"
  handle: "999px"
  stamp: "0"
spacing:
  pad: "clamp(1.25rem, 4vw, 3.5rem)"
  section-gap: "1.25rem"
  measure: "38rem"
  cross-check-max: "36rem"
  flow-max: "40rem"
  sheet-max: "44rem"
  breakpoint: "720px"
components:
  stamp-primary:
    backgroundColor: "{colors.stamp}"
    textColor: "{colors.cream}"
    typography: "{typography.label}"
    rounded: "{rounded.stamp}"
    padding: "0.85rem 1.25rem"
  stamp-primary-hover:
    backgroundColor: "{colors.stamp-hover}"
    textColor: "{colors.cream}"
  stamp-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.stamp}"
    rounded: "{rounded.stamp}"
    padding: "0.85rem 1.25rem"
  hanging-tab:
    backgroundColor: "{colors.manila-deep}"
    textColor: "{colors.ink}"
    typography: "{typography.tab}"
    rounded: "{rounded.tab}"
    padding: "0.6rem 0.9rem 0.55rem"
  hanging-tab-active:
    backgroundColor: "{colors.kraft}"
    textColor: "{colors.ink}"
  folder:
    backgroundColor: "{colors.manila}"
    textColor: "{colors.ink}"
    rounded: "{rounded.folder}"
    padding: "clamp(1.75rem, 4vw, 3rem) clamp(1.5rem, 4vw, 3.5rem)"
  sheet:
    backgroundColor: "{colors.cream}"
    textColor: "{colors.ink}"
    rounded: "{rounded.stamp}"
    padding: "1.15rem 1.25rem"
---

# Design System: BackupBinder

## Overview

**Creative North Star: "Filing Cabinet Tab Index"**

The marketing site is an open graphite filing-cabinet drawer: hanging manila tabs for navigation, stacked kraft-spined folders for sections, and a rubber-stamp teal CTA. It reads as household record-keeping on this machine — sites as labeled folders — not a cloud backup dashboard of equal feature cards.

Depth comes from metal drawer chrome, hard folder borders, stacked manila layers, and a slightly skewed stamp. Typography pairs Archivo (tabs, brand, stamps, labels) with Source Sans 3 (body). Soft full-page gradient washes, icon-card grids, and system display faces are out of world.

**Key Characteristics:**
- Full-bleed graphite metal drawer; manila folder faces with kraft spine and tab
- Hanging uppercase tabs as primary nav; brand is a kraft tab and hero display name
- Rubber-stamp primary CTA (solid teal, hard border, slight rotate); dashed ghost secondary
- Trust as typed cross-check labels and ruled lists — not metric cards
- Archivo + Source Sans 3; hard metal-deep borders throughout

## Colors

Cabinet metal, manila paper, kraft brass tabs, and one stamp teal for decisive actions. Status green marks trust bullets only; focus blue is for keyboard rings alone.

### Primary
- **Stamp Teal** (`stamp`): Primary CTA fill and ghost-stamp text. The action color — rare, decisive, rubber-ink.
- **Stamp Teal Deep** (`stamp-hover`): Hover fill for the solid stamp.

### Secondary
- **Kraft Brass** (`kraft`): Active/brand hanging tabs, folder spines, folder tab labels, footer brand emphasis. Structural cabinet metal/paper cue, not a page flood.

### Neutral
- **Drawer Metal** (`metal` / `metal-edge` / `metal-deep`): Cabinet shell gradient and deep ground; metal-deep also strokes every folder, tab, and stamp.
- **Manila** (`manila`): Open folder face.
- **Manila Deep** (`manila-deep`): Inactive hanging tabs, folder peek strip, stacked folder shadow layer.
- **Folder Cream** (`cream`): Command sheet surface and stamp label ink.
- **Cabinet Ink** (`ink`): Primary text (matches metal-deep).
- **Warm Ink Soft** (`ink-soft`): Lede and supporting copy on manila.
- **File Mute** (`muted`): Subtitle and cross-check values.
- **Handle Steel** (`handle`): Drawer pull bar.
- **Foot Mist** / **Foot Mute** (`foot` / `foot-muted`): Footer type on metal.
- **Selection Mint** (`selection`): Text selection wash.
- **Focus Blue** (`focus`): `:focus-visible` outline only.

### Status
- **Restore Green** (`ok`): Square markers on the trust list — prove-restore signal, not a fill system.

### Named Rules
**The Stamp Is the Accent Rule.** Teal appears on CTAs (and ghost-stamp text). Kraft marks tabs, spines, and structural labels — do not swap their jobs or flood the page with either.

**The Manila Ground Rule.** Content lives on manila folder faces inside the metal drawer. Cream is for the command sheet inset, not a second full-page background.

**The No Soft Wash Rule.** Cabinet depth is a hard metal gradient and stacked folders. Do not add decorative soft color washes behind sections.

## Typography

**Display Font:** Archivo (with system-ui sans fallback)
**Body Font:** Source Sans 3 (with system-ui sans fallback)
**Mono Font:** ui-monospace / Cascadia Code / SF Mono / Menlo (code and the get-started sheet only)

**Character:** Archivo carries the typed-label, tab-index voice — heavy, slightly condensed, uppercase where it is chrome. Source Sans 3 carries the readable file contents: warm, open, never display-costumed.

### Hierarchy
- **Display / brand** (800, `clamp(2.6rem, 7vw, 4.25rem)`, line-height 1): Hero product name only — largest signal in the first viewport.
- **Headline** (800, `clamp(1.75rem, 4.2vw, 2.65rem)`, ~1.12, max ~16ch): Hero promise under the brand.
- **Title** (800, `clamp(1.45rem, 3vw, 2rem)`, ~1.2, max ~22ch): Section folder `h2`.
- **Door title** (800, ~1.05rem): Three-doors list headings.
- **Body** (400, 1.0625rem / 1.5): Default copy; lede at 1.125rem, measure ~38rem.
- **Label** (800, ~0.78–0.85rem, 0.05–0.07em, uppercase): Cross-check `dt`, flow step names, folder peek, stamp text.
- **Tab** (700–800, 0.875rem, 0.04em, uppercase): Hanging nav; brand tab at 800.
- **Mono / sheet** (~0.88rem, 1.55): Get-started commands; inline `code` slightly smaller with a light ink wash outside the sheet.

### Named Rules
**The Brand Speaks First Rule.** In the hero, BackupBinder is display-scale; the headline must not overpower it. No kicker or eyebrow above the brand or headline.

**The Typed Label Rule.** Uppercase Archivo labels are for chrome (tabs, stamps, cross-check keys, flow steps). Body sentences stay sentence case in Source Sans 3.

## Layout

One composition: sticky drawer chrome (handle + hanging tabs) over a vertical stack of folders with `1.25rem` gap and horizontal pad `clamp(1.25rem, 4vw, 3.5rem)`. The hero folder is the first viewport (`min-height` up to ~70vh / 36rem on desktop); a hanging peek strip tips the next folder. Section folders are full-width manila slabs — not equal card grids.

Flow steps are a two-column ruled list (`8.5rem` label + body; stacks to one column below 720px). Doors and trust items are single-column ruled lists. Cross-check is three columns (collapses to 2+1 on small screens). Footer sits on metal below the last folder.

## Elevation & Depth

Hybrid: tonal metal drawer + hard structural borders + layered manila shadows. Soft ambient blur appears only under the open folder stack; interactive stamps use hard offset “ink” shadows native to the rubber-stamp material.

### Shadow Vocabulary
- **Folder stack** (`inset 0 0 0 1px rgba(255,255,255,0.28), 0 10px 0 -2px manila-deep, 0 14px 28px rgba(0,0,0,0.28)`): Open folder resting in the drawer.
- **Tab rail lip** (gradient bar + `inset 0 2px 0 …` + `0 3px 0 #0e1014`): Metal ledge under hanging tabs.
- **Stamp press** (`inset` cream ring + bottom inset dark + `2px 3px 0 rgba(26,28,32,0.35)`): Primary CTA at rest; hover lifts offset slightly.
- **Handle inset** (`inset 0 -1px 0 rgba(255,255,255,0.12)`): Drawer pull.

### Named Rules
**The Hard Edge Rule.** Folders, tabs, stamps, and the cream sheet use 2–3px `metal-deep` borders. Do not substitute soft shadow-only cards for that stroke language.

**The Stamp Offset Rule.** Hard offset shadows belong on the rubber stamp (and folder stack geometry), not as a generic card costume across the page.

## Shapes

Tabs and peeks are open at the bottom (top radii only: tab ~5px, folder tab ~4px, peek ~7px). Folder faces use asymmetric radius (`0 10px 4px 4px`) with a kraft left spine (~0.85rem). Stamps and the cream command sheet are square-cornered. The drawer handle is a small pill (~999px). Primary stamp rests at ~`-1.4deg`; ghost stamp at ~`0.8deg`.

### Named Rules
**The Open Tab Rule.** Hanging tabs and peeks never close their bottom edge — they hang from the metal rail or the folder face.

## Components

### Buttons (Stamps)
- **Shape:** Square corners; 3px metal-deep border; uppercase Archivo
- **Primary:** Stamp teal fill, cream type, slight rotate, hard offset shadow; padding `0.85rem 1.25rem`
- **Hover:** Deeper teal, slight un-rotate + `translateY(-2px)`, stronger offset
- **Ghost:** Transparent fill, dashed border, stamp-teal type, opposite micro-rotate; hover adds light teal wash only

### Folders / Containers
- **Corner Style:** Asymmetric folder radius; kraft spine + optional kraft tab label
- **Background:** Manila face; cream only for the command `sheet`
- **Shadow Strategy:** Folder stack (see Elevation)
- **Border:** 3px metal-deep
- **Internal Padding:** Face pad `clamp(1.75rem, 4vw, 3rem)` horizontal/vertical; left pad opens for the spine

### Navigation (Hanging Tabs)
- Sticky metal chrome with centered handle bar
- Manila-deep tabs; kraft for brand and `aria-current="page"`
- Hover: kraft fill + `translateY(-2px)`
- Mobile: slightly tighter type and padding; tabs wrap

### Cross-check / Flow / Doors / Trust
- Ruled dividers (`2px` section tops, `1px` row rules) — not equal cards
- Cross-check: uppercase `dt` + muted `dd`
- Trust list: small ok-green squares with metal-deep stroke as markers (not emoji or icon fonts)

### Command Sheet
- Cream inset with 3px metal-deep border; mono commands; no card chrome beyond the sheet

### Handbook (`site/docs/`)
Reading pages use the same drawer, one open folder, and the cream command sheet. A typed index (uppercase Archivo, kraft underline, stamp underline on the current page) sits under the lede. Body copy stays on the 38rem measure. Section headings have more space above than below. Previous / next links are a ruled row at the bottom of the folder: small uppercase label, page name in Archivo. Docs pages do not repeat the landing hero entrance.

### Signature: Folder Peek
- Deep-manila strip hanging from the hero bottom, uppercase label linking to the next section; hover kraft + lift

## Do's and Don'ts

### Do:
- **Do** keep the first viewport as one open folder: brand, one headline, one lede, stamp CTA group, cross-check, and next-folder peek.
- **Do** use hanging tabs for in-page nav and kraft for the active/brand tab.
- **Do** express sequences as ruled flow/doors/trust lists, not icon+heading card grids.
- **Do** reserve stamp teal for CTAs and kraft for structural cabinet cues.
- **Do** honor `prefers-reduced-motion` (instant hero in; no scroll hijacking).

### Don't:
- **Don't** rebuild this as the localhost Household Binder Operate UI (system UI sans, paper pane, left rail) — that system lives in the repo-root `DESIGN.md`.
- **Don't** add kickers/eyebrows above brand or headlines.
- **Don't** introduce soft purple/indigo washes, glass blurs, or metric/stat strips in the hero.
- **Don't** use emoji or glyph icons for trust markers; keep the square ok markers or typed labels.
- **Don't** invent testimonials, customers, or benchmarks; WordPress stays one addon, not the product story.
