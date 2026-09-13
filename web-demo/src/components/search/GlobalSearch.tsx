import { useQuery } from "@tanstack/react-query"
import { Search, Server } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { Dialog, DialogContent } from "@/components/ui/dialog"
import { TypeChip } from "@/components/ui/type-chip"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"

// Mirrors api.searchResult (internal/api/search.go).
interface SearchResult {
  connectionId: string
  connectionName: string
  type: "qemu" | "lxc"
  vmid: number
  name?: string
  node: string
  tags?: string
  status?: string
}

/**
 * Cross-remote global search: a floating trigger plus its own "/" shortcut,
 * self-contained so it can be dropped into any page with one import —
 * separate from the CommandPalette.
 *
 * Results come from GET /api/v1/search, which matches guest name/vmid/tags/
 * node across every configured connection.
 */
export function GlobalSearch() {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [debounced, setDebounced] = useState("")
  const inputRef = useRef<HTMLInputElement>(null)
  const navigate = useNavigate()

  useEffect(() => {
    if (!open) return
    // Focus once the dialog has mounted its content.
    const id = window.setTimeout(() => inputRef.current?.focus(), 0)
    return () => window.clearTimeout(id)
  }, [open])

  useEffect(() => {
    const id = window.setTimeout(() => setDebounced(query.trim()), 200)
    return () => window.clearTimeout(id)
  }, [query])

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      const target = e.target as HTMLElement | null
      const typing = target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)
      if (e.key === "/" && !typing && !open) {
        e.preventDefault()
        setOpen(true)
      }
      if (e.key === "Escape") setOpen(false)
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [open])

  const { data, isFetching } = useQuery({
    queryKey: ["global-search", debounced],
    queryFn: () => api.get<SearchResult[]>(`/search?q=${encodeURIComponent(debounced)}`),
    enabled: open && debounced.length > 0,
    staleTime: 5_000,
  })

  function goTo(r: SearchResult) {
    setOpen(false)
    setQuery("")
    // Inventory is the one page that already knows how to deep-link to a
    // single guest across any connection.
    navigate(`/inventory?connection=${encodeURIComponent(r.connectionId)}&vmid=${r.vmid}`)
  }

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="inline-flex h-8 items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--bg-surface)] px-2.5 text-xs text-[var(--text-muted)] transition-colors hover:border-[var(--border-strong)] hover:text-[var(--text)]"
        aria-label="Search guests across every connection"
      >
        <Search className="h-3.5 w-3.5" />
        <span>Search fleet</span>
        <kbd className="rounded border border-[var(--border)] bg-[var(--bg-muted)] px-1 font-mono text-[10px]">/</kbd>
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="top-[20%] max-w-xl translate-y-0 p-0">
          <div className="flex items-center gap-2 border-b border-[var(--border)] px-4 py-3">
            <Search className="h-4 w-4 shrink-0 text-[var(--text-muted)]" />
            <input
              ref={inputRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search by name, VMID, tag, or node…"
              className="w-full bg-transparent font-mono text-sm text-[var(--text)] placeholder:text-[var(--text-faint)] focus:outline-none"
            />
          </div>

          <div className="max-h-[60vh] overflow-y-auto p-1.5">
            {debounced.length === 0 ? (
              <p className="px-3 py-8 text-center text-xs text-[var(--text-muted)]">
                Start typing to search every connected cluster and server at once.
              </p>
            ) : isFetching && !data ? (
              <p className="px-3 py-8 text-center text-xs text-[var(--text-muted)]">Searching…</p>
            ) : !data || data.length === 0 ? (
              <p className="px-3 py-8 text-center text-xs text-[var(--text-muted)]">No guests match "{debounced}".</p>
            ) : (
              <ul>
                {data.map((r) => (
                  <li key={`${r.connectionId}-${r.type}-${r.vmid}`}>
                    <button
                      type="button"
                      onClick={() => goTo(r)}
                      className="flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-sm transition-colors hover:bg-[var(--bg-surface-hover)]"
                    >
                      <TypeChip type={r.type} />
                      <span className="min-w-0 flex-1 truncate">{r.name || `#${r.vmid}`}</span>
                      <span className="shrink-0 font-mono text-xs text-[var(--text-faint)]">#{r.vmid}</span>
                      <span className="shrink-0 truncate text-xs text-[var(--text-muted)]">
                        {r.connectionName} · {r.node}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="flex items-center justify-between border-t border-[var(--border)] px-4 py-2 text-[10px] text-[var(--text-faint)]">
            <span className="flex items-center gap-1">
              <Server className={cn("h-3 w-3")} /> Searches every configured connection
            </span>
            <span>Esc to close</span>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
