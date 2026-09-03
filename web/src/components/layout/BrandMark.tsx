import { cn } from "@/lib/utils"

/** Ferrum's mark: the periodic-table symbol for iron — the name's actual
 * etymology, not an arbitrary initial. Used everywhere the wordmark badge
 * appears (sidebar, login, setup) so it only needs to be designed once. */
export function BrandMark({ size = "md", className }: { size?: "md" | "lg"; className?: string }) {
  return (
    <div
      className={cn(
        "flex shrink-0 items-center justify-center rounded-lg bg-brand-600 font-display font-bold text-white",
        size === "md" ? "h-7 w-7 text-xs" : "h-10 w-10 text-lg",
        className,
      )}
    >
      Fe
    </div>
  )
}
