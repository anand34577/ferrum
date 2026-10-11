import type { ReactNode } from "react"
import { BrandMark } from "@/components/layout/BrandMark"

/** Shared shell for the unauthenticated surfaces (login, setup): centered
 * card on a calm, brand-tinted backdrop. One place to restyle the front
 * door of the app. */
export function AuthLayout({ title, subtitle, children }: { title: string; subtitle?: ReactNode; children: ReactNode }) {
  return (
    <div className="relative flex min-h-full items-center justify-center overflow-hidden p-6">
      {/* The active look's own page-grid texture (none, unless Glass Flight
          Deck) — same --page-grid token the app body paints, so the front
          door never shows a texture the rest of the app doesn't. */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{ backgroundImage: "var(--page-grid)", backgroundSize: "32px 32px" }}
      />
      <div className="relative w-full max-w-sm">
        <div className="rounded-lg border border-[var(--border)] bg-[var(--bg-elevated)] p-8 shadow-lg">
          <div className="mb-6 text-center">
            <BrandMark size="lg" className="mx-auto mb-3" />
            <h1 className="panel-label text-xl font-bold text-[var(--text)]">{title}</h1>
            {subtitle && <p className="mt-1 text-sm normal-case tracking-normal text-[var(--text-muted)]">{subtitle}</p>}
          </div>
          {children}
        </div>
        <p className="panel-label mt-4 text-center text-[10px] text-[var(--text-faint)]">
          Ferrum · Proxmox fleet control
        </p>
      </div>
    </div>
  )
}
