import { useEffect, useRef, useState } from "react"

/**
 * The "Copied ✓" flash after a copy-to-clipboard click, auto-reverting after
 * `ms` — shared by every copy button in the app (code blocks, chat message
 * copy, API keys) so the revert timer is cleared on unmount instead of each
 * call site risking a "set state on an unmounted component" warning if the
 * button disappears (scrolled out of a virtualized list, dialog closed)
 * before its timeout fires.
 */
export function useCopiedFlag(ms = 1500): [boolean, () => void] {
  const [copied, setCopied] = useState(false)
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => () => {
    if (timeoutRef.current !== null) clearTimeout(timeoutRef.current)
  }, [])

  function flash() {
    if (timeoutRef.current !== null) clearTimeout(timeoutRef.current)
    setCopied(true)
    timeoutRef.current = setTimeout(() => setCopied(false), ms)
  }

  return [copied, flash]
}
