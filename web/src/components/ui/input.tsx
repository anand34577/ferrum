import { ChevronDown, ChevronUp } from "lucide-react"
import { type InputHTMLAttributes, forwardRef, useRef } from "react"
import { cn } from "@/lib/utils"

const inputClass = cn(
  "flex h-9 w-full rounded-md border border-[var(--border)] bg-[var(--bg-surface)] px-3 font-mono text-sm text-[var(--text)] transition-all duration-150",
  "placeholder:text-[var(--text-faint)] hover:border-[var(--border-strong)]",
  "focus-visible:outline-none focus-visible:border-[var(--ring)] focus-visible:ring-[3px] focus-visible:ring-[color-mix(in_oklab,var(--ring)_25%,transparent)]",
  "disabled:cursor-not-allowed disabled:opacity-50",
  "aria-[invalid=true]:border-[var(--status-error)] aria-[invalid=true]:focus-visible:border-[var(--status-error)] aria-[invalid=true]:focus-visible:ring-[color-mix(in_oklab,var(--status-error)_25%,transparent)]",
)

/** Every text input sets in mono deliberately — part of the app's technical-
 * instrument register (tracked-caps badges, mono metrics elsewhere). Prose
 * fields (Description/Notes) opt back into the body font at the call site
 * the same way DatacenterOptionsForm's textarea already does.
 *
 * `type="number"` renders the app's own stepper instead of the browser's
 * spin buttons (which look different in every browser and ignore the theme)
 * — see NumberField below. Call sites don't change: it is still a real
 * number input and still fires the same onChange. */
export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  ({ className, type, ...props }, ref) => {
    if (type === "number") return <NumberField ref={ref} className={className} {...props} />
    return <input ref={ref} type={type} className={cn(inputClass, className)} {...props} />
  },
)
Input.displayName = "Input"

const stepButtonClass =
  "flex h-1/2 w-6 cursor-pointer items-center justify-center text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-muted)] hover:text-[var(--text)] active:bg-[var(--border)] disabled:pointer-events-none disabled:opacity-40"

const NumberField = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  ({ className, disabled, readOnly, onWheel, ...props }, forwardedRef) => {
    const innerRef = useRef<HTMLInputElement | null>(null)

    // stepUp/stepDown honour the input's own min/max/step. They change the
    // value without going through React, so the bubbling `input` event is
    // what makes the caller's onChange fire, exactly as if it had been typed.
    function step(direction: 1 | -1) {
      const el = innerRef.current
      if (!el) return
      try {
        if (direction === 1) el.stepUp()
        else el.stepDown()
      } catch {
        return // step="any" or a value the browser refuses to step from
      }
      el.dispatchEvent(new Event("input", { bubbles: true }))
      el.focus()
    }

    const locked = disabled || readOnly

    return (
      // Width/margin utilities from the call site (`className="w-24"`) size
      // the wrapper; the input always fills it.
      <div className={cn("group/number relative w-full", className)}>
        <input
          ref={(node) => {
            innerRef.current = node
            if (typeof forwardedRef === "function") forwardedRef(node)
            else if (forwardedRef) forwardedRef.current = node
          }}
          type="number"
          disabled={disabled}
          readOnly={readOnly}
          // A focused number input changes its value on scroll — scrolling a
          // long settings form past one silently rewrote the field under the
          // cursor. Dropping focus lets the wheel scroll the page instead.
          onWheel={(e) => {
            if (document.activeElement === e.currentTarget) e.currentTarget.blur()
            onWheel?.(e)
          }}
          className={cn(inputClass, "number-field pr-8 tabular")}
          {...props}
        />
        <div
          className={cn(
            "absolute inset-y-px right-px flex w-6 flex-col overflow-hidden rounded-r-[calc(var(--radius-md)-1px)] border-l border-[var(--border)] bg-[var(--bg-surface)]",
            locked && "opacity-50",
          )}
        >
          {/* tabIndex -1: the field itself already steps with ↑/↓, so these
              would only add two dead tab stops per number on every form. */}
          <button type="button" tabIndex={-1} aria-label="Increase" disabled={locked} onClick={() => step(1)} className={stepButtonClass}>
            <ChevronUp className="h-3 w-3" aria-hidden />
          </button>
          <button
            type="button"
            tabIndex={-1}
            aria-label="Decrease"
            disabled={locked}
            onClick={() => step(-1)}
            className={cn(stepButtonClass, "border-t border-[var(--border)]")}
          >
            <ChevronDown className="h-3 w-3" aria-hidden />
          </button>
        </div>
      </div>
    )
  },
)
NumberField.displayName = "NumberField"
