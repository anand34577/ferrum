import type { ReactNode } from "react"

/** Fades each page in on mount (`page-enter`, index.css). Enter-only on
 * purpose: the previous exit-then-enter sequence held every navigation back
 * by the length of the exit animation before the next page could even start
 * rendering, and needed a 100 kB animation library in the entry bundle to do
 * it. Routes are keyed by path (App.tsx), so the mount — and with it this
 * animation — happens on every navigation. */
export function PageTransition({ children }: { children: ReactNode }) {
  return (
    // Unbounded (auto) height here breaks any page that needs `h-full` to
    // build an internally-scrolling layout (e.g. AI Assistant: a pinned
    // input with only the message list scrolling) — `h-full`'s percentage
    // chain requires every ancestor up to <main> to have a resolvable
    // height, and this was the missing link, so the whole page grew with
    // the conversation instead and the input kept sinking further down.
    // Content taller than this still renders in full either way (nothing
    // here clips it), so pages that just scroll with the page are
    // unaffected.
    <div className="page-enter h-full">{children}</div>
  )
}
