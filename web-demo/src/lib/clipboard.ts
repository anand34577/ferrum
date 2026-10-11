/**
 * Copies text to the clipboard, resolving true on success.
 *
 * `navigator.clipboard` only exists in a secure context (HTTPS or
 * localhost). Ferrum is routinely reached at http://<lan-ip>:8080, where it
 * is `undefined` — so the bare `navigator.clipboard.writeText(...)` calls
 * this replaces threw a TypeError before their `.catch` could run, and the
 * copy button for a just-created API key (shown exactly once) did nothing.
 * The textarea + execCommand path below still works there.
 */
export async function copyText(text: string): Promise<boolean> {
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // Permission denied or document not focused — try the fallback.
    }
  }
  return legacyCopy(text)
}

function legacyCopy(text: string): boolean {
  if (typeof document === "undefined") return false
  const el = document.createElement("textarea")
  el.value = text
  el.setAttribute("readonly", "")
  // Off-screen but still selectable; `display: none` would make the
  // selection (and therefore the copy) empty.
  el.style.position = "fixed"
  el.style.top = "0"
  el.style.left = "-9999px"
  el.style.opacity = "0"
  // Inside an open Radix dialog the focus trap pulls focus back from <body>,
  // so mount next to whatever currently has focus when there is such a thing.
  const host = (document.activeElement?.closest('[role="dialog"]') as HTMLElement | null) ?? document.body
  const previous = document.activeElement as HTMLElement | null
  host.appendChild(el)
  el.select()
  el.setSelectionRange(0, text.length)
  let ok = false
  try {
    ok = document.execCommand("copy")
  } catch {
    ok = false
  }
  el.remove()
  previous?.focus?.()
  return ok
}
