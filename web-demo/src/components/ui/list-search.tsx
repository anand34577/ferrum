import { Search, X } from "lucide-react"
import { type InputHTMLAttributes, forwardRef } from "react"
import { cn } from "@/lib/utils"

export interface ListSearchProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "value" | "onChange" | "size"> {
  value: string
  onChange: (value: string) => void
  /** Shown as both the placeholder and the input's accessible name when no
   * separate `aria-label` is given — same convention as DataTable's inline
   * search box. */
  placeholder?: string
  /** Compact height (used inside toolbars/tables) vs the default 36px —
   * both stay >=36px tall so the tap target reads fine on a touch device. */
  size?: "sm" | "default"
  containerClassName?: string
}

/** The one search-a-list input used across the app (Inventory, Storage,
 * Pools, Users, Connections, DataTable's own toolbar, ...): a leading
 * search glyph, a trailing clear button that only appears once there's
 * something to clear, and the same border/focus language as Input. Extracted
 * so every list search behaves and looks identical instead of each page
 * hand-rolling its own `<Search/> + <Input/>` pair. */
export const ListSearch = forwardRef<HTMLInputElement, ListSearchProps>(
  ({ value, onChange, placeholder = "Search...", size = "default", className, containerClassName, ...props }, ref) => {
    return (
      <div className={cn("relative", containerClassName)}>
        <Search
          className={cn(
            "pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-[var(--text-faint)]",
            size === "sm" ? "h-3.5 w-3.5" : "h-4 w-4",
          )}
        />
        <input
          ref={ref}
          type="text"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          aria-label={props["aria-label"] ?? placeholder}
          className={cn(
            "flex w-full rounded-md border border-[var(--border)] bg-[var(--bg-surface)] pl-8 pr-8 font-mono text-[var(--text)] transition-all duration-150",
            "placeholder:text-[var(--text-faint)] hover:border-[var(--border-strong)]",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)] focus-visible:border-transparent",
            "disabled:cursor-not-allowed disabled:opacity-50",
            size === "sm" ? "h-8.5 text-xs" : "h-9 text-sm",
            className,
          )}
          {...props}
        />
        {value && (
          <button
            type="button"
            onClick={() => onChange("")}
            aria-label="Clear search"
            className={cn(
              "absolute right-1 top-1/2 flex -translate-y-1/2 items-center justify-center rounded-sm text-[var(--text-faint)] transition-colors",
              "hover:text-[var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]",
              "h-7 w-7",
            )}
          >
            <X className={size === "sm" ? "h-3 w-3" : "h-3.5 w-3.5"} />
          </button>
        )}
      </div>
    )
  },
)
ListSearch.displayName = "ListSearch"
