import { motion } from "framer-motion"
import type { ReactNode } from "react"

export function PageTransition({ children }: { children: ReactNode }) {
  return (
    <motion.div
      // Unbounded (auto) height here breaks any page that needs `h-full` to
      // build an internally-scrolling layout (e.g. AI Assistant: a pinned
      // input with only the message list scrolling) — `h-full`'s percentage
      // chain requires every ancestor up to <main> to have a resolvable
      // height, and this was the missing link, so the whole page grew with
      // the conversation instead and the input kept sinking further down.
      // Content taller than this still renders in full either way (nothing
      // here clips it), so pages that just scroll with the page are
      // unaffected.
      className="h-full"
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -6 }}
      transition={{ duration: 0.18, ease: "easeOut" }}
    >
      {children}
    </motion.div>
  )
}
