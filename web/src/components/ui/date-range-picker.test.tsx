import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { DateRange } from "@/lib/dates"
import { DateRangePicker } from "./date-range-picker"

// A fixed "today" so the calendar opens on a known month.
const NOW = new Date(2026, 9, 15, 12) // 15 October 2026

beforeEach(() => {
  vi.useFakeTimers({ now: NOW, toFake: ["Date"] })
})
afterEach(() => {
  vi.useRealTimers()
})

function dayButton(day: number) {
  const grid = screen.getByRole("grid")
  // aria-label is the full date; match the in-month day by its visible number.
  return within(grid)
    .getAllByRole("button")
    .find((b) => b.textContent === String(day) && /October/.test(b.getAttribute("aria-label") ?? ""))!
}

describe("DateRangePicker", () => {
  it("commits an ordered range after two clicks, whichever end is clicked first", async () => {
    const user = userEvent.setup()
    const onChange = vi.fn<(r: DateRange) => void>()
    render(<DateRangePicker value={{}} onChange={onChange} />)

    await user.click(screen.getByRole("button", { name: "Filter by date" }))
    await user.click(dayButton(12))
    expect(onChange).not.toHaveBeenCalled() // only the start is chosen so far
    await user.click(dayButton(5))

    expect(onChange).toHaveBeenCalledTimes(1)
    const range = onChange.mock.calls[0][0]
    expect(range.from?.getDate()).toBe(5)
    expect(range.to?.getDate()).toBe(12)
  })

  it("applies a preset in one click", async () => {
    const user = userEvent.setup()
    const onChange = vi.fn<(r: DateRange) => void>()
    render(<DateRangePicker value={{}} onChange={onChange} />)

    await user.click(screen.getByRole("button", { name: "Filter by date" }))
    await user.click(screen.getByRole("button", { name: "Last 7 days" }))

    const range = onChange.mock.calls[0][0]
    expect(range.from?.getDate()).toBe(9)
    expect(range.to?.getDate()).toBe(15)
  })

  it("won't pick a day after maxDate", async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<DateRangePicker value={{}} onChange={onChange} maxDate={NOW} />)

    await user.click(screen.getByRole("button", { name: "Filter by date" }))
    expect(dayButton(16)).toBeDisabled()
    expect(dayButton(15)).toBeEnabled()
    expect(screen.getByRole("button", { name: "Next month" })).toBeDisabled()
  })

  it("moves by keyboard and selects with Enter", async () => {
    const user = userEvent.setup()
    const onChange = vi.fn<(r: DateRange) => void>()
    render(<DateRangePicker value={{}} onChange={onChange} />)

    await user.click(screen.getByRole("button", { name: "Filter by date" }))
    dayButton(15).focus()
    await user.keyboard("{Enter}{ArrowLeft}{ArrowLeft}")
    // Focus follows on the next frame.
    await vi.waitFor(() => expect(dayButton(13)).toHaveFocus())
    await user.keyboard("{Enter}")

    const range = onChange.mock.calls[0][0]
    expect([range.from?.getDate(), range.to?.getDate()]).toEqual([13, 15])
  })

  it("shows the chosen range on the trigger and clears it", async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(<DateRangePicker value={{ from: new Date(2026, 9, 3), to: new Date(2026, 9, 12) }} onChange={onChange} />)

    expect(screen.getByRole("button", { name: /^Date range:/ })).toHaveTextContent("12")
    await user.click(screen.getByRole("button", { name: "Clear date range" }))
    expect(onChange).toHaveBeenCalledWith({})
  })
})
