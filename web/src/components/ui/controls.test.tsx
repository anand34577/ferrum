import { act, fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import type { ColumnDef } from "@tanstack/react-table"
import { useState } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { DataTable } from "./data-table"
import { FileDropzone } from "./file-dropzone"
import { Input } from "./input"
import { TipLayer } from "./tip-layer"

describe("Input type=number (themed stepper)", () => {
  function Field(props: { min?: number; max?: number; initial?: string }) {
    const [value, setValue] = useState(props.initial ?? "5")
    return <Input type="number" aria-label="Cores" min={props.min} max={props.max} value={value} onChange={(e) => setValue(e.target.value)} />
  }

  it("steps through the caller's onChange", async () => {
    const user = userEvent.setup()
    render(<Field />)
    await user.click(screen.getByRole("button", { name: "Increase" }))
    await user.click(screen.getByRole("button", { name: "Increase" }))
    expect(screen.getByLabelText("Cores")).toHaveValue(7)
    await user.click(screen.getByRole("button", { name: "Decrease" }))
    expect(screen.getByLabelText("Cores")).toHaveValue(6)
  })

  it("stops at min and max", async () => {
    const user = userEvent.setup()
    render(<Field min={1} max={2} initial="1" />)
    await user.click(screen.getByRole("button", { name: "Decrease" }))
    expect(screen.getByLabelText("Cores")).toHaveValue(1)
    await user.click(screen.getByRole("button", { name: "Increase" }))
    await user.click(screen.getByRole("button", { name: "Increase" }))
    expect(screen.getByLabelText("Cores")).toHaveValue(2)
  })

  it("still accepts typing, and the steppers stay out of the tab order", async () => {
    const user = userEvent.setup()
    render(<Field initial="" />)
    await user.type(screen.getByLabelText("Cores"), "12")
    expect(screen.getByLabelText("Cores")).toHaveValue(12)
    expect(screen.getByRole("button", { name: "Increase" })).toHaveAttribute("tabindex", "-1")
  })

  it("is inert when disabled", () => {
    render(<Input type="number" aria-label="Cores" value={3} onChange={() => {}} disabled />)
    expect(screen.getByRole("button", { name: "Increase" })).toBeDisabled()
  })

  it("leaves other input types as a plain input", () => {
    const { container } = render(<Input aria-label="Name" defaultValue="x" />)
    expect(container.querySelector("button")).toBeNull()
  })
})

describe("FileDropzone", () => {
  const iso = () => new File(["x".repeat(2048)], "debian-13.iso", { type: "application/x-iso9660-image" })

  it("picks a file through the hidden input and shows its name and size", async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    const { container, rerender } = render(<FileDropzone file={null} onChange={onChange} accept=".iso,.img" />)
    await user.upload(container.querySelector('input[type="file"]')!, iso())
    expect(onChange).toHaveBeenCalledTimes(1)

    rerender(<FileDropzone file={onChange.mock.calls[0][0]} onChange={onChange} accept=".iso,.img" />)
    expect(screen.getByText("debian-13.iso")).toBeInTheDocument()
    expect(screen.getByText("2 KB")).toBeInTheDocument()
  })

  it("accepts a dropped file", () => {
    const onChange = vi.fn()
    render(<FileDropzone file={null} onChange={onChange} accept=".iso,.img" />)
    fireEvent.drop(screen.getByText(/drag it here/i).closest("label")!, { dataTransfer: { files: [iso()] } })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ name: "debian-13.iso" }))
  })

  it("rejects a dropped file of the wrong type and says why", () => {
    const onChange = vi.fn()
    render(<FileDropzone file={null} onChange={onChange} accept=".iso,.img" />)
    const wrong = new File(["x"], "notes.txt", { type: "text/plain" })
    fireEvent.drop(screen.getByText(/drag it here/i).closest("label")!, { dataTransfer: { files: [wrong] } })
    expect(onChange).not.toHaveBeenCalled()
    expect(screen.getByRole("alert")).toHaveTextContent("notes.txt")
  })

  it("matches multi-part extensions like .tar.zst", () => {
    const onChange = vi.fn()
    render(<FileDropzone file={null} onChange={onChange} accept=".tar.gz,.tar.xz,.tar.zst" />)
    const tpl = new File(["x"], "debian-13-standard_13.1-2_amd64.tar.zst")
    fireEvent.drop(screen.getByText(/drag it here/i).closest("label")!, { dataTransfer: { files: [tpl] } })
    expect(onChange).toHaveBeenCalled()
  })

  it("clears the file, and locks while uploading", async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    const { rerender } = render(<FileDropzone file={iso()} onChange={onChange} />)
    await user.click(screen.getByRole("button", { name: "Remove debian-13.iso" }))
    expect(onChange).toHaveBeenCalledWith(null)

    rerender(<FileDropzone file={iso()} onChange={onChange} progress={42.4} />)
    expect(screen.getByRole("status")).toHaveTextContent("uploading 42%")
    expect(screen.queryByRole("button", { name: /Remove/ })).toBeNull()
  })
})

describe("TipLayer", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  const over = (el: Element) => fireEvent.pointerOver(el, { pointerType: "mouse" })

  it("shows a data-tip after the hover delay and hides on leave", () => {
    render(
      <>
        <span data-tip="Round-trip time of the last poll">12 ms</span>
        <TipLayer />
      </>,
    )
    over(screen.getByText("12 ms"))
    expect(screen.queryByRole("tooltip")).toBeNull() // not before the delay
    act(() => void vi.advanceTimersByTime(400))
    expect(screen.getByRole("tooltip")).toHaveTextContent("Round-trip time of the last poll")

    fireEvent.pointerOut(screen.getByText("12 ms"))
    expect(screen.queryByRole("tooltip")).toBeNull()
  })

  it("stays quiet when the tip only repeats text that isn't truncated", () => {
    render(
      <>
        <span data-tip="pve-node-01">pve-node-01</span>
        <TipLayer />
      </>,
    )
    over(screen.getByText("pve-node-01"))
    act(() => void vi.advanceTimersByTime(400))
    expect(screen.queryByRole("tooltip")).toBeNull()
  })

  it("ignores touch, and closes on Escape", () => {
    render(
      <>
        <span data-tip="Node online">●</span>
        <TipLayer />
      </>,
    )
    fireEvent.pointerOver(screen.getByText("●"), { pointerType: "touch" })
    act(() => void vi.advanceTimersByTime(400))
    expect(screen.queryByRole("tooltip")).toBeNull()

    over(screen.getByText("●"))
    act(() => void vi.advanceTimersByTime(400))
    expect(screen.getByRole("tooltip")).toBeInTheDocument()
    fireEvent.keyDown(document, { key: "Escape" })
    expect(screen.queryByRole("tooltip")).toBeNull()
  })
})

describe("DataTable preferences", () => {
  interface Row {
    name: string
    age: number
  }
  const columns: ColumnDef<Row, any>[] = [
    { accessorKey: "name", header: "Name" },
    { accessorKey: "age", header: "Age" },
  ]
  const data: Row[] = Array.from({ length: 30 }, (_, i) => ({ name: `Row ${String(i).padStart(2, "0")}`, age: 30 - i }))
  const firstCell = () => screen.getAllByRole("row")[1].querySelector("td")!.textContent

  // An in-memory stand-in: the test runner's own `localStorage` global is not
  // a working Storage, and the table only needs getItem/setItem.
  beforeEach(() => {
    const store = new Map<string, string>()
    vi.stubGlobal("localStorage", {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, String(v)),
      removeItem: (k: string) => void store.delete(k),
      clear: () => store.clear(),
    })
  })
  afterEach(() => vi.unstubAllGlobals())

  it("remembers the sort order under its storageKey", async () => {
    const user = userEvent.setup()
    const { unmount } = render(<DataTable columns={columns} data={data} storageKey="t1" />)
    // Name: first click ascending, second descending.
    await user.click(screen.getByRole("button", { name: /^Name/ }))
    await user.click(screen.getByRole("button", { name: /^Name/ }))
    expect(firstCell()).toBe("Row 29")
    unmount()

    render(<DataTable columns={columns} data={data} storageKey="t1" />)
    expect(firstCell()).toBe("Row 29")
  })

  it("keeps tables with different keys independent, and ignores junk", () => {
    localStorage.setItem("ferrum:table:t1", JSON.stringify({ sorting: [{ id: "age", desc: false }], pageSize: 25 }))
    localStorage.setItem("ferrum:table:t2", "{not json")
    localStorage.setItem("ferrum:table:t3", JSON.stringify({ sorting: [{ id: "removed-column", desc: true }], pageSize: -4 }))

    const a = render(<DataTable columns={columns} data={data} storageKey="t1" />)
    expect(firstCell()).toBe("Row 29")
    expect(screen.getByText(/Page 1 of 2/)).toBeInTheDocument() // 30 rows at 25 per page
    a.unmount()

    for (const key of ["t2", "t3"]) {
      const r = render(<DataTable columns={columns} data={data} storageKey={key} />)
      expect(firstCell()).toBe("Row 00")
      r.unmount()
    }
  })
})
