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
            <button
              onClick={onMoveUp}
              disabled={!onMoveUp}
              className="flex h-6 w-6 items-center justify-center rounded text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-muted)] hover:text-[var(--text)] disabled:pointer-events-none disabled:opacity-30"
              aria-label={`Move ${widgetLabel(type)} widget earlier`}
            >
              <ChevronUp className="h-3.5 w-3.5" />
            </button>
            <button
              onClick={onMoveDown}
              disabled={!onMoveDown}
              className="flex h-6 w-6 items-center justify-center rounded text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-muted)] hover:text-[var(--text)] disabled:pointer-events-none disabled:opacity-30"
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
              "ml-auto flex h-6 w-6 items-center justify-center rounded text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-muted)] hover:text-[var(--text)]",
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
              "flex h-6 w-6 items-center justify-center rounded text-[var(--text-muted)] transition-colors hover:bg-[color-mix(in_oklab,var(--status-error)_12%,transparent)] hover:text-[var(--status-error)]",
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
                className="max-w-40 rounded border border-[var(--border)] bg-[var(--bg-surface)] px-1.5 py-0.5 text-xs"
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
