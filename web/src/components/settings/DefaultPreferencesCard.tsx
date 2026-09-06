import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ErrorState } from "@/components/ui/error-state"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { api, ApiError } from "@/lib/api"
import type { Accent, Density, Look, ThemePreference } from "@/lib/theme"

interface DefaultPreferences {
  theme: ThemePreference
  accent: Accent
  look: Look
  landingPage: string
  density: Density
}

const LOOKS: { value: Look; label: string }[] = [
  { value: "enterprise", label: "Enterprise" },
  { value: "proxmox", label: "Proxmox-native" },
  { value: "terminal", label: "Terminal" },
  { value: "glassFlightDeck", label: "Glass Flight Deck" },
  { value: "midnight", label: "Midnight" },
  { value: "paper", label: "Paper" },
  { value: "glassmorphism", label: "Glassmorphism" },
  { value: "neumorphism", label: "Neumorphism" },
  { value: "brutalist", label: "Brutalist" },
  { value: "solarized", label: "Solarized" },
  { value: "highContrast", label: "High Contrast" },
  { value: "aurora", label: "Aurora" },
]
const ACCENTS: { value: Accent; label: string }[] = [
  { value: "oxide", label: "Oxide" },
  { value: "azure", label: "Azure" },
  { value: "verdant", label: "Verdant" },
  { value: "violet", label: "Violet" },
  { value: "slate", label: "Slate" },
]
const LANDING_PAGES = [
  { value: "/", label: "Fleet Overview" },
  { value: "/dashboard", label: "Custom Dashboard" },
  { value: "/inventory", label: "Inventory" },
  { value: "/topology", label: "Topology" },
  { value: "/storage", label: "Storage" },
  { value: "/pools", label: "Resource Pools" },
  { value: "/ha", label: "High Availability" },
  { value: "/backups", label: "Backups" },
  { value: "/firewall", label: "Firewall" },
  { value: "/alerts", label: "Alerts" },
  { value: "/tasks", label: "Task Center" },
  { value: "/ai-assistant", label: "AI Assistant" },
]
const DENSITIES: { value: Density; label: string }[] = [
  { value: "comfortable", label: "Comfortable" },
  { value: "compact", label: "Compact" },
]

/**
 * Org-wide defaults a brand-new account starts with, before it has ever
 * saved a preference of its own (see internal/api/settings_defaults.go).
 * Existing users who already picked their own theme/look/landing page are
 * unaffected by changes here.
 */
export function DefaultPreferencesCard() {
  const query = useQuery({
    queryKey: ["admin", "settings", "defaults"],
    queryFn: () => api.get<DefaultPreferences>("/admin/settings/defaults"),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>New account defaults</CardTitle>
        <CardDescription>What a brand-new account starts with — anyone who has already set their own preferences keeps them.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {query.isError ? (
          <ErrorState title="Couldn't load defaults" onRetry={query.refetch} />
        ) : query.isLoading ? (
          <Skeleton className="h-24 w-full" />
        ) : (
          <DefaultPreferencesForm key={JSON.stringify(query.data)} initial={query.data!} />
        )}
      </CardContent>
    </Card>
  )
}

function DefaultPreferencesForm({ initial }: { initial: DefaultPreferences }) {
  const queryClient = useQueryClient()
  const [form, setForm] = useState(initial)

  const save = useMutation({
    mutationFn: () => api.put<DefaultPreferences>("/admin/settings/defaults", form),
    onSuccess: (data) => {
      toast.success("Default preferences saved")
      queryClient.setQueryData(["admin", "settings", "defaults"], data)
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Failed to save defaults"),
  })

  return (
    <>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5">
        <div className="space-y-1.5">
          <Label>Theme</Label>
          <Select value={form.theme} onValueChange={(v) => setForm({ ...form, theme: v as ThemePreference })}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="light">Light</SelectItem>
              <SelectItem value="dark">Dark</SelectItem>
              <SelectItem value="system">System</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label>Accent</Label>
          <Select value={form.accent} onValueChange={(v) => setForm({ ...form, accent: v as Accent })}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              {ACCENTS.map((a) => <SelectItem key={a.value} value={a.value}>{a.label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label>Look &amp; feel</Label>
          <Select value={form.look} onValueChange={(v) => setForm({ ...form, look: v as Look })}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              {LOOKS.map((l) => <SelectItem key={l.value} value={l.value}>{l.label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label>Landing page</Label>
          <Select value={form.landingPage} onValueChange={(v) => setForm({ ...form, landingPage: v })}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              {LANDING_PAGES.map((p) => <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label>Density</Label>
          <Select value={form.density} onValueChange={(v) => setForm({ ...form, density: v as Density })}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              {DENSITIES.map((d) => <SelectItem key={d.value} value={d.value}>{d.label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
      </div>

      <Button size="sm" loading={save.isPending} onClick={() => save.mutate()}>
        Save defaults
      </Button>
    </>
  )
}
