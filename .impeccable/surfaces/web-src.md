---
version: 1
slug: "web-src"
primary_target: "web/src"
related_targets: []
---

## Direction contract

THESIS: Ferrum reads as a glass flight deck for your fleet — every page is an instrument, not a form; the arrangement it refuses is the generic rounded-card SaaS admin panel with a soft accent color and no caution hierarchy.

OWN-WORLD: near-black panel ground (`#0a0d10`–`#12161a`); caution hierarchy strictly amber(warn)/red(critical, reserved, "master caution")/green(nominal)/cyan(info, live-data); condensed grotesk (Inter/Plex Sans Condensed) in tracked ALL-CAPS for panel labels/nav; tabular monospace (IBM Plex Mono / JetBrains Mono) for every number, timestamp, and ID; hairline 1px panel-seam borders, no soft drop shadows, no large border-radius (2–4px only); toggle-switch controls for booleans, annunciator-chip controls for status.

STORY: an operator opens Ferrum and immediately reads fleet state the way a pilot reads a panel — nominal is quiet and green, anything needing action surfaces as a caution/master-caution element they cannot miss, and drilling into a node/VM is "selecting a system page," not navigating to a new app.

FIRST VIEWPORT (Overview/Dashboard): a persistent master-caution bar pinned at the very top (empty/quiet when nothing needs attention, amber/red banded strip with the worst active alert when it does); below it a synoptic strip of fleet-wide instrument tiles (nodes up, VMs running, storage headroom, active tasks) rendered as digit-bank readouts with engraved caps labels; below that the per-cluster/per-node system grid as a dense monospace table with annunciator-chip status in the leftmost column.

FORM: chosen candidate "Glass Flight Deck" (aviation cockpit/EICAS), IMPECCABLE'S PICK card, seed key 026d7572 (assigned index 5 was "Approach Scope"/ATC radar; user selected the pick instead).

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance.
