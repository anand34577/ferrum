import { cn } from "@/lib/utils"

/** LXC/VM kind chip — the at-a-glance type marker used in inventory rows,
 * the guest detail header and the topology cards, colored consistently
 * everywhere via the chart tokens. */
export function TypeChip({ type, size = "md", className }: { type: string; size?: "sm" | "md"; className?: string }) {
  const lxc = type === "lxc"
  return (
    <span
      className={cn(
        // Bordered, not filled: a tinted fill lifts the background enough that the
        // chart hue drops below 4.5:1 on elevated surfaces (dialogs). Same
        // construction as Badge.
        "shrink-0 rounded border px-1.5 py-px font-mono text-[10px] font-semibold uppercase tracking-wide",
        // Topology's guest cards sit on a 9px type scale and can't spare the
        // default padding — `sm` tightens radius + padding, same colors.
        size === "sm" && "rounded-sm px-1",
        lxc
          ? "border-[color-mix(in_oklab,var(--chart-2)_35%,transparent)] text-[var(--chart-2)]"
          : "border-[color-mix(in_oklab,var(--chart-1)_35%,transparent)] text-[var(--chart-1)]",
        className,
      )}
    >
      {lxc ? "LXC" : "VM"}
    </span>
  )
}

