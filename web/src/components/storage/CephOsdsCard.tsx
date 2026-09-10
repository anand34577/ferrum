import { HardDrive } from "lucide-react"
import { useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ListSearch } from "@/components/ui/list-search"
import { StatusDot } from "@/components/ui/status-dot"
import type { CephOSD } from "@/lib/api"

/** A large Ceph deployment can carry hundreds of OSDs — past the point where
 * a plain unbounded flex-wrap (no search, no scroll ceiling) is readable or
 * keeps the rest of the page from growing behind it. Same threshold/pattern
 * as every other plain list on this page: a search box past 8 items, and a
 * capped, internally-scrolling container instead of growing the card forever. */
export function CephOsdsCard({ connName, osds }: { connName: string; osds: CephOSD[] }) {
  const [search, setSearch] = useState("")
  const filtered = search
    ? osds.filter((o) => `osd.${o.id} ${o.host ?? ""}`.toLowerCase().includes(search.toLowerCase()))
    : osds

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-2">
        <CardTitle className="flex items-center gap-2">
          <HardDrive className="h-4 w-4" /> Ceph OSDs — {connName}
        </CardTitle>
        {osds.length > 8 && <ListSearch value={search} onChange={setSearch} placeholder="Search OSDs..." className="w-full sm:w-48" />}
      </CardHeader>
      <CardContent>
        <div className={osds.length > 24 ? "flex max-h-72 flex-wrap gap-2 overflow-y-auto" : "flex flex-wrap gap-2"}>
          {filtered.map((osd) => (
            <div key={osd.id} className="flex items-center gap-2 rounded-md bg-[var(--bg-muted)] px-3 py-1.5 text-sm">
              <StatusDot status={osd.up === 1 ? "ok" : "error"} />
              <span className="font-mono text-xs">osd.{osd.id}</span>
              {osd.host && <span className="text-xs text-[var(--text-muted)]">{osd.host}</span>}
              <span className="text-xs text-[var(--text-muted)]">
                {osd.up === 1 ? "up" : "down"} · {osd.in === 1 ? "in" : "out"}
              </span>
            </div>
          ))}
          {filtered.length === 0 && <p className="text-xs text-[var(--text-muted)]">No OSD matches "{search}".</p>}
        </div>
      </CardContent>
    </Card>
  )
}
