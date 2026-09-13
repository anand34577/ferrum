import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Play, RotateCw, ShieldCheck, Square, Trash2, Upload } from "lucide-react"
import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { api, ApiError, type NodeCertificate, type NodeDNSConfig, type NodeHosts, type NodeService, type NodeTimeInfo } from "@/lib/api"

// Services critical to reachability/cluster membership — restarting or
// stopping these can disconnect this app from the node or split the cluster,
// unlike PVE's own API (which has no such special-casing) the UI warns loudly.
const CRITICAL_SERVICES = new Set(["pveproxy", "pvedaemon", "pve-cluster", "corosync"])

interface NodeSystemPanelProps {
  connId: string
  node: string
}

/** DNS resolver, timezone, /etc/hosts, and TLS certificates for one node —
 * grouped together since they're all one-off host config an admin visits
 * rarely, unlike the metrics/storage/disks tabs this sits alongside. */
export function NodeSystemPanel({ connId, node }: NodeSystemPanelProps) {
  const base = `/connections/${connId}/nodes/${node}`
  const queryClient = useQueryClient()
  const confirm = useConfirm()

  // --- DNS ---
  const dnsQuery = useQuery({ queryKey: ["node-dns", connId, node], queryFn: () => api.get<NodeDNSConfig>(`${base}/dns`) })
  const [dnsForm, setDnsForm] = useState<NodeDNSConfig>({})
  useEffect(() => {
    if (dnsQuery.data) setDnsForm(dnsQuery.data)
  }, [dnsQuery.data])
  const saveDns = useMutation({
    mutationFn: () => api.put(`${base}/dns`, dnsForm),
    onSuccess: () => {
      toast.success("DNS config updated")
      queryClient.invalidateQueries({ queryKey: ["node-dns", connId, node] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to update DNS"),
  })

  // --- Time ---
  const timeQuery = useQuery({ queryKey: ["node-time", connId, node], queryFn: () => api.get<NodeTimeInfo>(`${base}/time`) })
  const [timezone, setTimezone] = useState("")
  useEffect(() => {
    if (timeQuery.data) setTimezone(timeQuery.data.timezone)
  }, [timeQuery.data])
  const saveTimezone = useMutation({
    mutationFn: () => api.put(`${base}/time`, { timezone }),
    onSuccess: () => {
      toast.success("Timezone updated")
      queryClient.invalidateQueries({ queryKey: ["node-time", connId, node] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to update timezone"),
  })

  // --- Hosts ---
  const hostsQuery = useQuery({ queryKey: ["node-hosts", connId, node], queryFn: () => api.get<NodeHosts>(`${base}/hosts`) })
  const [hostsData, setHostsData] = useState("")
  useEffect(() => {
    if (hostsQuery.data) setHostsData(hostsQuery.data.data)
  }, [hostsQuery.data])
  const saveHosts = useMutation({
    mutationFn: () => api.put(`${base}/hosts`, { data: hostsData, digest: hostsQuery.data?.digest }),
    onSuccess: () => {
      toast.success("/etc/hosts updated")
      queryClient.invalidateQueries({ queryKey: ["node-hosts", connId, node] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to update hosts file"),
  })

  // --- Services ---
  const servicesQuery = useQuery({ queryKey: ["node-services", connId, node], queryFn: () => api.get<NodeService[]>(`${base}/services`) })
  const [pendingService, setPendingService] = useState<string | null>(null)
  const serviceAction = useMutation({
    mutationFn: ({ service, action }: { service: string; action: string }) => api.post(`${base}/services/${service}/${action}`),
    onMutate: ({ service }) => setPendingService(service),
    onSuccess: (_data, { service, action }) => {
      toast.success(`${service}: ${action} sent`)
      queryClient.invalidateQueries({ queryKey: ["node-services", connId, node] })
    },
    onError: (err, { service, action }) =>
      toast.error(err instanceof ApiError ? err.message : `Failed to ${action} ${service}`),
    onSettled: () => setPendingService(null),
  })

  async function runServiceAction(service: string, action: "start" | "stop" | "restart" | "reload") {
    if (action === "restart" || action === "stop") {
      const critical = CRITICAL_SERVICES.has(service)
      const ok = await confirm({
        title: `${action === "restart" ? "Restart" : "Stop"} ${service}?`,
        description: critical
          ? `${action === "restart" ? "Restarting" : "Stopping"} ${service} may disconnect this app from the node and disrupt cluster membership.`
          : `This will ${action} the ${service} service on ${node}.`,
        confirmLabel: action === "restart" ? "Restart" : "Stop",
        destructive: true,
      })
      if (!ok) return
    }
    serviceAction.mutate({ service, action })
  }

  // --- Certificates ---
  const certsQuery = useQuery({ queryKey: ["node-certificates", connId, node], queryFn: () => api.get<NodeCertificate[]>(`${base}/certificates`) })
  const [certPem, setCertPem] = useState("")
  const [keyPem, setKeyPem] = useState("")
  const uploadCert = useMutation({
    mutationFn: () => api.post(`${base}/certificates`, { certificate: certPem, key: keyPem || undefined, force: true }),
    onSuccess: () => {
      toast.success("Certificate uploaded")
      setCertPem("")
      setKeyPem("")
      queryClient.invalidateQueries({ queryKey: ["node-certificates", connId, node] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Certificate upload failed"),
  })
  const deleteCert = useMutation({
    mutationFn: () => api.delete(`${base}/certificates`),
    onSuccess: () => {
      toast.success("Custom certificate removed")
      queryClient.invalidateQueries({ queryKey: ["node-certificates", connId, node] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to remove certificate"),
  })
  const orderAcme = useMutation({
    mutationFn: () => api.post(`${base}/certificates/acme`),
    onSuccess: () => toast.success("ACME certificate order started — this node's pveproxy restarts once it completes"),
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "ACME order failed — configure an ACME account/domain on this node first"),
  })
  const revokeAcme = useMutation({
    mutationFn: () => api.delete(`${base}/certificates/acme`),
    onSuccess: () => {
      toast.success("ACME certificate revoked")
      queryClient.invalidateQueries({ queryKey: ["node-certificates", connId, node] })
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "ACME revoke failed"),
  })

  async function revokeAcmeCert() {
    const ok = await confirm({
      title: "Revoke ACME certificate?",
      description: "Tells the CA to revoke it and reverts this node's pveproxy to its self-signed certificate.",
      confirmLabel: "Revoke",
    })
    if (ok) revokeAcme.mutate()
  }

  async function removeCert() {
    const ok = await confirm({
      title: "Remove custom certificate?",
      description: "pveproxy reverts to its self-signed certificate for this node.",
      confirmLabel: "Remove",
    })
    if (ok) deleteCert.mutate()
  }

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Card>
        <CardHeader><CardTitle className="text-sm">DNS</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          {dnsQuery.isLoading ? (
            <Skeleton className="h-24" />
          ) : (
            <>
              <div className="space-y-1.5">
                <Label>Search domain</Label>
                <Input value={dnsForm.search ?? ""} onChange={(e) => setDnsForm((f) => ({ ...f, search: e.target.value }))} />
              </div>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
                {(["dns1", "dns2", "dns3"] as const).map((k) => (
                  <div key={k} className="space-y-1.5">
                    <Label>{k.toUpperCase()}</Label>
                    <Input value={dnsForm[k] ?? ""} onChange={(e) => setDnsForm((f) => ({ ...f, [k]: e.target.value }))} />
                  </div>
                ))}
              </div>
              <Button size="sm" loading={saveDns.isPending} onClick={() => saveDns.mutate()}>
                Save DNS
              </Button>
            </>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle className="text-sm">Time</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          {timeQuery.isLoading ? (
            <Skeleton className="h-16" />
          ) : (
            <>
              <p className="text-xs text-[var(--text-muted)]">
                Node clock: {timeQuery.data ? new Date(timeQuery.data.localtime * 1000).toLocaleString() : "-"}
              </p>
              <div className="space-y-1.5">
                <Label>Timezone (e.g. UTC, America/New_York)</Label>
                <div className="flex gap-2">
                  <Input value={timezone} onChange={(e) => setTimezone(e.target.value)} />
                  <Button size="sm" disabled={!timezone || saveTimezone.isPending} onClick={() => saveTimezone.mutate()}>
                    Save
                  </Button>
                </div>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      <Card className="lg:col-span-2">
        <CardHeader><CardTitle className="text-sm">/etc/hosts</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          {hostsQuery.isLoading ? (
            <Skeleton className="h-32" />
          ) : (
            <>
              <Textarea className="text-xs" rows={6} value={hostsData} onChange={(e) => setHostsData(e.target.value)} />
              <Button size="sm" loading={saveHosts.isPending} onClick={() => saveHosts.mutate()}>
                Save hosts file
              </Button>
            </>
          )}
        </CardContent>
      </Card>

      <Card className="lg:col-span-2">
        <CardHeader><CardTitle className="text-sm">Services</CardTitle></CardHeader>
        <CardContent className="space-y-1.5">
          {servicesQuery.isLoading ? (
            <Skeleton className="h-32" />
          ) : (
            (servicesQuery.data ?? []).map((svc) => {
              const running = svc.state === "running"
              const busy = serviceAction.isPending && pendingService === svc.service
              return (
                <div key={svc.service} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                  <div className="min-w-0">
                    <p className="flex items-center gap-1.5 truncate font-medium">
                      {svc.name || svc.service}
                      {CRITICAL_SERVICES.has(svc.service) && <Badge variant="warn">critical</Badge>}
                    </p>
                    <p className="truncate text-xs text-[var(--text-muted)]">{svc.desc || svc.service}</p>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge variant={running ? "ok" : "default"}>{svc.state}</Badge>
                    <Button size="sm" variant="secondary" disabled={running || busy} onClick={() => runServiceAction(svc.service, "start")}>
                      <Play className="h-3.5 w-3.5" /> Start
                    </Button>
                    <Button size="sm" variant="secondary" disabled={busy} onClick={() => runServiceAction(svc.service, "restart")}>
                      <RotateCw className="h-3.5 w-3.5" /> Restart
                    </Button>
                    <Button size="sm" variant="secondary" disabled={busy} onClick={() => runServiceAction(svc.service, "reload")}>
                      Reload
                    </Button>
                    <Button size="sm" variant="destructive" disabled={!running || busy} onClick={() => runServiceAction(svc.service, "stop")}>
                      <Square className="h-3.5 w-3.5" /> Stop
                    </Button>
                  </div>
                </div>
              )
            })
          )}
          {servicesQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No services reported.</p>}
        </CardContent>
      </Card>

      <Card className="lg:col-span-2">
        <CardHeader><CardTitle className="text-sm">Certificates</CardTitle></CardHeader>
        <CardContent className="space-y-3">
          {certsQuery.isLoading ? (
            <Skeleton className="h-20" />
          ) : (
            <div className="space-y-1.5">
              {(certsQuery.data ?? []).map((c) => (
                <div key={c.filename} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                  <div className="min-w-0">
                    <p className="flex items-center gap-1.5 truncate font-medium"><ShieldCheck className="h-3.5 w-3.5 shrink-0" /> {c.filename}</p>
                    <p className="truncate text-xs text-[var(--text-muted)]">{c.subject || c.issuer || "-"}</p>
                  </div>
                  {c.notafter && (
                    // Wall-clock at paint is intentional: expiry is day-granular and
                    // the panel re-renders on every certificates refetch, so a
                    // mounted badge never needs to re-evaluate on a timer.
                    // eslint-disable-next-line react/purity
                    <Badge variant={c.notafter * 1000 < Date.now() ? "error" : "default"}>
                      Expires {new Date(c.notafter * 1000).toLocaleDateString()}
                    </Badge>
                  )}
                </div>
              ))}
              {certsQuery.data?.length === 0 && <p className="text-sm text-[var(--text-muted)]">No certificates reported.</p>}
            </div>
          )}

          <div className="flex flex-wrap gap-2 border-t border-[var(--border)] pt-3">
            <Button size="sm" variant="secondary" disabled={orderAcme.isPending} onClick={() => orderAcme.mutate()}>
              Order ACME certificate
            </Button>
            <Button size="sm" variant="destructive" disabled={revokeAcme.isPending} onClick={() => revokeAcmeCert()}>
              Revoke ACME certificate
            </Button>
            <Button size="sm" variant="destructive" disabled={deleteCert.isPending} onClick={() => removeCert()}>
              <Trash2 className="h-3.5 w-3.5" /> Remove custom certificate
            </Button>
          </div>

          <div className="space-y-1.5 border-t border-[var(--border)] pt-3">
            <Label>Upload custom certificate (PEM)</Label>
            <Textarea
              className="text-xs"
              rows={4}
              placeholder="-----BEGIN CERTIFICATE-----"
              value={certPem}
              onChange={(e) => setCertPem(e.target.value)}
            />
            <Textarea
              className="text-xs"
              rows={4}
              placeholder="-----BEGIN PRIVATE KEY----- (leave blank to keep the existing key)"
              value={keyPem}
              onChange={(e) => setKeyPem(e.target.value)}
            />
            <Button size="sm" disabled={!certPem} loading={uploadCert.isPending} onClick={() => uploadCert.mutate()}>
              {!uploadCert.isPending && <Upload className="h-3.5 w-3.5" />} Upload
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
