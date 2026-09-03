import type { LabelHTMLAttributes } from "react"
import { cn } from "@/lib/utils"

export function Label({ className, ...props }: LabelHTMLAttributes<HTMLLabelElement>) {
  return (
    <label
      className={cn("text-xs font-medium text-[var(--text-muted)]", className)}
      {...props}
    />
  )
}
