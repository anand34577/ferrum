import { Check, ChevronDown, ListFilter } from "lucide-react"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"

/**
 * Multi-select dropdown for filters. The trigger summarizes the selection the
 * way fleet dashboards should: nothing (or everything) selected reads "All …",
 * exactly one selected reads that option's name, and a partial mix reads
 * "N selected" — so a two-value filter like VM/Container never overflows its
 * trigger the way a long single-select label would.
 *
 * An empty `selected` is the "all" state; there is deliberately no separate
 * All option value to keep filter predicates simple (`set.has(x)` with an
 * empty set meaning "don't filter").
 */

export interface MultiSelectOption {
  value: string
  label: string
}

interface MultiSelectProps {
  options: MultiSelectOption[]
  /** Currently picked values; empty means "all". */
  selected: string[]
  onChange: (selected: string[]) => void
  /** Trigger text for the everything-selected state, e.g. "All types". */
  allLabel: string
  /** Optional heading inside the dropdown, e.g. "Filter by status". */
  label?: string
  className?: string
  /** Show a filter icon before the summary text. */
  showFilterIcon?: boolean
}

export function MultiSelect({ options, selected, onChange, allLabel, label, className, showFilterIcon = true }: MultiSelectProps) {
  const all = selected.length === 0 || selected.length === options.length
  let summary: string
  if (all) {
    summary = allLabel
  } else if (selected.length === 1) {
    summary = options.find((o) => o.value === selected[0])?.label ?? allLabel
  } else {
    summary = `${selected.length} selected`
  }

  const toggle = (value: string) => {
    const isPicked = selected.includes(value)
    const next = isPicked ? selected.filter((v) => v !== value) : [...selected, value]
    // Toggling the last option off (or picking every option on) lands back on
    // the canonical "all" state: an empty selection.
    onChange(next.length === options.length ? [] : next)
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className={cn(
          "flex h-9 items-center justify-between gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-surface)] px-3 text-sm shadow-xs transition-colors",
          "hover:border-[var(--border-strong)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]",
          "data-[state=open]:border-[var(--border-strong)]",
          className,
        )}
        aria-label={label ?? allLabel}
      >
        <span className="flex min-w-0 items-center gap-1.5">
          {showFilterIcon && <ListFilter className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" aria-hidden />}
          <span className={cn("truncate", all && "text-[var(--text-muted)]")}>{summary}</span>
        </span>
        <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-[var(--radix-dropdown-menu-content-available-height,18rem)] min-w-44 overflow-y-auto">
        <DropdownMenuLabel>{label ?? allLabel}</DropdownMenuLabel>
        <DropdownMenuItem
          onSelect={(e) => {
            e.preventDefault()
            onChange([])
          }}
          className={cn("justify-between", selected.length === 0 && "bg-[var(--bg-muted)]")}
        >
          All
          {selected.length === 0 && <Check className="h-3.5 w-3.5" aria-hidden />}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {options.map((o) => {
          const picked = selected.includes(o.value)
          return (
            <DropdownMenuItem
              key={o.value}
              onSelect={(e) => {
                // Keep the menu open so several options can be ticked in one go.
                e.preventDefault()
                toggle(o.value)
              }}
            >
              <span
                className={cn(
                  "flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-[3px] border",
                  picked ? "border-brand-600 bg-brand-600 text-white" : "border-[var(--border-strong)]",
                )}
                aria-hidden
              >
                {picked && <Check className="h-3 w-3" />}
              </span>
              {o.label}
            </DropdownMenuItem>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
