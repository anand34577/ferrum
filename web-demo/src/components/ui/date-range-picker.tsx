import { CalendarDays, ChevronDown, ChevronLeft, ChevronRight, X } from "lucide-react"
import { type KeyboardEvent, useMemo, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { addDays, addMonths, type DateRange, formatRange, isSameDay, localeWeekStart, monthGrid, startOfDay } from "@/lib/dates"
import { cn } from "@/lib/utils"

interface Preset {
  label: string
  range: () => DateRange
}

const PRESETS: Preset[] = [
  { label: "Today", range: () => ({ from: startOfDay(new Date()), to: startOfDay(new Date()) }) },
  { label: "Yesterday", range: () => ({ from: addDays(new Date(), -1), to: addDays(new Date(), -1) }) },
  { label: "Last 7 days", range: () => ({ from: addDays(new Date(), -6), to: startOfDay(new Date()) }) },
  { label: "Last 30 days", range: () => ({ from: addDays(new Date(), -29), to: startOfDay(new Date()) }) },
  { label: "This month", range: () => ({ from: new Date(new Date().getFullYear(), new Date().getMonth(), 1), to: startOfDay(new Date()) }) },
]

interface DateRangePickerProps {
  value: DateRange
  onChange: (range: DateRange) => void
  /** Trigger text while no range is set. */
  placeholder?: string
  /** Days after this can't be picked — pass today for logs and history. */
  maxDate?: Date
  className?: string
}

/**
 * The app's date-range control: a themed calendar popover in place of the
 * browser's `<input type="date">`, which looks different in every browser,
 * ignores the theme, and can't express a range at all.
 *
 * Click a start day, then an end day (in either order). Presets cover the
 * common ranges in one click. Keyboard: arrows move by day/week, PageUp /
 * PageDown by month, Home/End to the week's edges, Enter or Space to pick.
 */
export function DateRangePicker({ value, onChange, placeholder = "Any time", maxDate, className }: DateRangePickerProps) {
  const [open, setOpen] = useState(false)
  // The range being built inside the popover; committed on the second click.
  const [draft, setDraft] = useState<DateRange>(value)
  const [month, setMonth] = useState(() => startOfDay(value.to ?? value.from ?? new Date()))
  const [focused, setFocused] = useState(() => startOfDay(value.to ?? value.from ?? new Date()))
  const [hovered, setHovered] = useState<Date | null>(null)
  const gridRef = useRef<HTMLDivElement>(null)

  const weekStartsOn = useMemo(() => localeWeekStart(), [])
  const days = useMemo(() => monthGrid(month, weekStartsOn), [month, weekStartsOn])
  const weekdays = useMemo(() => {
    const fmt = new Intl.DateTimeFormat(undefined, { weekday: "short" })
    return days.slice(0, 7).map((d) => fmt.format(d))
  }, [days])
  const monthLabel = useMemo(() => new Intl.DateTimeFormat(undefined, { month: "long", year: "numeric" }).format(month), [month])
  const dayLabel = useMemo(() => new Intl.DateTimeFormat(undefined, { dateStyle: "full" }), [])
  const max = maxDate ? startOfDay(maxDate) : undefined
  const today = startOfDay(new Date())

  function onOpenChange(next: boolean) {
    setOpen(next)
    if (next) {
      const anchor = startOfDay(value.to ?? value.from ?? new Date())
      setDraft(value)
      setMonth(anchor)
      setFocused(anchor)
      setHovered(null)
    }
  }

  const disabled = (d: Date) => !!max && d.getTime() > max.getTime()

  function pick(day: Date) {
    if (disabled(day)) return
    // Second click of a range: order the two ends and commit.
    if (draft.from && !draft.to) {
      const [from, to] = day.getTime() < draft.from.getTime() ? [day, draft.from] : [draft.from, day]
      setDraft({ from, to })
      onChange({ from, to })
      setOpen(false)
      return
    }
    setDraft({ from: day, to: undefined })
  }

  function moveFocus(next: Date) {
    if (disabled(next)) next = max!
    setFocused(next)
    if (next.getMonth() !== month.getMonth() || next.getFullYear() !== month.getFullYear()) setMonth(next)
    // After the grid re-renders for a new month, focus the day's button.
    requestAnimationFrame(() => gridRef.current?.querySelector<HTMLButtonElement>('[data-focused="true"]')?.focus())
  }

  function onKeyDown(e: KeyboardEvent) {
    const offset = (focused.getDay() - weekStartsOn + 7) % 7
    const moves: Record<string, () => Date> = {
      ArrowLeft: () => addDays(focused, -1),
      ArrowRight: () => addDays(focused, 1),
      ArrowUp: () => addDays(focused, -7),
      ArrowDown: () => addDays(focused, 7),
      Home: () => addDays(focused, -offset),
      End: () => addDays(focused, 6 - offset),
      PageUp: () => new Date(focused.getFullYear(), focused.getMonth() - 1, focused.getDate()),
      PageDown: () => new Date(focused.getFullYear(), focused.getMonth() + 1, focused.getDate()),
    }
    const move = moves[e.key]
    if (!move) return
    e.preventDefault()
    moveFocus(move())
  }

  // While choosing the end day, preview the range out to the hovered/focused day.
  const previewEnd = draft.from && !draft.to ? (hovered ?? focused) : undefined
  const lo = draft.from && (draft.to ?? previewEnd) ? Math.min(draft.from.getTime(), (draft.to ?? previewEnd)!.getTime()) : undefined
  const hi = draft.from && (draft.to ?? previewEnd) ? Math.max(draft.from.getTime(), (draft.to ?? previewEnd)!.getTime()) : undefined

  const label = value.from ? formatRange(value) : placeholder
  const nextMonthDisabled = !!max && addMonths(month, 1).getTime() > max.getTime()

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <div className={cn("relative", className)}>
        <PopoverTrigger
          className={cn(
            "flex h-9 w-full items-center justify-between gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-surface)] px-3 text-sm shadow-xs transition-colors",
            "hover:border-[var(--border-strong)] focus-visible:outline-none focus-visible:border-[var(--ring)] focus-visible:ring-[3px] focus-visible:ring-[color-mix(in_oklab,var(--ring)_25%,transparent)]",
            "data-[state=open]:border-[var(--border-strong)]",
            value.from && "pr-8",
          )}
          aria-label={value.from ? `Date range: ${label}` : "Filter by date"}
        >
          <span className="flex min-w-0 items-center gap-1.5">
            <CalendarDays className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" aria-hidden />
            <span className={cn("truncate tabular", !value.from && "text-[var(--text-muted)]")}>{label}</span>
          </span>
          {!value.from && <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" aria-hidden />}
        </PopoverTrigger>
        {/* Sibling of the trigger, not a child: a button can't nest a button. */}
        {value.from && (
          <button
            type="button"
            onClick={() => onChange({})}
            aria-label="Clear date range"
            className="absolute right-1 top-1/2 flex h-7 w-7 -translate-y-1/2 cursor-pointer items-center justify-center rounded-sm text-[var(--text-faint)] transition-colors hover:text-[var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]"
          >
            <X className="h-3.5 w-3.5" aria-hidden />
          </button>
        )}
      </div>

      <PopoverContent
        className="flex flex-col sm:flex-row"
        aria-label="Choose a date range"
        // Land on the calendar's current day, not the first preset: arrow
        // keys work immediately, and a mouse user doesn't get a focus ring
        // parked on "Today" as if it were selected.
        onOpenAutoFocus={(e) => {
          e.preventDefault()
          gridRef.current?.querySelector<HTMLButtonElement>('[data-focused="true"]')?.focus()
        }}
      >
        <div className="flex gap-1 overflow-x-auto border-b border-[var(--border)] p-2 sm:w-36 sm:flex-col sm:border-b-0 sm:border-r">
          {PRESETS.map((p) => {
            const r = p.range()
            const active = isSameDay(r.from, value.from) && isSameDay(r.to, value.to)
            return (
              <button
                key={p.label}
                type="button"
                onClick={() => {
                  onChange(r)
                  setOpen(false)
                }}
                className={cn(
                  "shrink-0 cursor-pointer rounded-md px-2.5 py-1.5 text-left text-xs whitespace-nowrap transition-colors hover:bg-[var(--bg-muted)]",
                  active ? "bg-[var(--bg-muted)] font-medium text-[var(--text)]" : "text-[var(--text-muted)] hover:text-[var(--text)]",
                )}
              >
                {p.label}
              </button>
            )
          })}
          <button
            type="button"
            onClick={() => {
              onChange({})
              setOpen(false)
            }}
            className={cn(
              "shrink-0 cursor-pointer rounded-md px-2.5 py-1.5 text-left text-xs whitespace-nowrap transition-colors hover:bg-[var(--bg-muted)] sm:mt-auto",
              !value.from ? "bg-[var(--bg-muted)] font-medium text-[var(--text)]" : "text-[var(--text-muted)] hover:text-[var(--text)]",
            )}
          >
            Any time
          </button>
        </div>

        <div className="p-3">
          <div className="mb-2 flex items-center justify-between">
            <Button size="icon-sm" variant="ghost" aria-label="Previous month" onClick={() => setMonth((m) => addMonths(m, -1))}>
              <ChevronLeft className="h-4 w-4" />
            </Button>
            <span className="font-display text-sm font-semibold" aria-live="polite">{monthLabel}</span>
            <Button size="icon-sm" variant="ghost" aria-label="Next month" disabled={nextMonthDisabled} onClick={() => setMonth((m) => addMonths(m, 1))}>
              <ChevronRight className="h-4 w-4" />
            </Button>
          </div>

          <div ref={gridRef} role="grid" aria-label={monthLabel} onKeyDown={onKeyDown} onPointerLeave={() => setHovered(null)}>
            <div role="row" className="grid grid-cols-7">
              {weekdays.map((w) => (
                <span key={w} role="columnheader" className="flex h-7 w-9 items-center justify-center font-mono text-[10px] uppercase tracking-wide text-[var(--text-faint)]">
                  {w.slice(0, 2)}
                </span>
              ))}
            </div>
            {Array.from({ length: 6 }, (_, week) => (
              <div key={week} role="row" className="grid grid-cols-7">
                {days.slice(week * 7, week * 7 + 7).map((day) => {
                  const t = day.getTime()
                  const outside = day.getMonth() !== month.getMonth()
                  const isStart = lo !== undefined && t === lo
                  const isEnd = hi !== undefined && t === hi
                  const inRange = lo !== undefined && hi !== undefined && t > lo && t < hi
                  const selected = isStart || isEnd || isSameDay(day, draft.from)
                  const isFocused = isSameDay(day, focused)
                  return (
                    <div
                      key={t}
                      role="gridcell"
                      aria-selected={selected || inRange}
                      // The band between the two ends, squared off so it reads as one strip.
                      className={cn(
                        "flex h-9 w-9 items-center justify-center",
                        (inRange || (isStart && hi !== lo) || (isEnd && hi !== lo)) && "bg-[color-mix(in_oklab,var(--color-brand-500)_14%,transparent)]",
                        isStart && "rounded-l-md",
                        isEnd && "rounded-r-md",
                      )}
                    >
                      <button
                        type="button"
                        tabIndex={isFocused ? 0 : -1}
                        data-focused={isFocused}
                        disabled={disabled(day)}
                        aria-label={dayLabel.format(day)}
                        aria-current={isSameDay(day, today) ? "date" : undefined}
                        onClick={() => {
                          setFocused(day)
                          pick(day)
                        }}
                        onPointerEnter={() => setHovered(day)}
                        onFocus={() => setFocused(day)}
                        className={cn(
                          "relative flex h-8 w-8 cursor-pointer items-center justify-center rounded-md text-xs tabular transition-colors",
                          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]",
                          "disabled:cursor-not-allowed disabled:opacity-30",
                          selected
                            ? "bg-brand-600 font-semibold text-white"
                            : cn("hover:bg-[var(--bg-muted)]", outside ? "text-[var(--text-faint)] opacity-60" : "text-[var(--text)]"),
                          // Today: a dot under the number, visible in or out of a selection.
                          isSameDay(day, today) &&
                            "after:absolute after:bottom-1 after:h-1 after:w-1 after:rounded-full after:bg-current after:opacity-70 after:content-['']",
                        )}
                      >
                        {day.getDate()}
                      </button>
                    </div>
                  )
                })}
              </div>
            ))}
          </div>

          <p className="mt-2 h-4 text-center text-[11px] text-[var(--text-muted)]" aria-live="polite">
            {draft.from && !draft.to ? "Now pick the last day" : draft.from ? formatRange(draft) : "Pick the first day"}
          </p>
        </div>
      </PopoverContent>
    </Popover>
  )
}
