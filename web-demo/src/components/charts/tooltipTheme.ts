import type { CSSProperties } from "react"

/**
 * Shared chrome for every Recharts `<Tooltip>` that uses the default content.
 *
 * Recharts 3 hardcodes the default `itemStyle` color to `#000`, which is
 * unreadable black-on-dark once the tooltip surface is themed for dark mode —
 * so the colors must always be passed explicitly. Spreading these props at
 * every call site also keeps the tooltips visually identical (same surface,
 * border, shadow and type scale) instead of the drift we had before.
 *
 * Keep `cursor` (and `formatter`) at the call site — they differ per chart
 * type. `isAnimationActive: false` turns off the 400ms transform glide: the
 * tooltip tracks the cursor immediately, which reads far better on dense,
 * fast-refreshing ops charts.
 */
export const chartTooltip = {
  contentStyle: {
    background: "var(--bg-elevated)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-md)",
    fontSize: 12,
    boxShadow: "var(--shadow-md)",
    color: "var(--text)",
  } satisfies CSSProperties,
  labelStyle: {
    color: "var(--text-muted)",
    fontSize: 11,
    fontWeight: 600,
    marginBottom: 4,
  } satisfies CSSProperties,
  itemStyle: {
    color: "var(--text)",
    paddingTop: 2,
    paddingBottom: 2,
  } satisfies CSSProperties,
  isAnimationActive: false,
} as const
