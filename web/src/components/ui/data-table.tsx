import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table"
import { ArrowDown, ArrowUp, ArrowUpDown, ChevronLeft, ChevronRight, Download, X } from "lucide-react"
import { type ReactNode, useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { ListSearch } from "@/components/ui/list-search"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { downloadCsv } from "@/lib/download"
import { cn } from "@/lib/utils"

const PAGE_SIZES = [10, 25, 50, 100]

interface SelectionProps<T> {
  /** Stable string id for a row — required to make selection work across sort/filter/paging. */
  rowId: (row: T) => string
  selected: Set<string>
  onSelectedChange: (next: Set<string>) => void
  /** Rendered in a toolbar above the table whenever at least one row is selected. */
  bulkActions?: (selectedIds: string[]) => ReactNode
}

interface DataTableProps<T> {
  columns: ColumnDef<T, any>[]
  data: T[]
  searchPlaceholder?: string
  emptyMessage?: string
  pageSize?: number
  /** Renders skeleton rows instead of the body while the data is in flight. */
  loading?: boolean
  /** Hides the search box for small, fixed datasets. */
  searchable?: boolean
  /** Adds a checkbox column with select-all — pass this instead of building bulk-select per page. */
  selection?: SelectionProps<T>
  /** Extra controls rendered next to the search input (filters, actions). */
  toolbar?: ReactNode
  /** Adds an "Export CSV" button that saves the rows matching the current
   * search/filters, in the current sort order, as `<exportName>.csv`. Only
   * columns with an `accessorKey` and a text header are exported. */
  exportName?: string
  /** Remembers this table's sort order and rows-per-page in this browser
   * under the given key. Must be unique per table. */
  storageKey?: string
}

interface StoredPrefs {
  sorting?: SortingState
  pageSize?: number
}

function loadPrefs(storageKey: string | undefined): StoredPrefs {
  if (!storageKey) return {}
  try {
    const parsed = JSON.parse(localStorage.getItem(`ferrum:table:${storageKey}`) ?? "{}") as StoredPrefs
    return {
      sorting: Array.isArray(parsed.sorting) ? parsed.sorting.filter((s) => typeof s?.id === "string") : undefined,
      pageSize: typeof parsed.pageSize === "number" && parsed.pageSize > 0 && parsed.pageSize <= 500 ? parsed.pageSize : undefined,
    }
  } catch {
    return {} // blocked storage or a corrupt entry — fall back to defaults
  }
}

export function DataTable<T>({
  columns,
  data,
  searchPlaceholder = "Search...",
  emptyMessage = "No results.",
  pageSize = 15,
  loading = false,
  searchable = true,
  selection,
  toolbar,
  exportName,
  storageKey,
}: DataTableProps<T>) {
  // Read once: these seed the table's initial state, they don't track storage.
  const [stored] = useState(() => loadPrefs(storageKey))
  // A remembered sort on a column that has since been renamed or removed
  // would make the table throw, so it is dropped rather than applied.
  const [sorting, setSorting] = useState<SortingState>(() => {
    const known = new Set(columns.map((c) => (c as { id?: string; accessorKey?: string }).id ?? (c as { accessorKey?: string }).accessorKey))
    return (stored.sorting ?? []).filter((s) => known.has(s.id))
  })
  const [globalFilter, setGlobalFilter] = useState("")

  const table = useReactTable({
    data,
    columns,
    state: { sorting, globalFilter },
    onSortingChange: setSorting,
    onGlobalFilterChange: setGlobalFilter,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    initialState: { pagination: { pageSize: stored.pageSize ?? pageSize } },
    // Off on purpose. The default resets to page 1 whenever `data` changes
    // identity — and every table here is fed by a polling query, so anyone
    // reading page 3 was thrown back to page 1 on the next refetch (or on any
    // parent re-render that rebuilt the array). The cases that should move
    // the page are handled explicitly below.
    autoResetPageIndex: false,
  })

  // A match on page 3 is invisible while pagination stays parked on page 1 —
  // the same "search doesn't lead you to the actual result" gap fixed on
  // Inventory, generalized to every DataTable (Tasks, Alerts, Backups,
  // Users, Connections, Audit, ...): jump back to the first page whenever
  // the search term or the sort order changes so the top result is never
  // hidden a page away.
  useEffect(() => {
    table.setPageIndex(0)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [globalFilter, sorting])

  const filteredCount = table.getFilteredRowModel().rows.length
  const pageCount = table.getPageCount()
  const pageIndex = table.getState().pagination.pageIndex
  const currentPageSize = table.getState().pagination.pageSize
  // Rows went away under the current page (a filter narrowed, tasks aged
  // out): land on the new last page rather than an empty one.
  useEffect(() => {
    if (pageIndex > 0 && pageIndex >= pageCount) table.setPageIndex(Math.max(0, pageCount - 1))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pageIndex, pageCount])

  useEffect(() => {
    if (!storageKey) return
    try {
      localStorage.setItem(`ferrum:table:${storageKey}`, JSON.stringify({ sorting, pageSize: currentPageSize } satisfies StoredPrefs))
    } catch {
      // not persisted in this browser
    }
  }, [storageKey, sorting, currentPageSize])

  const pageSizeOptions = PAGE_SIZES.includes(pageSize) ? PAGE_SIZES : [...PAGE_SIZES, pageSize].sort((a, b) => a - b)

  function exportCsv() {
    const exportable = table
      .getAllLeafColumns()
      .map((col) => col.columnDef as ColumnDef<T, any> & { accessorKey?: string })
      .filter((def) => typeof def.accessorKey === "string" && typeof def.header === "string" && def.header !== "")
    const header = exportable.map((def) => def.header as string)
    const body = table
      .getSortedRowModel()
      .rows.map((row) => exportable.map((def) => (row.original as Record<string, unknown>)[def.accessorKey as string]))
    downloadCsv(`${exportName}.csv`, header, body)
  }

  const rows = table.getRowModel().rows
  // "Select all" acts on every row matching the current search/filter, not
  // just the current page — the intuitive meaning when someone is about to
  // bulk-delete a filtered-down list.
  const filteredIds = selection ? table.getFilteredRowModel().rows.map((r) => selection.rowId(r.original)) : []
  const allFilteredSelected = selection ? filteredIds.length > 0 && filteredIds.every((id) => selection.selected.has(id)) : false

  function toggleAll() {
    if (!selection) return
    const next = new Set(selection.selected)
    if (allFilteredSelected) {
      for (const id of filteredIds) next.delete(id)
    } else {
      for (const id of filteredIds) next.add(id)
    }
    selection.onSelectedChange(next)
  }

  function toggleRow(id: string) {
    if (!selection) return
    const next = new Set(selection.selected)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    selection.onSelectedChange(next)
  }

  return (
    <div className="space-y-3">
      {(searchable || toolbar || exportName || (selection && selection.selected.size > 0)) && (
        <div className="flex flex-wrap items-center gap-2">
          {searchable && (
            <ListSearch
              value={globalFilter}
              onChange={setGlobalFilter}
              placeholder={searchPlaceholder}
              size="sm"
              containerClassName="w-full sm:w-64"
            />
          )}
          {toolbar}
          {exportName && (
            <Button size="sm" variant="outline" className="ml-auto" disabled={loading || filteredCount === 0} onClick={exportCsv}>
              <Download className="h-3.5 w-3.5" /> Export CSV
            </Button>
          )}
          {selection && selection.selected.size > 0 && (
            <div className="flex flex-1 flex-wrap items-center gap-2 rounded-md border border-[color-mix(in_oklab,var(--color-brand-500)_35%,var(--border))] bg-[color-mix(in_oklab,var(--color-brand-500)_8%,var(--bg-surface))] px-3 py-1.5">
              <span className="text-xs font-semibold text-brand-600 dark:text-brand-400">{selection.selected.size} selected</span>
              {selection.bulkActions?.(Array.from(selection.selected))}
              <Button size="sm" variant="ghost" className="ml-auto text-xs" onClick={() => selection.onSelectedChange(new Set())}>
                <X className="h-3.5 w-3.5" /> Clear
              </Button>
            </div>
          )}
        </div>
      )}

      <div className="overflow-x-auto rounded-lg border border-[var(--border)] bg-[var(--bg-surface)]">
        <table className="w-full text-sm">
          <thead>
            {table.getHeaderGroups().map((hg) => (
              <tr key={hg.id} className="border-b border-[var(--border)] bg-[var(--bg-muted)]/50 backdrop-blur-xs">
                {selection && (
                  <th className="dt-cell w-8 px-3.5 py-2.5">
                    <Checkbox checked={allFilteredSelected} onCheckedChange={toggleAll} aria-label="Select all rows" />
                  </th>
                )}
                {hg.headers.map((header) => {
                  const sortable = header.column.getCanSort()
                  const sorted = header.column.getIsSorted()
                  const ariaSort = sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : undefined
                  // Lower-priority columns (timestamps, secondary ids) collapse below
                  // md instead of forcing the whole table into horizontal scroll on
                  // phones — set via `meta: { hideBelowMd: true }` on the column def.
                  const hideBelowMd = (header.column.columnDef.meta as { hideBelowMd?: boolean } | undefined)?.hideBelowMd
                  return (
                    <th
                      key={header.id}
                      scope="col"
                      aria-sort={ariaSort}
                      className={cn(
                        "dt-cell px-3.5 py-2.5 text-left font-mono text-[11px] font-semibold uppercase tracking-wider text-[var(--text-muted)]",
                        hideBelowMd && "hidden md:table-cell",
                      )}
                    >
                      {header.isPlaceholder ? null : header.column.columnDef.header === "" ? (
                        <span className="sr-only">Actions</span>
                      ) : sortable ? (
                        <button
                          // `uppercase` repeated here: the browser/preflight reset
                          // sets text-transform: none on <button>, so the header
                          // cell's own uppercase never reached sortable labels and
                          // "Name" sat next to "SCOPE" in the same row.
                          className="flex items-center gap-1.5 uppercase transition-colors hover:text-[var(--text)]"
                          onClick={header.column.getToggleSortingHandler()}
                          aria-label={`${String(header.column.columnDef.header)}: activate to sort${
                            sorted === "asc" ? ", currently ascending" : sorted === "desc" ? ", currently descending" : ""
                          }`}
                        >
                          {flexRender(header.column.columnDef.header, header.getContext())}
                          {sorted === "asc" && <ArrowUp className="h-3 w-3 text-brand-500" />}
                          {sorted === "desc" && <ArrowDown className="h-3 w-3 text-brand-500" />}
                          {!sorted && <ArrowUpDown className="h-3 w-3 opacity-35" />}
                        </button>
                      ) : (
                        flexRender(header.column.columnDef.header, header.getContext())
                      )}
                    </th>
                  )
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {loading &&
              Array.from({ length: 6 }).map((_, i) => (
                <tr key={`skeleton-${i}`} className="border-b border-[var(--border)] last:border-0">
                  {selection && (
                    <td className="dt-cell px-3.5 py-2.5">
                      <Skeleton className="h-4 w-4" />
                    </td>
                  )}
                  {columns.map((col, c) => (
                    <td key={c} className={cn("dt-cell px-3.5 py-2.5", (col.meta as { hideBelowMd?: boolean } | undefined)?.hideBelowMd && "hidden md:table-cell")}>
                      <Skeleton className="h-4 max-w-32" style={{ width: `${40 + ((i * 13 + c * 29) % 45)}%`, opacity: 1 - i * 0.1 }} />
                    </td>
                  ))}
                </tr>
              ))}
            {!loading &&
              rows.map((row) => {
                const id = selection?.rowId(row.original)
                const checked = id ? selection?.selected.has(id) : false
                return (
                  <tr
                    key={row.id}
                    className={cn(
                      "border-b border-[var(--border)] transition-colors duration-150 last:border-0 hover:bg-[var(--bg-surface-hover)]/70",
                      checked && "bg-[color-mix(in_oklab,var(--color-brand-500)_7%,transparent)]",
                    )}
                  >
                    {selection && id && (
                      <td className="dt-cell px-3.5 py-2.5">
                        <Checkbox checked={checked} onCheckedChange={() => toggleRow(id)} aria-label="Select row" />
                      </td>
                    )}
                    {row.getVisibleCells().map((cell) => (
                      <td
                        key={cell.id}
                        className={cn(
                          "dt-cell px-3.5 py-2.5 align-middle text-xs",
                          (cell.column.columnDef.meta as { hideBelowMd?: boolean } | undefined)?.hideBelowMd && "hidden md:table-cell",
                        )}
                      >
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                )
              })}
            {!loading && rows.length === 0 && (
              <tr>
                <td colSpan={columns.length + (selection ? 1 : 0)} className="px-3.5 py-12 text-center text-sm text-[var(--text-muted)]">
                  {emptyMessage}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* Shown whenever there is more than the smallest page of rows, so the
          page-size control stays reachable after switching to a size that
          fits everything on one page. */}
      {!loading && filteredCount > Math.min(pageSize, pageSizeOptions[0]) && (
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-[var(--border)] bg-[var(--bg-surface)] px-3 py-2 text-xs text-[var(--text-muted)]">
          <span className="tabular">
            Page {pageIndex + 1} of {Math.max(1, pageCount)} ·{" "}
            {filteredCount === data.length ? `${data.length} rows` : `${filteredCount} of ${data.length} rows`}
          </span>
          <div className="flex items-center gap-2">
            <span className="hidden sm:inline">Rows per page</span>
            <Select value={String(currentPageSize)} onValueChange={(v) => table.setPageSize(Number(v))}>
              <SelectTrigger className="h-8 w-[4.5rem] px-2 text-xs" aria-label="Rows per page">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {pageSizeOptions.map((n) => (
                  <SelectItem key={n} value={String(n)}>{n}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <div className="flex gap-1">
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label="Previous page"
                className={cn(!table.getCanPreviousPage() && "opacity-40")}
                disabled={!table.getCanPreviousPage()}
                onClick={() => table.previousPage()}
              >
                <ChevronLeft className="h-4 w-4" />
              </Button>
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label="Next page"
                className={cn(!table.getCanNextPage() && "opacity-40")}
                disabled={!table.getCanNextPage()}
                onClick={() => table.nextPage()}
              >
                <ChevronRight className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
