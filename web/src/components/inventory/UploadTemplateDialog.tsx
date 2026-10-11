import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Upload } from "lucide-react"
import { useRef, useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { FormError } from "@/components/ui/form-error"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api, ApiError, notifyUnauthorized, type ClusterResource, type Storage } from "@/lib/api"
import { FileDropzone } from "@/components/ui/file-dropzone"

// Multipart file upload bypasses api.ts's request() helper (it always sets
// Content-Type: application/json when a body is present) — the browser must
// set the multipart boundary itself.
//
// XMLHttpRequest rather than fetch: fetch still has no upload-progress event,
// and an ISO is routinely several gigabytes — a spinner with no percentage
// for ten minutes is indistinguishable from a hung upload.
function uploadFile(path: string, form: FormData, onProgress: (pct: number) => void, signal: AbortSignal): Promise<{ upid: string }> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open("POST", `/api/v1${path}`)
    xhr.withCredentials = true
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress((e.loaded / e.total) * 100)
    }
    xhr.onload = () => {
      let data: { upid?: string; error?: unknown; code?: string } | undefined
      try {
        data = JSON.parse(xhr.responseText)
      } catch {
        data = undefined
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(data as { upid: string })
        return
      }
      if (xhr.status === 401) notifyUnauthorized()
      reject(
        new ApiError(
          xhr.status,
          (typeof data?.error === "string" && data.error) || xhr.statusText || `Request failed (HTTP ${xhr.status})`,
          data?.code,
        ),
      )
    }
    xhr.onerror = () => reject(new Error("The upload was interrupted — check your connection and try again."))
    xhr.onabort = () => reject(new DOMException("Upload cancelled", "AbortError"))
    signal.addEventListener("abort", () => xhr.abort())
    xhr.send(form)
  })
}

const ACCEPT: Record<"iso" | "vztmpl", string> = {
  iso: ".iso,.img",
  vztmpl: ".tar.gz,.tar.xz,.tar.zst,.tgz",
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
  const [progress, setProgress] = useState<number | undefined>(undefined)
  const abortRef = useRef<AbortController | null>(null)

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
      abortRef.current = new AbortController()
      setProgress(0)
      return uploadFile(`/connections/${connId}/nodes/${node}/storage/${storage}/upload`, form, setProgress, abortRef.current.signal)
    },
    onSettled: () => {
      setProgress(undefined)
      abortRef.current = null
    },
    onSuccess: () => {
      toast.success(`${content === "iso" ? "ISO" : "Template"} upload started`)
      queryClient.invalidateQueries({ queryKey: ["templates", connId] })
      setOpen(false)
      reset()
    },
    onError: (err) => {
      if (err instanceof DOMException && err.name === "AbortError") return void toast.info("Upload cancelled")
      toast.error(err instanceof Error && err.message ? err.message : "Upload failed")
    },
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
    <Dialog
      open={open}
      onOpenChange={(o) => {
        // Closing mid-upload would otherwise leave a multi-gigabyte transfer
        // running with nothing on screen to show or stop it.
        if (!o && uploadMutation.isPending) return
        setOpen(o)
        if (!o) reset()
      }}
    >
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

        <div className="space-y-4">
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
              <FileDropzone
                file={file}
                onChange={setFile}
                accept={ACCEPT[content]}
                hint={content === "iso" ? "ISO or IMG image" : "Container template archive (.tar.gz, .tar.xz, .tar.zst)"}
                progress={progress}
              />
              <FormError
                message={
                  uploadMutation.error && !(uploadMutation.error instanceof DOMException)
                    ? uploadMutation.error.message || "Upload failed"
                    : undefined
                }
              />
              <DialogFooter>
                {uploadMutation.isPending && (
                  <Button variant="ghost" onClick={() => abortRef.current?.abort()}>
                    Cancel upload
                  </Button>
                )}
                <Button disabled={!node || !storage || !file} loading={uploadMutation.isPending} onClick={() => uploadMutation.mutate()}>
                  {!uploadMutation.isPending && <Upload className="h-3.5 w-3.5" />}
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
                <Button disabled={!node || !storage || !url || !filename} loading={downloadMutation.isPending} onClick={() => downloadMutation.mutate()}>
                  {!downloadMutation.isPending && <Upload className="h-3.5 w-3.5" />}
                  Fetch
                </Button>
              </DialogFooter>
            </TabsContent>
          </Tabs>
        </div>
      </DialogContent>
    </Dialog>
  )
}
