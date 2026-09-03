import { useQuery } from "@tanstack/react-query"
import { createContext, type ReactNode, useContext, useEffect, useMemo, useRef, useState } from "react"
import { toast } from "sonner"
import { api, ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"

/**
 * UI theme: the user's *preference* is one of light / dark / system and is
 * persisted server-side per account (PUT /auth/me/preferences), so it follows
 * the user across browsers and devices. localStorage only mirrors the last
 * known value to avoid a flash of the wrong theme on the first paint — the
 * database copy wins whenever they differ.
 */

export type ThemePreference = "light" | "dark" | "system"
export type Theme = "light" | "dark"
export type Accent = "oxide" | "azure" | "verdant" | "violet" | "slate"

const THEME_KEY = "ferrum-theme"
const ACCENT_KEY = "ferrum-accent"

interface ThemeContextValue {
  /** The stored preference — "system" means follow the OS setting. */
  theme: ThemePreference
  /** What is actually painted: "system" resolves to light or dark. */
  effectiveTheme: Theme
  setTheme: (t: ThemePreference) => void
  /** Convenience for the header button: force the opposite of what's painted. */
  toggle: () => void
  /** Named accent palette — repaints every brand-* color via index.css's
   * [data-accent] rules. */
  accent: Accent
  setAccent: (a: Accent) => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

function cachedTheme(): ThemePreference {
  const stored = localStorage.getItem(THEME_KEY)
  return stored === "light" || stored === "dark" || stored === "system" ? stored : "system"
}

function cachedAccent(): Accent {
  const stored = localStorage.getItem(ACCENT_KEY)
  return stored === "azure" || stored === "verdant" || stored === "violet" || stored === "slate" ? stored : "oxide"
}

const osDarkQuery = () => window.matchMedia("(prefers-color-scheme: dark)")

// Applies the classes/attributes and keeps the browser UI chrome (theme-color) in sync.
function applyTheme(theme: Theme, accent: Accent) {
  document.documentElement.classList.toggle("dark", theme === "dark")
  document.documentElement.dataset.accent = accent
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute("content", theme === "dark" ? "#15171b" : "#ffffff")
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  const [theme, setThemeState] = useState<ThemePreference>(() => cachedTheme())
  const [accent, setAccentState] = useState<Accent>(() => cachedAccent())
  const themeRef = useRef(theme)
  themeRef.current = theme
  const accentRef = useRef(accent)
  accentRef.current = accent

  // The authoritative copy lives in the database — pull it once per session
  // (and after any sign-in) and adopt it when it differs from the cache.
  const prefsQuery = useQuery({
    queryKey: ["auth", "preferences"],
    queryFn: () => api.get<{ theme: ThemePreference; accent: Accent }>("/auth/me/preferences"),
    enabled: !!user,
    staleTime: 60_000,
  })
  useEffect(() => {
    const server = prefsQuery.data?.theme
    if (server && (server === "light" || server === "dark" || server === "system") && server !== themeRef.current) {
      setThemeState(server)
      localStorage.setItem(THEME_KEY, server)
    }
    const serverAccent = prefsQuery.data?.accent
    const knownAccents: Accent[] = ["oxide", "azure", "verdant", "violet", "slate"]
    if (serverAccent && knownAccents.includes(serverAccent) && serverAccent !== accentRef.current) {
      setAccentState(serverAccent)
      localStorage.setItem(ACCENT_KEY, serverAccent)
    }
  }, [prefsQuery.data])

  const [osDark, setOsDark] = useState(() => osDarkQuery().matches)
  useEffect(() => {
    const media = osDarkQuery()
    const onChange = (e: MediaQueryListEvent) => setOsDark(e.matches)
    media.addEventListener("change", onChange)
    return () => media.removeEventListener("change", onChange)
  }, [])

  const effectiveTheme: Theme = theme === "system" ? (osDark ? "dark" : "light") : theme

  useEffect(() => {
    applyTheme(effectiveTheme, accent)
  }, [effectiveTheme, accent])

  // Both fields persist together — the API stores one row per user, not
  // independent columns a client can PATCH separately.
  function save(nextTheme: ThemePreference, nextAccent: Accent) {
    if (!user) return
    api.put("/auth/me/preferences", { theme: nextTheme, accent: nextAccent }).catch((err) => {
      if (!(err instanceof ApiError && err.status === 401)) {
        toast.error("Preference saved for this browser only — the server didn't accept the change.")
      }
    })
  }

  const setTheme = (next: ThemePreference) => {
    setThemeState(next)
    localStorage.setItem(THEME_KEY, next)
    save(next, accentRef.current)
  }

  const setAccent = (next: Accent) => {
    setAccentState(next)
    localStorage.setItem(ACCENT_KEY, next)
    save(themeRef.current, next)
  }

  const value = useMemo<ThemeContextValue>(
    () => ({
      theme,
      effectiveTheme,
      setTheme,
      toggle: () => setTheme(effectiveTheme === "dark" ? "light" : "dark"),
      accent,
      setAccent,
    }),
    // setTheme/setAccent close over `user` — include it so a sign-in/out rebinds them.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [theme, effectiveTheme, accent, user],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme() {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error("useTheme must be used within ThemeProvider")
  return ctx
}
