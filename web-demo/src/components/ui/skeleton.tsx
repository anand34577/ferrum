import { cn } from "@/lib/utils"

/** Placeholder shimmer for content that is loading. Render skeleton shapes
 * that mirror the real layout — never a bare "Loading..." string. */
export function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      aria-hidden
      className={cn(
        "relative overflow-hidden rounded-md bg-[var(--bg-muted)] before:absolute before:inset-0 before:-translate-x-full before:animate-shimmer before:bg-gradient-to-r before:from-transparent before:via-white/15 dark:before:via-white/5 before:to-transparent",
        className,
      )}
      {...props}
    />
  )
}

/** Mimics the DataTable layout (header row + N data rows) so page height
 * barely shifts when real data arrives. */
export function TableSkeleton({ rows = 5, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn("overflow-hidden rounded-md border border-[var(--border)]", className)} aria-hidden>
      <div className="flex gap-4 border-b border-[var(--border)] bg-[var(--bg-muted)] px-3 py-2.5">
        <Skeleton className="h-3 w-20" />
        <Skeleton className="h-3 w-16" />
        <Skeleton className="ml-auto h-3 w-12" />
      </div>
      <div className="divide-y divide-[var(--border)]">
        {Array.from({ length: rows }).map((_, i) => (
          <div key={i} className="flex items-center gap-4 px-3 py-3">
            <Skeleton className="h-3.5 w-24" style={{ opacity: 1 - i * 0.12 }} />
            <Skeleton className="h-3.5 w-16" style={{ opacity: 1 - i * 0.12 }} />
            <Skeleton className="ml-auto h-3.5 w-10" style={{ opacity: 1 - i * 0.12 }} />
          </div>
        ))}
      </div>
    </div>
  )
}

/** Mimics a Card with header + a couple of content lines. */
export function CardSkeleton({ className }: { className?: string }) {
  return (
    <div className={cn("rounded-lg border border-[var(--border)] bg-[var(--bg-surface)] p-4 shadow-xs", className)} aria-hidden>
      <Skeleton className="h-3.5 w-32" />
      <Skeleton className="mt-3 h-8 w-20" />
      <Skeleton className="mt-3 h-3 w-full" />
    </div>
  )
}
