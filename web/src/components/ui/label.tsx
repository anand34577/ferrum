import { type LabelHTMLAttributes, useId, useLayoutEffect, useRef } from "react"
import { cn } from "@/lib/utils"

const CONTROL = 'input, textarea, select, [role="combobox"], [role="switch"], [role="checkbox"]'

/** Form label. When no `htmlFor` is given it names the control that follows
 * it in the DOM — the `<Label/><Input/>` / `<Label/><Select/>` pair every
 * form here is built from — via aria-labelledby, so the ~150 fields written
 * that way get an accessible name without threading ids through each one.
 * Controls that already carry a name (aria-label, aria-labelledby, or an
 * explicit htmlFor) are left alone. */
export function Label({ className, htmlFor, id, ...props }: LabelHTMLAttributes<HTMLLabelElement>) {
  const autoId = useId()
  const labelId = id ?? (htmlFor ? undefined : autoId)
  const ref = useRef<HTMLLabelElement>(null)

  useLayoutEffect(() => {
    if (htmlFor || !ref.current) return
    const next = ref.current.nextElementSibling
    const control = next?.matches(CONTROL) ? next : next?.querySelector(CONTROL)
    if (!control || control.hasAttribute("aria-label") || control.hasAttribute("aria-labelledby")) return
    control.setAttribute("aria-labelledby", labelId!)
  })

  return (
    <label
      ref={ref}
      id={labelId}
      htmlFor={htmlFor}
      className={cn("text-xs font-medium text-[var(--text-muted)]", className)}
      {...props}
    />
  )
}
