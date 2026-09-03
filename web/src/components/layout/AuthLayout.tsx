import type { ReactNode } from "react"
import { BrandMark } from "@/components/layout/BrandMark"

/** Shared shell for the unauthenticated surfaces (login, setup): centered
 * card on a calm, brand-tinted backdrop. One place to restyle the front
 * door of the app. */
export function AuthLayout({ title, subtitle, children }: { title: string; subtitle?: ReactNode; children: ReactNode }) {
  return (
    <div className="relative flex min-h-full items-center justify-center overflow-hidden p-6">
      {/* Soft oxide glow behind the card — pure CSS, no images */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{
          background:
            "radial-gradient(600px 420px at 50% 12%, color-mix(in oklab, var(--color-brand-500) 14%, transparent), transparent 70%)",
        }}
      />
      <div className="relative w-full max-w-sm">
        <div className="rounded-xl border border-[var(--border)] bg-[var(--bg-elevated)] p-8 shadow-md">
          <div className="mb-6 text-center">
            <BrandMark size="lg" className="mx-auto mb-3" />
            <h1 className="font-display text-xl font-semibold tracking-tight">{title}</h1>
            {subtitle && <p className="mt-1 text-sm text-[var(--text-muted)]">{subtitle}</p>}
          </div>
          {children}
        </div>
        <p className="mt-4 text-center font-mono text-[10px] uppercase tracking-[0.14em] text-[var(--text-faint)]">
          Ferrum · Proxmox fleet control
        </p>
      </div>
    </div>
  )
}
