import { Histogram, type HistogramBin } from "@/components/charts/Histogram"
import type { WidgetSettings } from "@/lib/dashboardTypes"
import { useScopedInventory } from "@/lib/fleet"

// Frequency distribution of guest utilization across the whole fleet —
// shows the shape of the population (most guests idle? a few hot ones?).
export function UtilizationHistogramWidget({ settings }: { settings: WidgetSettings }) {
  const metric = settings.metric === "mem" ? "mem" : "cpu"
  const { resources } = useScopedInventory(settings)

  const values = resources
    .filter((r) => (r.type === "qemu" || r.type === "lxc") && r.status === "running")
    .map((g) => {
      if (metric === "cpu") return (g.cpu ?? 0) * 100
      return g.maxmem ? ((g.mem ?? 0) / g.maxmem) * 100 : undefined
    })
    .filter((v): v is number => v !== undefined)

  const bins: HistogramBin[] = Array.from({ length: 10 }, (_, i) => {
    const lo = i * 10
    const hi = lo + 10
    const count = values.filter((v) => v >= lo && (i === 9 ? v <= 100 : v < hi)).length
    const mid = (lo + hi) / 2
    return {
      label: `${lo}–${hi}`,
      count,
      color: mid >= 90 ? "var(--status-error)" : mid >= 75 ? "var(--status-warn)" : "var(--chart-3)",
    }
  })

  return (
    <div className="flex h-full flex-col">
      <Histogram bins={bins} valueLabel={metric === "cpu" ? "guests" : "guests"} height={150} />
      <p className="mt-auto pt-1 text-center text-[10px] text-[var(--text-faint)]">
        {values.length} running guest{values.length === 1 ? "" : "s"} by {metric === "cpu" ? "CPU" : "memory"} utilization bracket
      </p>
    </div>
  )
}
