import { FileUp, X } from "lucide-react"
import { type DragEvent, useId, useRef, useState } from "react"
import { cn, formatBytes } from "@/lib/utils"

interface FileDropzoneProps {
  file: File | null
  onChange: (file: File | null) => void
  /** Same syntax as an input's `accept` (".iso,.img"); also enforced on drop,
   * which the browser's own picker filter doesn't cover. */
  accept?: string
  /** One line under the prompt, e.g. "ISO images up to the storage's free space". */
  hint?: string
  disabled?: boolean
  /** 0–100 while an upload is in flight; renders a progress bar over the file row. */
  progress?: number
  className?: string
}

function matchesAccept(file: File, accept?: string): boolean {
  if (!accept) return true
  const name = file.name.toLowerCase()
  return accept
    .split(",")
    .map((a) => a.trim().toLowerCase())
    .filter(Boolean)
    .some((a) => (a.startsWith(".") ? name.endsWith(a) : a.endsWith("/*") ? file.type.startsWith(a.slice(0, -1)) : file.type === a))
}

/** The app's file picker: a themed drop target (click, keyboard, or drag a
 * file in) in place of the browser's unstyleable "Choose file" control. The
 * real <input type="file"> is still there, visually hidden, so the OS picker
 * and form semantics keep working. */
export function FileDropzone({ file, onChange, accept, hint, disabled, progress, className }: FileDropzoneProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const inputId = useId()
  const [dragging, setDragging] = useState(false)
  const [rejected, setRejected] = useState<string | null>(null)
  const uploading = progress !== undefined

  function pick(next: File | null | undefined) {
    if (!next) return
    if (!matchesAccept(next, accept)) {
      setRejected(`${next.name} isn't a supported file type (${accept}).`)
      return
    }
    setRejected(null)
    onChange(next)
  }

  function onDrop(e: DragEvent) {
    e.preventDefault()
    setDragging(false)
    if (disabled || uploading) return
    pick(e.dataTransfer.files?.[0])
  }

  function clear() {
    onChange(null)
    setRejected(null)
    if (inputRef.current) inputRef.current.value = ""
  }

  return (
    <div className={className}>
      <input
        ref={inputRef}
        id={inputId}
        type="file"
        accept={accept}
        disabled={disabled || uploading}
        className="peer sr-only"
        onChange={(e) => {
          pick(e.target.files?.[0])
          // Reset so choosing the same file again (after clearing) still fires.
          e.target.value = ""
        }}
      />
      {file ? (
        <div className="relative overflow-hidden rounded-md border border-[var(--border)] bg-[var(--bg-surface)]">
          {uploading && (
            <div
              className="absolute inset-y-0 left-0 bg-[color-mix(in_oklab,var(--color-brand-500)_14%,transparent)] transition-[width] duration-200"
              style={{ width: `${Math.min(100, Math.max(0, progress))}%` }}
              aria-hidden
            />
          )}
          <div className="relative flex items-center gap-3 px-3 py-2.5">
            <FileUp className="h-4 w-4 shrink-0 text-brand-500" aria-hidden />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium" data-tip={file.name}>{file.name}</p>
              <p className="text-xs text-[var(--text-muted)] tabular">
                {formatBytes(file.size)}
                {uploading && (
                  <span role="status"> · {progress >= 100 ? "received — finishing on the server…" : `uploading ${Math.round(progress)}%`}</span>
                )}
              </p>
            </div>
            {!uploading && (
              <button
                type="button"
                onClick={clear}
                disabled={disabled}
                aria-label={`Remove ${file.name}`}
                className="flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded-md text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-muted)] hover:text-[var(--text)] disabled:pointer-events-none disabled:opacity-50"
              >
                <X className="h-3.5 w-3.5" aria-hidden />
              </button>
            )}
          </div>
        </div>
      ) : (
        <label
          htmlFor={inputId}
          onDragOver={(e) => {
            e.preventDefault()
            if (!disabled) setDragging(true)
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={onDrop}
          className={cn(
            "flex cursor-pointer flex-col items-center gap-1.5 rounded-md border border-dashed border-[var(--border-strong)] bg-[var(--bg-surface)] px-4 py-6 text-center transition-colors",
            "hover:border-brand-500/60 hover:bg-[var(--bg-surface-hover)]",
            "peer-focus-visible:border-[var(--ring)] peer-focus-visible:ring-[3px] peer-focus-visible:ring-[color-mix(in_oklab,var(--ring)_25%,transparent)]",
            dragging && "border-brand-500 bg-[color-mix(in_oklab,var(--color-brand-500)_8%,var(--bg-surface))]",
            disabled && "pointer-events-none opacity-50",
          )}
        >
          <FileUp className={cn("h-5 w-5 text-[var(--text-muted)] transition-colors", dragging && "text-brand-500")} aria-hidden />
          <span className="text-sm">
            <span className="font-medium text-brand-600 dark:text-brand-400">Choose a file</span>
            <span className="text-[var(--text-muted)]"> or drag it here</span>
          </span>
          {hint && <span className="text-xs text-[var(--text-muted)]">{hint}</span>}
        </label>
      )}
      {rejected && <p className="mt-1.5 text-xs text-[var(--status-error)]" role="alert">{rejected}</p>}
    </div>
  )
}
