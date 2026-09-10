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
export type Accent = "oxide" | "azure" | "verdant" | "violet" | "slate" | "amber" | "rose" | "teal"
/** Row/list padding across tables and lists app-wide — orthogonal to look,
 * same as accent. See the [data-density="compact"] rules in index.css. */
export type Density = "comfortable" | "compact"
/** Whole-app visual register, orthogonal to light/dark and accent — see the
 * "Look-and-feel presets" block in index.css for what each one repaints. */
export type Look =
  | "enterprise"
  | "proxmox"
  | "terminal"
  | "glassFlightDeck"
  | "midnight"
  | "paper"
  | "glassmorphism"
  | "neumorphism"
  | "brutalist"
  | "solarized"
  | "highContrast"
  | "aurora"

const KNOWN_LOOKS: Look[] = [
  "enterprise",
  "proxmox",
  "terminal",
  "glassFlightDeck",
  "midnight",
  "paper",
  "glassmorphism",
  "neumorphism",
  "brutalist",
  "solarized",
  "highContrast",
  "aurora",
]

const THEME_KEY = "ferrum-theme"
const ACCENT_KEY = "ferrum-accent"
const LOOK_KEY = "ferrum-look"
const DENSITY_KEY = "ferrum-density"

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
  /** Named look-and-feel preset — repaints typography, radius, elevation,
   * and surface tone via index.css's [data-look] rules. */
  look: Look
  setLook: (l: Look) => void
  /** Row/list padding preset — repaints via index.css's [data-density] rules. */
  density: Density
  setDensity: (d: Density) => void
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

function cachedLook(): Look {
  const stored = localStorage.getItem(LOOK_KEY)
  return (KNOWN_LOOKS as string[]).includes(stored ?? "") ? (stored as Look) : "enterprise"
}

function cachedDensity(): Density {
  const stored = localStorage.getItem(DENSITY_KEY)
  return stored === "compact" ? "compact" : "comfortable"
}

const osDarkQuery = () => window.matchMedia("(prefers-color-scheme: dark)")

// Applies the classes/attributes and keeps the browser UI chrome (theme-color) in sync.
function applyTheme(theme: Theme, accent: Accent, look: Look, density: Density) {
  document.documentElement.classList.toggle("dark", theme === "dark")
  document.documentElement.dataset.accent = accent
  document.documentElement.dataset.look = look
  document.documentElement.dataset.density = density
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute("content", theme === "dark" ? "#15171b" : "#ffffff")
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  const [theme, setThemeState] = useState<ThemePreference>(() => cachedTheme())
  const [accent, setAccentState] = useState<Accent>(() => cachedAccent())
  const [look, setLookState] = useState<Look>(() => cachedLook())
  const [density, setDensityState] = useState<Density>(() => cachedDensity())
  const themeRef = useRef(theme)
  themeRef.current = theme
  const accentRef = useRef(accent)
  accentRef.current = accent
  const lookRef = useRef(look)
  lookRef.current = look
  const densityRef = useRef(density)
  densityRef.current = density

  // The authoritative copy lives in the database — pull it once per session
  // (and after any sign-in) and adopt it when it differs from the cache.
  const prefsQuery = useQuery({
    queryKey: ["auth", "preferences"],
    queryFn: () => api.get<{ theme: ThemePreference; accent: Accent; look?: Look; density?: Density }>("/auth/me/preferences"),
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
    const knownAccents: Accent[] = ["oxide", "azure", "verdant", "violet", "slate", "amber", "rose", "teal"]
    if (serverAccent && knownAccents.includes(serverAccent) && serverAccent !== accentRef.current) {
      setAccentState(serverAccent)
      localStorage.setItem(ACCENT_KEY, serverAccent)
    }
    const serverLook = prefsQuery.data?.look
    if (serverLook && KNOWN_LOOKS.includes(serverLook) && serverLook !== lookRef.current) {
      setLookState(serverLook)
      localStorage.setItem(LOOK_KEY, serverLook)
    }
    const serverDensity = prefsQuery.data?.density
    if (serverDensity && (serverDensity === "comfortable" || serverDensity === "compact") && serverDensity !== densityRef.current) {
      setDensityState(serverDensity)
      localStorage.setItem(DENSITY_KEY, serverDensity)
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
    applyTheme(effectiveTheme, accent, look, density)
  }, [effectiveTheme, accent, look, density])

  // All four fields persist together — the API stores one row per user, not
  // independent columns a client can PATCH separately.
  function save(nextTheme: ThemePreference, nextAccent: Accent, nextLook: Look, nextDensity: Density) {
    if (!user) return
    api.put("/auth/me/preferences", { theme: nextTheme, accent: nextAccent, look: nextLook, density: nextDensity }).catch((err) => {
      if (!(err instanceof ApiError && err.status === 401)) {
        toast.error("Preference saved for this browser only — the server didn't accept the change.")
      }
    })
  }

  const setTheme = (next: ThemePreference) => {
    setThemeState(next)
    localStorage.setItem(THEME_KEY, next)
    save(next, accentRef.current, lookRef.current, densityRef.current)
  }

  const setAccent = (next: Accent) => {
    setAccentState(next)
    localStorage.setItem(ACCENT_KEY, next)
    save(themeRef.current, next, lookRef.current, densityRef.current)
  }

  const setLook = (next: Look) => {
    setLookState(next)
    localStorage.setItem(LOOK_KEY, next)
    save(themeRef.current, accentRef.current, next, densityRef.current)
  }

  const setDensity = (next: Density) => {
    setDensityState(next)
    localStorage.setItem(DENSITY_KEY, next)
    save(themeRef.current, accentRef.current, lookRef.current, next)
  }

  const value = useMemo<ThemeContextValue>(
    () => ({
      theme,
      effectiveTheme,
      setTheme,
      toggle: () => setTheme(effectiveTheme === "dark" ? "light" : "dark"),
      accent,
      setAccent,
      look,
      setLook,
      density,
      setDensity,
    }),
    // setTheme/setAccent/setLook/setDensity close over `user` — include it so a sign-in/out rebinds them.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [theme, effectiveTheme, accent, look, density, user],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme() {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error("useTheme must be used within ThemeProvider")
  return ctx
}
