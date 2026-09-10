import { Check, ChevronDown, Search } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"

export interface ComboboxOption {
  value: string
  label: string
  disabled?: boolean
}

interface ComboboxProps {
  options: ComboboxOption[]
  value?: string
  onChange: (value: string) => void
  placeholder?: string
  searchPlaceholder?: string
  emptyText?: string
  className?: string
  disabled?: boolean
  /** Below this many options, skip the search box entirely — same threshold
   * ListSearch uses elsewhere, so a short list (a handful of disk buses)
   * doesn't grow an extra row for nothing. */
  searchThreshold?: number
}

/** `Select` has no way to search a long option list — fine for a handful of
 * enum values, unusable for a node/storage/ISO picker on a real fleet. This
 * is the same trigger look as `Select`, but built on `DropdownMenu` (a plain
 * `<input>` inside the popover, not a Radix listbox) so the options can
 * actually be filtered by typing instead of blind-scrolling an 18rem box. */
export function Combobox({
  options,
  value,
  onChange,
  placeholder = "Select…",
  searchPlaceholder = "Search...",
  emptyText = "No matches.",
  className,
  disabled,
  searchThreshold = 8,
}: ComboboxProps) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState("")
  const inputRef = useRef<HTMLInputElement>(null)
  const selected = options.find((o) => o.value === value)
  const showSearch = options.length > searchThreshold
  const filtered = search ? options.filter((o) => o.label.toLowerCase().includes(search.toLowerCase())) : options

  useEffect(() => {
    if (!open) {
      setSearch("")
      return
    }
    if (showSearch) inputRef.current?.focus()
  }, [open, showSearch])

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        disabled={disabled}
        className={cn(
          "flex h-9 w-full items-center justify-between gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-surface)] px-3 font-mono text-sm transition-colors",
          "hover:border-[var(--border-strong)] disabled:cursor-not-allowed disabled:opacity-50",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]",
          className,
        )}
      >
        <span className={cn("min-w-0 flex-1 truncate text-left", !selected && "text-[var(--text-faint)]")}>
          {selected?.label ?? placeholder}
        </span>
        <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="start"
        className="max-h-[var(--radix-dropdown-menu-content-available-height,18rem)] min-w-56 overflow-y-auto"
      >
        {showSearch && (
          <div className="relative mb-1 px-1 pt-1">
            <Search className="pointer-events-none absolute left-3.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-[var(--text-faint)]" />
            <input
              ref={inputRef}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={searchPlaceholder}
              aria-label={searchPlaceholder}
              className="h-8 w-full rounded-md border border-[var(--border)] bg-[var(--bg-surface)] pl-8 pr-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]"
              onKeyDown={(e) => {
                // Radix's roving-focus/typeahead on the menu items would
                // otherwise swallow every keystroke meant for this input —
                // only navigation/selection/close keys are allowed through.
                if (!["Escape", "ArrowDown", "ArrowUp", "Enter"].includes(e.key)) {
                  e.stopPropagation()
                }
                if (e.key === "Enter" && filtered.length > 0) {
                  onChange(filtered[0].value)
                  setOpen(false)
                }
              }}
            />
          </div>
        )}
        {filtered.length === 0 && <p className="px-2.5 py-3 text-center text-xs text-[var(--text-muted)]">{emptyText}</p>}
        {filtered.map((o) => (
          <DropdownMenuItem key={o.value} disabled={o.disabled} onSelect={() => onChange(o.value)} className="justify-between gap-2">
            <span className="truncate">{o.label}</span>
            {o.value === value && <Check className="h-3.5 w-3.5 shrink-0" aria-hidden />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
