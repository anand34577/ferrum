import { cn } from "@/lib/utils"

/** LXC/VM kind chip — the at-a-glance type marker used in inventory rows and
 * the guest detail header, colored consistently with the topology view. */
export function TypeChip({ type, className }: { type: string; className?: string }) {
  const lxc = type === "lxc"
  return (
    <span
      className={cn(
        "shrink-0 rounded px-1.5 py-px font-mono text-[9px] font-semibold uppercase tracking-wide",
        lxc
          ? "bg-[color-mix(in_oklab,var(--chart-2)_15%,transparent)] text-[var(--chart-2)]"
          : "bg-[color-mix(in_oklab,var(--chart-1)_15%,transparent)] text-[var(--chart-1)]",
        className,
      )}
    >
      {lxc ? "LXC" : "VM"}
    </span>
  )
}

