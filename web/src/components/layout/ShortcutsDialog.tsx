import { useEffect, useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform)
const modKey = isMac ? "⌘" : "Ctrl"

const shortcuts: { keys: string[]; label: string }[] = [
  { keys: [`${modKey}`, "K"], label: "Open command palette — jump to a page, node, or guest" },
  { keys: ["?"], label: "Show this shortcuts reference" },
  { keys: ["Esc"], label: "Close the open dialog, drawer, or palette" },
  { keys: ["↑", "↓"], label: "Move the selection in the command palette or a menu" },
  { keys: ["↵"], label: "Activate the selected command palette result" },
]

/** Global "?" shortcuts reference — the palette's footer hints only cover the
 * palette itself, so this is the one place a keyboard user can see every
 * shortcut the app has without guessing. */
export function ShortcutsDialog() {
  const [open, setOpen] = useState(false)

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== "?" || e.metaKey || e.ctrlKey || e.altKey) return
      const target = e.target as HTMLElement | null
      const editing = target?.tagName === "INPUT" || target?.tagName === "TEXTAREA" || target?.isContentEditable
      if (editing) return
      e.preventDefault()
      setOpen((o) => !o)
    }
    function onOpen() {
      setOpen(true)
    }
    document.addEventListener("keydown", onKeyDown)
    document.addEventListener("ferrum:open-shortcuts", onOpen)
    return () => {
      document.removeEventListener("keydown", onKeyDown)
      document.removeEventListener("ferrum:open-shortcuts", onOpen)
    }
  }, [])

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Keyboard shortcuts</DialogTitle>
          <DialogDescription>Available anywhere in Ferrum.</DialogDescription>
        </DialogHeader>
        <ul className="space-y-2.5">
          {shortcuts.map((s) => (
            <li key={s.label} className="flex items-center justify-between gap-4 text-sm">
              <span className="text-[var(--text-muted)]">{s.label}</span>
              <span className="flex shrink-0 gap-1">
                {s.keys.map((k) => (
                  <kbd key={k} className="rounded-sm border border-[var(--border)] bg-[var(--bg-muted)] px-1.5 py-0.5 font-mono text-[10px]">
                    {k}
                  </kbd>
                ))}
              </span>
            </li>
          ))}
        </ul>
      </DialogContent>
    </Dialog>
  )
}
