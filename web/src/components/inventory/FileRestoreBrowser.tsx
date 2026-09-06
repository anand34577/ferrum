import { useQuery } from "@tanstack/react-query"
import { Download, File, Folder, FolderOpen } from "lucide-react"
import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type FileRestoreEntry } from "@/lib/api"
import { formatBytes } from "@/lib/utils"

interface FileRestoreBrowserProps {
  connId: string
  node: string
  storage: string
  volume: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** Browses a single PBS backup archive's file tree and downloads individual
 * files/directories (as a zip) — only PBS storage supports this; a local
 * vzdump archive answers with an error, shown as-is. */
export function FileRestoreBrowser({ connId, node, storage, volume, open, onOpenChange }: FileRestoreBrowserProps) {
  const [path, setPath] = useState("/")
  const base = `/connections/${connId}/nodes/${node}/storage/${storage}/file-restore`

  const listQuery = useQuery({
    queryKey: ["file-restore", connId, node, storage, volume, path],
    queryFn: () => api.get<FileRestoreEntry[]>(`${base}?volume=${encodeURIComponent(volume)}&path=${encodeURIComponent(path)}`),
    enabled: open,
    retry: false,
  })

  function downloadUrl(entry: FileRestoreEntry) {
    const q = new URLSearchParams({ volume, path: entry.filepath })
    return `/api/v1${base}/download?${q.toString()}`
  }

  function enterDir(entry: FileRestoreEntry) {
    setPath(entry.filepath)
  }

  const segments = path.split("/").filter(Boolean)

  return (
    <Dialog open={open} onOpenChange={(o) => { onOpenChange(o); if (!o) setPath("/") }}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Browse backup files</DialogTitle>
          <DialogDescription className="truncate font-mono text-xs">{volume}</DialogDescription>
        </DialogHeader>

        <div className="flex flex-wrap items-center gap-1 text-xs text-[var(--text-muted)]">
          <button className="hover:underline" onClick={() => setPath("/")}>root</button>
          {segments.map((seg, i) => (
            <span key={i} className="flex items-center gap-1">
              <span>/</span>
              <button className="hover:underline" onClick={() => setPath("/" + segments.slice(0, i + 1).join("/"))}>{seg}</button>
            </span>
          ))}
        </div>

        {listQuery.isLoading ? (
          <Skeleton className="h-24" />
        ) : listQuery.isError ? (
          <p className="text-sm text-[var(--text-muted)]">
            File-level browsing isn't available for this storage — it's only supported for Proxmox Backup Server (PBS) archives.
          </p>
        ) : (
          <div className="max-h-80 space-y-0.5 overflow-y-auto">
            {(listQuery.data ?? []).map((entry) => (
              <div key={entry.filepath} className="flex items-center justify-between gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-[var(--bg-muted)]">
                {entry.type === "d" ? (
                  <button className="flex min-w-0 flex-1 items-center gap-2 text-left" onClick={() => enterDir(entry)}>
                    <Folder className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" />
                    <span className="truncate">{entry.filepath.split("/").pop()}</span>
                  </button>
                ) : (
                  <span className="flex min-w-0 flex-1 items-center gap-2">
                    <File className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" />
                    <span className="truncate">{entry.filepath.split("/").pop()}</span>
                    {entry.size !== undefined && <span className="shrink-0 text-xs text-[var(--text-muted)]">{formatBytes(entry.size)}</span>}
                  </span>
                )}
                <a href={downloadUrl(entry)} target="_blank" rel="noreferrer">
                  <Button size="icon-sm" variant="ghost" aria-label={`Download ${entry.filepath}`}>
                    {entry.type === "d" ? <FolderOpen className="h-3.5 w-3.5" /> : <Download className="h-3.5 w-3.5" />}
                  </Button>
                </a>
              </div>
            ))}
            {(listQuery.data ?? []).length === 0 && <p className="text-sm text-[var(--text-muted)]">Empty directory.</p>}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
