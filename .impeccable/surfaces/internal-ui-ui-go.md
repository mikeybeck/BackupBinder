---
version: 1
slug: "internal-ui-ui-go"
primary_target: "internal/ui/ui.go"
related_targets: []
---

# Localhost UI

<!-- impeccable:surface-brief -->

Mode: Operate  
Primary target: `internal/ui/ui.go`  
Audience: anyone backing up files with minimum hassle (first-run + returning)  
Job: set up a trustworthy local backup; after backups exist, see last run and restore confidence without hunting  
Action: Set up a backup (lead); Sites for trust  
Constraints: embedded Go HTML; localhost only; no WordPress-first copy; local-only is complete; destinations optional; never invent secrets

## Direction contract

THESIS: The UI is a household binder — Setup and Sites as labeled tabs — so the next backup decision is always one page turn away, not a generic cloud dashboard of status cards.  
OWN-WORLD: Paper ground `#f2efe8`, graphite rail `#1e2430`, teal accent `#0f5c57`, brass tab hint `#c4a574`; system UI sans; dense labeled forms; quiet list with one open site.  
STORY: First open lands on Setup; local-only is enough; site types are addons; after a backup exists, Sites makes trust (last run + restore confidence) obvious.  
FIRST VIEWPORT: Dark left rail with brand, Setup/Sites tabs, and Set up a backup CTA; main pane shows the active tab’s single job; empty Sites never appears before Setup when there are no sites.  
FORM: Household Binder (model-pick); seed key `2d1a13a1`.  
FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
