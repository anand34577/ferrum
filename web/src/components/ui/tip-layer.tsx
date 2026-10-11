import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"

const SHOW_DELAY_MS = 350
const GAP = 8
const EDGE = 8

interface ActiveTip {
  text: string
  rect: DOMRect
}

/** A tip that only repeats the element's own (truncated) text is noise unless
 * something is actually cut off. */
function isRedundant(el: HTMLElement, text: string): boolean {
  if ((el.textContent ?? "").trim() !== text.trim()) return false
  return el.scrollWidth <= el.clientWidth && el.scrollHeight <= el.clientHeight
}

/**
 * The app's replacement for the browser's native `title` tooltip: put
 * `data-tip="…"` on any element and this one layer (mounted once, in
 * main.tsx) shows it in the same themed bubble as `<Hint>`.
 *
 * `<Hint>` stays the right tool for icon buttons and anything interactive.
 * This exists for the long tail it fits badly — truncated table cells,
 * status dots, chart cells — where one Radix tooltip instance per row meant
 * hundreds of mounted tooltips on a large inventory. Here it is a single
 * delegated listener, however many rows there are.
 *
 * Shown on hover (after a short delay) and on keyboard focus; hidden on
 * leave, scroll, Escape, or any press. Skipped for touch, where there is no
 * hover and the native `title` it replaces never appeared either.
 */
export function TipLayer() {
  const [tip, setTip] = useState<ActiveTip | null>(null)
  const [position, setPosition] = useState<{ left: number; top: number } | null>(null)
  const bubbleRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    let current: HTMLElement | null = null

    const hide = () => {
      clearTimeout(timer)
      current = null
      setTip(null)
    }

    const show = (el: HTMLElement, delay: number) => {
      const text = el.dataset.tip
      if (!text || isRedundant(el, text)) return
      clearTimeout(timer)
      current = el
      timer = setTimeout(() => {
        if (current !== el || !el.isConnected) return
        setTip({ text, rect: el.getBoundingClientRect() })
      }, delay)
    }

    // Third-party widgets (the topology map's zoom controls, for one) set a
    // native `title` we can't reach from our own JSX. Adopt it on first
    // contact: the text moves to data-tip and the attribute is removed, so
    // the browser's own tooltip never gets the chance to appear as well.
    // SVG <title> children are left alone — they are accessible names.
    const adoptNativeTitle = (from: Element) => {
      const el = from.closest<HTMLElement>("[title]")
      if (!el || el.closest("svg") || !el.title) return
      if (!el.dataset.tip) el.dataset.tip = el.title
      if (!el.hasAttribute("aria-label") && !(el.textContent ?? "").trim()) el.setAttribute("aria-label", el.title)
      el.removeAttribute("title")
    }

    const targetOf = (e: Event) => {
      if (!(e.target instanceof Element)) return null
      adoptNativeTitle(e.target)
      return e.target.closest("[data-tip]") as HTMLElement | null
    }

    const onOver = (e: PointerEvent) => {
      if (e.pointerType === "touch") return
      const el = targetOf(e)
      if (el === current) return
      if (!el) return hide()
      show(el, SHOW_DELAY_MS)
    }
    const onOut = (e: PointerEvent) => {
      if (!current) return
      const to = e.relatedTarget instanceof Node ? e.relatedTarget : null
      if (!to || !current.contains(to)) hide()
    }
    const onFocus = (e: FocusEvent) => {
      const el = targetOf(e)
      // Only for keyboard focus — a mouse click focusing a button shouldn't
      // leave its tip stuck open.
      if (el && e.target instanceof Element && e.target.matches(":focus-visible")) show(el, 0)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") hide()
    }

    document.addEventListener("pointerover", onOver)
    document.addEventListener("pointerout", onOut)
    document.addEventListener("pointerdown", hide)
    document.addEventListener("focusin", onFocus)
    document.addEventListener("focusout", hide)
    document.addEventListener("keydown", onKey)
    // Capture: scrolling any pane moves the anchor out from under the bubble.
    window.addEventListener("scroll", hide, true)
    window.addEventListener("resize", hide)
    return () => {
      clearTimeout(timer)
      document.removeEventListener("pointerover", onOver)
      document.removeEventListener("pointerout", onOut)
      document.removeEventListener("pointerdown", hide)
      document.removeEventListener("focusin", onFocus)
      document.removeEventListener("focusout", hide)
      document.removeEventListener("keydown", onKey)
      window.removeEventListener("scroll", hide, true)
      window.removeEventListener("resize", hide)
    }
  }, [])

  // Measured after render so the bubble can be centred on its anchor, flipped
  // below when there is no room above, and kept inside the viewport.
  useLayoutEffect(() => {
    const bubble = bubbleRef.current
    if (!tip || !bubble) {
      setPosition(null)
      return
    }
    const { width, height } = bubble.getBoundingClientRect()
    const { rect } = tip
    const above = rect.top - height - GAP
    const top = above >= EDGE ? above : Math.min(window.innerHeight - height - EDGE, rect.bottom + GAP)
    const centred = rect.left + rect.width / 2 - width / 2
    const left = Math.max(EDGE, Math.min(window.innerWidth - width - EDGE, centred))
    setPosition({ left, top })
  }, [tip])

  if (!tip) return null

  return createPortal(
    <div
      ref={bubbleRef}
      role="tooltip"
      // Same surface as ui/tooltip's TooltipContent, so the two are indistinguishable.
      className="pointer-events-none fixed z-[400] max-w-72 rounded-md border border-[var(--sidebar-border)] bg-[var(--sidebar-bg)] px-2.5 py-1.5 text-xs leading-snug break-words text-[var(--sidebar-text)] shadow-md animate-in fade-in zoom-in-95"
      style={{ left: position?.left ?? 0, top: position?.top ?? 0, visibility: position ? "visible" : "hidden" }}
    >
      {tip.text}
    </div>,
    document.body,
  )
}
