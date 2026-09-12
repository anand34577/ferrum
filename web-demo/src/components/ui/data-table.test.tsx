import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import type { ColumnDef } from "@tanstack/react-table"
import { describe, expect, it } from "vitest"
import { DataTable } from "./data-table"

interface Row {
  name: string
  age: number
}

const columns: ColumnDef<Row, any>[] = [
  { accessorKey: "name", header: "Name" },
  { accessorKey: "age", header: "Age" },
]

const data: Row[] = [
  { name: "Charlie", age: 40 },
  { name: "Alice", age: 30 },
  { name: "Bob", age: 25 },
]

function rowNames() {
  const rows = screen.getAllByRole("row").slice(1) // drop the header row
  return rows.map((r) => within(r).getAllByRole("cell")[0].textContent)
}

describe("DataTable", () => {
  it("renders every row when unfiltered", () => {
    render(<DataTable columns={columns} data={data} />)
    expect(rowNames()).toEqual(["Charlie", "Alice", "Bob"])
  })

  it("shows the empty message when there is no data", () => {
    render(<DataTable columns={columns} data={[]} emptyMessage="Nothing here." />)
    expect(screen.getByText("Nothing here.")).toBeInTheDocument()
  })

  it("filters rows by the global search box across all columns", async () => {
    const user = userEvent.setup()
    render(<DataTable columns={columns} data={data} searchPlaceholder="Search..." />)

    await user.type(screen.getByPlaceholderText("Search..."), "ali")

    expect(rowNames()).toEqual(["Alice"])
  })

  it("sorts a column ascending then descending on repeated header clicks", async () => {
    const user = userEvent.setup()
    render(<DataTable columns={columns} data={data} />)

    const nameHeader = screen.getByRole("button", { name: /name/i })
    await user.click(nameHeader)
    expect(rowNames()).toEqual(["Alice", "Bob", "Charlie"])

    await user.click(nameHeader)
    expect(rowNames()).toEqual(["Charlie", "Bob", "Alice"])
  })

  it("paginates once the row count exceeds pageSize", async () => {
    const user = userEvent.setup()
    const bigData: Row[] = Array.from({ length: 20 }, (_, i) => ({ name: `Row ${i}`, age: i }))
    render(<DataTable columns={columns} data={bigData} pageSize={10} />)

    expect(screen.getAllByRole("row")).toHaveLength(11) // header + 10 rows
    expect(screen.getByText(/page 1 of 2/i)).toBeInTheDocument()

    const prevButton = screen.getByRole("button", { name: "Previous page" })
    const nextButton = screen.getByRole("button", { name: "Next page" })
    expect(prevButton).toBeDisabled()

    await user.click(nextButton)

    expect(screen.getByText(/page 2 of 2/i)).toBeInTheDocument()
    expect(nextButton).toBeDisabled()
    expect(rowNames()).toEqual(Array.from({ length: 10 }, (_, i) => `Row ${i + 10}`))
  })
})
