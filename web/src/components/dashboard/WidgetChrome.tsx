import { ChevronDown, ChevronUp, GripVertical, Settings2, X } from "lucide-react"
import { type ReactNode, useState } from "react"
import { WIDGET_CATALOG, widgetLabel, type SettingField, type WidgetSettings, type WidgetType } from "@/lib/dashboardTypes"
import { useConnections } from "@/lib/fleet"
import { cn } from "@/lib/utils"
import { ErrorBoundary } from "@/components/ui/error-boundary"

interface WidgetChromeProps {
  type: WidgetType
  editing: boolean
  onRemove: () => void
  settings: WidgetSettings
  onSettingsChange: (next: WidgetSettings) => void
  children: ReactNode
  /** Keyboard-accessible reorder fallback for the drag handle — react-grid-layout's
   * drag/resize has no keyboard path, so this is the only way a keyboard or
   * switch-control user can reposition a widget. Omit an end to disable it. */
  onMoveUp?: () => void
  onMoveDown?: () => void
}

/** A failed query must never render identically to "no data yet" — a widget
 * stuck on a broken connection would otherwise look calm forever. Every
 * widget that can fail renders this instead of its normal empty state when
 * `isError` is true. */
export function WidgetError({ message = "Couldn't load this widget's data — check your connection." }: { message?: string }) {
  return <p className="text-sm text-[var(--status-error)]">{message}</p>
}

export function WidgetChrome({ type, editing, onRemove, settings, onSettingsChange, children, onMoveUp, onMoveDown }: WidgetChromeProps) {
  const [settingsOpen, setSettingsOpen] = useState(false)
  const fields = WIDGET_CATALOG.find((w) => w.type === type)?.settingsFields
  const { data: connections } = useConnections()

  function optionsFor(f: SettingField): { value: string; label: string }[] {
    if (f.dynamic !== "connections") return f.options
    return [
      { value: "all", label: "All connections" },
      ...(connections ?? []).map((c) => ({ value: c.id, label: c.name })),
    ]
  }

  return (
    <div className="relative flex h-full flex-col overflow-hidden rounded-lg border border-[var(--border)] bg-[var(--bg-surface)] transition-colors duration-200">
      <div
        className={cn(
          "drag-handle flex shrink-0 items-center gap-2 border-b border-[var(--border)] bg-[var(--bg-muted)]/30 px-3.5 py-2.5",
          editing && "cursor-move",
        )}
      >
        {editing && <GripVertical className="h-3.5 w-3.5 text-[var(--text-muted)]" aria-hidden />}
        <span className="panel-label text-[11px] text-[var(--text)]">{widgetLabel(type)}</span>
        {editing && (onMoveUp || onMoveDown) && (
          <div className="flex items-center">
            {/* Visual size stays 24px but the ::before pseudo expands the hit
                area to the app's 44px touch-target standard (same trick as
                button.tsx's icon sizes). */}
            <button
              onClick={onMoveUp}
              disabled={!onMoveUp}
              className="relative flex h-6 w-6 items-center justify-center rounded-sm text-[var(--text-muted)] transition-colors before:absolute before:-inset-1.5 before:content-[''] hover:bg-[var(--bg-muted)] hover:text-[var(--text)] disabled:pointer-events-none disabled:opacity-30"
              aria-label={`Move ${widgetLabel(type)} widget earlier`}
            >
              <ChevronUp className="h-3.5 w-3.5" />
            </button>
            <button
              onClick={onMoveDown}
              disabled={!onMoveDown}
              className="relative flex h-6 w-6 items-center justify-center rounded-sm text-[var(--text-muted)] transition-colors before:absolute before:-inset-1.5 before:content-[''] hover:bg-[var(--bg-muted)] hover:text-[var(--text)] disabled:pointer-events-none disabled:opacity-30"
              aria-label={`Move ${widgetLabel(type)} widget later`}
            >
              <ChevronDown className="h-3.5 w-3.5" />
            </button>
          </div>
        )}
        {fields && fields.length > 0 && (
          <button
            onClick={() => setSettingsOpen((o) => !o)}
            className={cn(
              "relative ml-auto flex h-6 w-6 items-center justify-center rounded text-[var(--text-muted)] transition-colors before:absolute before:-inset-1.5 before:content-[''] hover:bg-[var(--bg-muted)] hover:text-[var(--text)]",
              settingsOpen && "bg-[var(--bg-muted)] text-[var(--text)]",
            )}
            aria-label={`${widgetLabel(type)} settings`}
            aria-expanded={settingsOpen}
          >
            <Settings2 className="h-3.5 w-3.5" />
          </button>
        )}
        {editing && (
          <button
            onClick={onRemove}
            className={cn(
              "relative flex h-6 w-6 items-center justify-center rounded text-[var(--text-muted)] transition-colors before:absolute before:-inset-1.5 before:content-[''] hover:bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)] hover:text-[var(--status-error)]",
              !(fields && fields.length > 0) && "ml-auto",
            )}
            aria-label="Remove widget"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
      </div>

      {settingsOpen && fields && (
        <div className="shrink-0 space-y-2 border-b border-[var(--border)] bg-[var(--bg-muted)] px-3 py-2">
          {fields.map((f) => (
            <label key={f.key} className="flex items-center justify-between gap-2 text-xs">
              <span className="text-[var(--text-muted)]">{f.label}</span>
              <select
                value={settings[f.key] ?? (f.dynamic === "connections" ? "all" : f.options[0]?.value)}
                onChange={(e) => onSettingsChange({ ...settings, [f.key]: e.target.value })}
                className="max-w-40 rounded-sm border border-[var(--border)] bg-[var(--bg-surface)] px-1.5 py-0.5 text-xs transition-colors hover:border-[var(--border-strong)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)]"
              >
                {optionsFor(f).map((o) => (
                  <option key={o.value} value={o.value}>{o.label}</option>
                ))}
              </select>
            </label>
          ))}
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-auto p-3">
        {/* One broken widget must not blank the whole dashboard. */}
        <ErrorBoundary variant="widget">{children}</ErrorBoundary>
      </div>
    </div>
  )
}
