/** Calendar-day helpers for the date-range picker. Everything here works in
 * the viewer's local time zone: "12 March" means their 12 March. */

export interface DateRange {
  /** First day of the range (any time of day; treated as that whole day). */
  from?: Date
  /** Last day of the range, inclusive. */
  to?: Date
}

export function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate())
}

/** Day arithmetic through the Date constructor, so it stays correct across
 * daylight-saving changes (adding 24h in milliseconds does not). */
export function addDays(d: Date, days: number): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + days)
}

export function addMonths(d: Date, months: number): Date {
  return new Date(d.getFullYear(), d.getMonth() + months, 1)
}

export function isSameDay(a: Date | undefined, b: Date | undefined): boolean {
  return !!a && !!b && a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
}

/** The 42 days (6 weeks) shown for a month, padded with the neighbouring
 * months' days so every month renders the same height. `weekStartsOn`:
 * 0 = Sunday … 6 = Saturday. */
export function monthGrid(month: Date, weekStartsOn: number): Date[] {
  const first = new Date(month.getFullYear(), month.getMonth(), 1)
  const lead = (first.getDay() - weekStartsOn + 7) % 7
  return Array.from({ length: 42 }, (_, i) => addDays(first, i - lead))
}

/** First day of the week for the viewer's locale (0 = Sunday … 6 = Saturday). */
export function localeWeekStart(locale?: string): number {
  try {
    const loc = new Intl.Locale(locale ?? navigator.language) as Intl.Locale & {
      getWeekInfo?: () => { firstDay: number }
      weekInfo?: { firstDay: number }
    }
    const firstDay = loc.getWeekInfo?.().firstDay ?? loc.weekInfo?.firstDay
    if (typeof firstDay === "number") return firstDay % 7 // Intl uses 1 = Monday … 7 = Sunday
  } catch {
    // unsupported locale API — fall through
  }
  return 1
}

/** Turns an inclusive day range into the half-open instant range an API
 * wants: [start of `from`, start of the day after `to`). */
export function rangeToInstants(range: DateRange): { from?: string; to?: string } {
  return {
    from: range.from ? startOfDay(range.from).toISOString() : undefined,
    to: range.to ? addDays(range.to, 1).toISOString() : range.from ? addDays(range.from, 1).toISOString() : undefined,
  }
}

const dayMonth = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short" })
const dayMonthYear = new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short", year: "numeric" })

/** "12 Mar 2026", "3 – 12 Mar 2026", "28 Dec 2025 – 4 Jan 2026". */
export function formatRange(range: DateRange): string {
  const { from, to } = range
  if (!from) return ""
  if (!to || isSameDay(from, to)) return dayMonthYear.format(from)
  if (from.getFullYear() === to.getFullYear()) return `${dayMonth.format(from)} – ${dayMonthYear.format(to)}`
  return `${dayMonthYear.format(from)} – ${dayMonthYear.format(to)}`
}
