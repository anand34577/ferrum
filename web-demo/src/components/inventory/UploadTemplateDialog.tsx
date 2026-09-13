import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, Upload } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { FormError } from "@/components/ui/form-error"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api, ApiError, type ClusterResource, type Storage } from "@/lib/api"

// Multipart file upload bypasses api.ts's request() helper (it always sets
// Content-Type: application/json when a body is present) — the browser must
// set the multipart boundary itself.
async function uploadFile(path: string, form: FormData): Promise<{ upid: string }> {
  const res = await fetch(`/api/v1${path}`, { method: "POST", body: form, credentials: "include" })
  const data = await res.json().catch(() => undefined)
  if (!res.ok) {
    throw new ApiError(
      res.status,
      (typeof data?.error === "string" && data.error) || res.statusText || `Request failed (HTTP ${res.status})`,
      data?.code,
    )
  }
  return data
}

interface UploadTemplateDialogProps {
  connId: string
  nodes: ClusterResource[]
  content: "iso" | "vztmpl"
}

// Fills the confirmed gap from the Proxmox-API audit: there was no way to
// get an ISO or container template into a storage from this app at all — the
// create-guest wizard just told users to "download one in Proxmox first".
export function UploadTemplateDialog({ connId, nodes, content }: UploadTemplateDialogProps) {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [node, setNode] = useState("")
  const [storage, setStorage] = useState("")
  const [file, setFile] = useState<File | null>(null)
  const [url, setUrl] = useState("")
  const [filename, setFilename] = useState("")

  const storagesQuery = useQuery({
    queryKey: ["node-storage", connId, node],
    queryFn: () => api.get<Storage[]>(`/connections/${connId}/nodes/${node}/storage`),
    enabled: open && !!node,
  })
  const eligibleStorages = (storagesQuery.data ?? []).filter((s) => s.content?.split(",").includes(content))

  function reset() {
    setNode("")
    setStorage("")
    setFile(null)
    setUrl("")
    setFilename("")
  }

  const uploadMutation = useMutation({
    mutationFn: async () => {
      if (!file) throw new Error("Choose a file")
      const form = new FormData()
      form.set("content", content)
      form.set("file", file)
      return uploadFile(`/connections/${connId}/nodes/${node}/storage/${storage}/upload`, form)
    },
    onSuccess: () => {
      toast.success(`${content === "iso" ? "ISO" : "Template"} upload started`)
      queryClient.invalidateQueries({ queryKey: ["templates", connId] })
      setOpen(false)
      reset()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Upload failed"),
  })

  const downloadMutation = useMutation({
    mutationFn: () =>
      api.post(`/connections/${connId}/nodes/${node}/storage/${storage}/download-url`, {
        content,
        filename,
        url,
      }),
    onSuccess: () => {
      toast.success(`Fetching from ${url}`)
      queryClient.invalidateQueries({ queryKey: ["templates", connId] })
      setOpen(false)
      reset()
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Download failed"),
  })

  const label = content === "iso" ? "ISO" : "container template"

  return (
    <Dialog open={open} onOpenChange={(o) => { setOpen(o); if (!o) reset() }}>
      <DialogTrigger asChild>
        <Button type="button" variant="outline" size="sm">
          <Upload className="h-3.5 w-3.5" /> Add {label}
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add {label}</DialogTitle>
          <DialogDescription>Upload a file, or have the node fetch it directly from a URL.</DialogDescription>
        </DialogHeader>

        <div className="space-y-1.5">
          <Label>Node</Label>
          <Select value={node} onValueChange={(v) => { setNode(v); setStorage("") }}>
            <SelectTrigger><SelectValue placeholder="Select..." /></SelectTrigger>
            <SelectContent>
              {nodes.map((n) => <SelectItem key={n.node} value={n.node!}>{n.node}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label>Storage</Label>
          <Select value={storage} onValueChange={setStorage} disabled={!node}>
            <SelectTrigger><SelectValue placeholder={node ? "Select..." : "Choose a node first"} /></SelectTrigger>
            <SelectContent>
              {eligibleStorages.map((s) => <SelectItem key={s.storage} value={s.storage}>{s.storage}</SelectItem>)}
              {node && eligibleStorages.length === 0 && !storagesQuery.isLoading && (
                <SelectItem value="__none__" disabled>No storage on this node accepts {label}s</SelectItem>
              )}
            </SelectContent>
          </Select>
        </div>

        <Tabs defaultValue="upload">
          <TabsList>
            <TabsTrigger value="upload">Upload file</TabsTrigger>
            <TabsTrigger value="url">Download by URL</TabsTrigger>
          </TabsList>
          <TabsContent value="upload" className="space-y-3">
            <div className="space-y-1.5">
              <Label>File</Label>
              <Input type="file" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
            </div>
            <FormError message={uploadMutation.error instanceof ApiError ? uploadMutation.error.message : uploadMutation.error ? "Upload failed" : undefined} />
            <DialogFooter>
              <Button disabled={!node || !storage || !file || uploadMutation.isPending} onClick={() => uploadMutation.mutate()}>
                {uploadMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
                Upload
              </Button>
            </DialogFooter>
          </TabsContent>
          <TabsContent value="url" className="space-y-3">
            <div className="space-y-1.5">
              <Label>URL</Label>
              <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/image.iso" />
            </div>
            <div className="space-y-1.5">
              <Label>Filename</Label>
              <Input value={filename} onChange={(e) => setFilename(e.target.value)} placeholder="image.iso" />
            </div>
            <FormError message={downloadMutation.error instanceof ApiError ? downloadMutation.error.message : downloadMutation.error ? "Download failed" : undefined} />
            <DialogFooter>
              <Button disabled={!node || !storage || !url || !filename || downloadMutation.isPending} onClick={() => downloadMutation.mutate()}>
                {downloadMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
                Fetch
              </Button>
            </DialogFooter>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
