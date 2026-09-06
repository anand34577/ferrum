import type { Look } from "@/lib/theme"

const FLAT: ReadonlySet<Look> = new Set(["proxmox", "brutalist"])
const TIGHT: ReadonlySet<Look> = new Set(["terminal", "glassFlightDeck", "highContrast", "paper"])
const ROUNDED: ReadonlySet<Look> = new Set(["glassmorphism", "neumorphism", "aurora"])

/**
 * Bar/tile corner radius (px) that matches the active look's radius scale.
 * Recharts/SVG shapes need a literal pixel number — they can't resolve a
 * CSS custom property the way a `rounded-*` utility can — so without this,
 * chart bars stay permanently rounded even on a deliberately hard-edged
 * look (Proxmox-native, Brutalist), which is exactly the "boxy design but
 * the bars have corners" inconsistency to avoid.
 */
export function chartBarRadius(look: Look): number {
  if (FLAT.has(look)) return 0
  if (TIGHT.has(look)) return 2
  if (ROUNDED.has(look)) return 6
  return 4
}
