import { cn } from "@/lib/utils"

/** Ferrum's mark: the periodic-table symbol for iron — the name's actual
 * etymology, not an arbitrary initial. Used everywhere the wordmark badge
 * appears (sidebar, login, setup) so it only needs to be designed once. */
export function BrandMark({ size = "md", className }: { size?: "md" | "lg"; className?: string }) {
  if (size === "lg") {
    return (
      <div
        className={cn(
          "relative flex h-11 w-11 shrink-0 flex-col justify-between rounded-md border border-brand-700 bg-brand-600 p-1.5 text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.2)] select-none",
          className,
        )}
        aria-label="Ferrum (Element 26: Fe)"
      >
        <div className="flex items-start justify-between font-mono text-[8px] font-semibold opacity-85 leading-none">
          <span>26</span>
          <span className="text-[7px] opacity-75">55.8</span>
        </div>
        <div className="text-center font-mono text-base font-bold leading-none tracking-tight">
          Fe
        </div>
        <div className="text-center font-mono text-[6px] tracking-widest uppercase opacity-75 leading-none">
          IRON
        </div>
      </div>
    )
  }

  return (
    <div
      className={cn(
        "relative flex h-7 w-7 shrink-0 flex-col justify-between rounded-sm border border-brand-700 bg-brand-600 p-0.5 text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.16)] select-none",
        className,
      )}
      aria-label="Ferrum (Fe)"
    >
      <span className="font-mono text-[7px] font-medium opacity-85 leading-none">26</span>
      <span className="-mt-1 text-center font-mono text-xs font-bold leading-none tracking-tight">
        Fe
      </span>
      <span className="h-[2px]" />
    </div>
  )
}
