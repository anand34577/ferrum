import { Check, Monitor, Moon, Sun } from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useTheme, type Accent, type Look, type ThemePreference } from "@/lib/theme"
import { cn } from "@/lib/utils"

/**
 * Theme picker — Light / Dark / System. The choice is saved to the signed-in
 * user's account in the database (lib/theme.tsx handles the sync), so it
 * follows the user to any browser they sign in from; this browser only caches
 * it for a flash-free first paint.
 */

const OPTIONS: { value: ThemePreference; label: string; hint: string; icon: typeof Sun }[] = [
  { value: "light", label: "Light", hint: "Bright surfaces, for well-lit rooms", icon: Sun },
  { value: "dark", label: "Dark", hint: "The control-room look", icon: Moon },
  { value: "system", label: "System", hint: "Follow your OS setting automatically", icon: Monitor },
]

function ThemePreview({ variant, active }: { variant: ThemePreference; active: boolean }) {
  const dark = variant === "dark"
  return (
    <div
      aria-hidden
      className={cn(
        "pointer-events-none h-16 w-full overflow-hidden rounded-sm border text-[7px]",
        dark ? "border-[#202632] bg-[#090b0e]" : "border-[#d7dbe2] bg-[#eef0f3]",
        active && "ring-2 ring-[var(--ring)] ring-offset-2 ring-offset-[var(--bg-surface)]",
      )}
    >
      <div className={cn("flex h-2.5 items-center gap-0.5 border-b px-1", dark ? "border-[#202632] bg-[#12151b]" : "border-[#d7dbe2] bg-white")}>
        <span className="h-1 w-1 rounded-full bg-[var(--status-ok)]" />
        <span className={cn("h-1 w-4 rounded-full", dark ? "bg-[#3a414c]" : "bg-[#d7dbe2]")} />
      </div>
      <div className="flex gap-1 p-1">
        <div className={cn("w-3 rounded-sm", dark ? "bg-[#15171b]" : "bg-[#15171b]")} />
        <div className="flex-1 space-y-1">
          <div className="flex gap-1">
            <div className={cn("h-4 flex-1 rounded-sm", dark ? "bg-[#1a1d22]" : "bg-white")} />
            <div className={cn("h-4 flex-1 rounded-sm", dark ? "bg-[#1a1d22]" : "bg-white")} />
            <div className="h-4 flex-1 rounded-sm bg-brand-500" />
          </div>
          <div className={cn("h-1.5 w-2/3 rounded-full", dark ? "bg-[#20242a]" : "bg-[#e4e7ec]")} />
          <div className={cn("h-1.5 w-1/2 rounded-full", dark ? "bg-[#20242a]" : "bg-[#e4e7ec]")} />
        </div>
      </div>
    </div>
  )
}

const LOOKS: { value: Look; label: string; hint: string }[] = [
  { value: "enterprise", label: "Enterprise", hint: "Clean SaaS dashboard — restrained neutrals, soft shadows, one accent" },
  { value: "proxmox", label: "Proxmox-native", hint: "Utilitarian and dense — plain system font, flat bordered panels" },
  { value: "terminal", label: "Terminal", hint: "Quiet and dark — no display face, precision over metaphor" },
]

/** A real, live mockup, not a static illustration: scoping [data-look] to
 * this wrapper means every token inside genuinely repaints, so the preview
 * is never at risk of drifting from what the look actually does. */
function LookPreview({ variant, dark }: { variant: Look; dark: boolean }) {
  return (
    <div
      data-look={variant}
      className={cn("pointer-events-none h-16 w-full overflow-hidden border p-1.5", dark && "dark")}
      style={{ background: "var(--bg)", borderColor: "var(--border)", borderRadius: "var(--radius-lg)" }}
    >
      <div className="flex h-full gap-1.5">
        <div className="w-3 shrink-0" style={{ background: "var(--sidebar-bg, var(--bg-elevated))", borderRadius: "var(--radius-sm)" }} />
        <div className="flex-1 space-y-1">
          <div className="h-2 w-10" style={{ background: "var(--color-brand-500)", borderRadius: "var(--radius-sm)" }} />
          <div
            className="h-7 w-full border"
            style={{ background: "var(--bg-surface)", borderColor: "var(--border)", borderRadius: "var(--radius-md)", boxShadow: "var(--elev-xs)" }}
          />
          <div className="flex gap-1">
            <div className="h-1.5 flex-1" style={{ background: "var(--bg-muted)", borderRadius: "var(--radius-sm)" }} />
            <div className="h-1.5 flex-1" style={{ background: "var(--bg-muted)", borderRadius: "var(--radius-sm)" }} />
          </div>
        </div>
      </div>
    </div>
  )
}

const ACCENTS: { value: Accent; label: string; swatch: string }[] = [
  { value: "oxide", label: "Oxide", swatch: "#bd5a2c" },
  { value: "azure", label: "Azure", swatch: "#3568b8" },
  { value: "verdant", label: "Verdant", swatch: "#268a64" },
  { value: "violet", label: "Violet", swatch: "#6a3fb0" },
  { value: "slate", label: "Slate", swatch: "#5c6e82" },
]

export function AppearanceCard() {
  const { theme, setTheme, effectiveTheme, accent, setAccent, look, setLook } = useTheme()

  return (
    <Card>
      <CardHeader>
        <CardTitle>Appearance</CardTitle>
        <CardDescription>
          Saved to your account — follows you on every device you sign in from. "System" tracks your operating system's light/dark setting.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div>
          <p className="mb-2 text-sm font-medium">Look &amp; feel</p>
          <div className="grid max-w-xl grid-cols-1 gap-3 sm:grid-cols-3" role="radiogroup" aria-label="Look and feel">
            {LOOKS.map((opt) => {
              const active = look === opt.value
              return (
                <button
                  key={opt.value}
                  role="radio"
                  aria-checked={active}
                  onClick={() => setLook(opt.value)}
                  className={cn(
                    "group rounded-lg border p-3 text-left transition-colors",
                    active
                      ? "border-[var(--ring)] bg-[color-mix(in_oklab,var(--color-brand-500)_6%,var(--bg-surface))]"
                      : "border-[var(--border)] hover:border-[var(--border-strong)] hover:bg-[var(--bg-surface-hover)]",
                  )}
                >
                  <LookPreview variant={opt.value} dark={effectiveTheme === "dark"} />
                  <p className="mt-2.5 flex items-center gap-1.5 text-sm font-medium">
                    {opt.label}
                    {active && <span className="ml-auto text-[10px] font-semibold uppercase tracking-wide text-brand-600 dark:text-brand-400">Active</span>}
                  </p>
                  <p className="mt-0.5 text-xs text-[var(--text-muted)]">{opt.hint}</p>
                </button>
              )
            })}
          </div>
        </div>

        <div>
          <p className="mb-2 text-sm font-medium">Theme</p>
          <div className="grid max-w-xl grid-cols-1 gap-3 sm:grid-cols-3" role="radiogroup" aria-label="UI theme">
            {OPTIONS.map((opt) => {
              const active = theme === opt.value
              const Icon = opt.icon
              return (
                <button
                  key={opt.value}
                  role="radio"
                  aria-checked={active}
                  onClick={() => setTheme(opt.value)}
                  className={cn(
                    "group rounded-lg border p-3 text-left transition-colors",
                    active
                      ? "border-[var(--ring)] bg-[color-mix(in_oklab,var(--color-brand-500)_6%,var(--bg-surface))]"
                      : "border-[var(--border)] hover:border-[var(--border-strong)] hover:bg-[var(--bg-surface-hover)]",
                  )}
                >
                  <ThemePreview variant={opt.value} active={false} />
                  <p className="mt-2.5 flex items-center gap-1.5 text-sm font-medium">
                    <Icon className="h-3.5 w-3.5 text-[var(--text-muted)]" /> {opt.label}
                    {active && <span className="ml-auto text-[10px] font-semibold uppercase tracking-wide text-brand-600 dark:text-brand-400">Active</span>}
                  </p>
                  <p className="mt-0.5 text-xs text-[var(--text-muted)]">{opt.hint}</p>
                </button>
              )
            })}
          </div>
        </div>

        <div>
          <p className="mb-2 text-sm font-medium">Accent color</p>
          <div className="flex flex-wrap gap-2.5" role="radiogroup" aria-label="Accent color">
            {ACCENTS.map((a) => {
              const active = accent === a.value
              return (
                <button
                  key={a.value}
                  role="radio"
                  aria-checked={active}
                  aria-label={a.label}
                  onClick={() => setAccent(a.value)}
                  className={cn(
                    "flex h-9 w-9 items-center justify-center rounded-full ring-2 ring-offset-2 ring-offset-[var(--bg-surface)] transition-transform hover:scale-105",
                    active ? "ring-[var(--text)]" : "ring-transparent",
                  )}
                >
                  <span className="flex h-7 w-7 items-center justify-center rounded-full" style={{ background: a.swatch }}>
                    {active && <Check className="h-3.5 w-3.5 text-white" strokeWidth={3} aria-hidden />}
                  </span>
                </button>
              )
            })}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
