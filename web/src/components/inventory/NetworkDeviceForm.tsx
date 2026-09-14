import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { parsePropertyString, stringifyPropertyString } from "@/lib/utils"

// QEMU's netN key is the NIC model itself ("virtio=AA:BB:...", not
// "model=virtio,mac=AA:BB:..."), so the model has to be found by checking
// which of these known model names is present as a key, not read off a
// fixed key name the way every other field here is.
const QEMU_NIC_MODELS = ["virtio", "e1000", "e1000e", "rtl8139", "vmxnet3"]

interface NetworkDeviceFormProps {
  deviceKey: string // "net0", "net1", ...
  value: string // raw PVE property-string value
  guestType: "qemu" | "lxc"
  saving: boolean
  onSave: (newValue: string) => void
  onCancel: () => void
}

// Structured editor for one netN hardware line — Proxmox's own UI shows
// this as separate fields (bridge, VLAN, IP, gateway, firewall, ...), not
// one raw "name=eth0,bridge=vmbr0,ip=..." string; editing it as a single
// text blob is exactly the UX gap this closes. Fields this form doesn't
// model (hwaddr, type, queues, ...) round-trip untouched via `rest`.
export function NetworkDeviceForm({ deviceKey, value, guestType, saving, onSave, onCancel }: NetworkDeviceFormProps) {
  const [fields] = useState(() => parsePropertyString(value))
  const modelKey = guestType === "qemu" ? Object.keys(fields).find((k) => QEMU_NIC_MODELS.includes(k)) : undefined

  const [bridge, setBridge] = useState(fields.bridge ?? "")
  const [tag, setTag] = useState(fields.tag ?? "")
  const [firewall, setFirewall] = useState(fields.firewall === "1")
  const [rate, setRate] = useState(fields.rate ?? "")
  const [mtu, setMtu] = useState(fields.mtu ?? "")
  // LXC-only
  const [name, setName] = useState(fields.name ?? "eth0")
  const [ip, setIp] = useState(fields.ip ?? "")
  const [gw, setGw] = useState(fields.gw ?? "")
  const [ip6, setIp6] = useState(fields.ip6 ?? "")
  const [gw6, setGw6] = useState(fields.gw6 ?? "")
  // QEMU-only
  const [model, setModel] = useState(modelKey ?? "virtio")
  const [mac, setMac] = useState(modelKey ? fields[modelKey] : "")

  // Fields this form doesn't surface its own input for (hwaddr, type, or
  // any key PVE might add that isn't modeled above) — shown as a hint
  // rather than silently hidden, since they're preserved, not dropped.
  const modeledKeys = new Set([
    "bridge", "tag", "firewall", "rate", "mtu",
    ...(guestType === "lxc" ? ["name", "ip", "gw", "ip6", "gw6"] : QEMU_NIC_MODELS),
  ])
  const hasUnmodeledFields = Object.keys(fields).some((k) => !modeledKeys.has(k))

  function save() {
    const rest = { ...fields }
    delete rest.bridge
    delete rest.tag
    delete rest.firewall
    delete rest.rate
    delete rest.mtu
    if (guestType === "lxc") {
      delete rest.name
      delete rest.ip
      delete rest.gw
      delete rest.ip6
      delete rest.gw6
      onSave(
        stringifyPropertyString({
          ...rest,
          name,
          bridge,
          ...(tag ? { tag } : {}),
          firewall: firewall ? "1" : "0",
          ...(rate ? { rate } : {}),
          ...(mtu ? { mtu } : {}),
          ...(ip ? { ip } : {}),
          ...(gw ? { gw } : {}),
          ...(ip6 ? { ip6 } : {}),
          ...(gw6 ? { gw6 } : {}),
        }),
      )
    } else {
      for (const m of QEMU_NIC_MODELS) delete rest[m]
      onSave(
        stringifyPropertyString({
          ...rest,
          [model]: mac,
          bridge,
          ...(tag ? { tag } : {}),
          firewall: firewall ? "1" : "0",
          ...(rate ? { rate } : {}),
          ...(mtu ? { mtu } : {}),
        }),
      )
    }
  }

  return (
    <div className="space-y-3 rounded-md border border-[var(--border)] bg-[var(--bg-surface)] px-3 py-2.5 text-sm">
      <div className="flex items-center gap-2">
        <span className="font-mono text-xs text-[var(--text-muted)]">{deviceKey}</span>
        {hasUnmodeledFields && <span className="text-xs text-[var(--text-muted)]">— other fields on this device are kept as-is</span>}
      </div>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        {guestType === "lxc" ? (
          <div className="space-y-1">
            <Label className="text-xs">Interface name</Label>
            <Input value={name} onChange={(e) => setName(e.target.value)} className="h-8 text-xs" />
          </div>
        ) : (
          <>
            <div className="space-y-1">
              <Label className="text-xs">Model</Label>
              <Select value={model} onValueChange={setModel}>
                <SelectTrigger className="h-8 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {QEMU_NIC_MODELS.map((m) => <SelectItem key={m} value={m}>{m}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1">
              <Label className="text-xs">MAC address</Label>
              <Input value={mac} onChange={(e) => setMac(e.target.value)} placeholder="auto-generated" className="h-8 font-mono text-xs" />
            </div>
          </>
        )}
        <div className="space-y-1">
          <Label className="text-xs">Bridge</Label>
          <Input value={bridge} onChange={(e) => setBridge(e.target.value)} placeholder="vmbr0" className="h-8 text-xs" />
        </div>
        <div className="space-y-1">
          <Label className="text-xs">VLAN tag</Label>
          <Input value={tag} onChange={(e) => setTag(e.target.value)} placeholder="none" className="h-8 text-xs" />
        </div>
        {guestType === "lxc" && (
          <>
            <div className="space-y-1">
              <Label className="text-xs">IPv4</Label>
              <Input value={ip} onChange={(e) => setIp(e.target.value)} placeholder="dhcp or 10.0.0.5/24" className="h-8 font-mono text-xs" />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">Gateway</Label>
              <Input value={gw} onChange={(e) => setGw(e.target.value)} placeholder="10.0.0.1" className="h-8 font-mono text-xs" />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">IPv6</Label>
              <Input value={ip6} onChange={(e) => setIp6(e.target.value)} placeholder="auto, dhcp, or CIDR" className="h-8 font-mono text-xs" />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">Gateway (IPv6)</Label>
              <Input value={gw6} onChange={(e) => setGw6(e.target.value)} className="h-8 font-mono text-xs" />
            </div>
          </>
        )}
        <div className="space-y-1">
          <Label className="text-xs">Rate limit (MB/s)</Label>
          <Input value={rate} onChange={(e) => setRate(e.target.value)} placeholder="unlimited" className="h-8 text-xs" />
        </div>
        <div className="space-y-1">
          <Label className="text-xs">MTU</Label>
          <Input value={mtu} onChange={(e) => setMtu(e.target.value)} placeholder="1500" className="h-8 text-xs" />
        </div>
        <label className="flex items-center gap-2 self-end pb-1.5 text-xs">
          <Checkbox checked={firewall} onCheckedChange={(v) => setFirewall(v === true)} />
          Firewall
        </label>
      </div>
      <div className="flex gap-2">
        <Button size="sm" loading={saving} onClick={save}>Save</Button>
        <Button size="sm" variant="ghost" onClick={onCancel}>Cancel</Button>
      </div>
    </div>
  )
}
