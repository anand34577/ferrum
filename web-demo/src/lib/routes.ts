import { type ComponentType, lazy, type LazyExoticComponent } from "react"

/**
 * Every lazily-loaded page, keyed by its route's first path segment.
 *
 * One table so the router (App.tsx) and the navigation (sidebar, command
 * palette) agree on which chunk backs which path — the navigation uses it to
 * start downloading a page's chunk on hover/focus, so by the time the click
 * lands the page usually renders without a skeleton flash.
 */
const loaders = {
  "/": () => import("@/pages/OverviewPage").then((m) => ({ default: m.OverviewPage })),
  "/dashboard": () => import("@/pages/DashboardPage").then((m) => ({ default: m.DashboardPage })),
  "/inventory": () => import("@/pages/InventoryPage").then((m) => ({ default: m.InventoryPage })),
  "/nodes": () => import("@/pages/NodeDetailPage").then((m) => ({ default: m.NodeDetailPage })),
  "/topology": () => import("@/pages/TopologyPage").then((m) => ({ default: m.TopologyPage })),
  "/storage": () => import("@/pages/StoragePage").then((m) => ({ default: m.StoragePage })),
  "/pbs": () => import("@/pages/PBSPage").then((m) => ({ default: m.PBSPage })),
  "/pools": () => import("@/pages/PoolsPage").then((m) => ({ default: m.PoolsPage })),
  "/backups": () => import("@/pages/BackupsPage").then((m) => ({ default: m.BackupsPage })),
  "/ha": () => import("@/pages/HAPage").then((m) => ({ default: m.HAPage })),
  "/cluster": () => import("@/pages/ClusterPage").then((m) => ({ default: m.ClusterPage })),
  "/firewall": () => import("@/pages/FirewallPage").then((m) => ({ default: m.FirewallPage })),
  "/alerts": () => import("@/pages/AlertsPage").then((m) => ({ default: m.AlertsPage })),
  "/tasks": () => import("@/pages/TasksPage").then((m) => ({ default: m.TasksPage })),
  "/ai-assistant": () => import("@/pages/AIAssistantPage").then((m) => ({ default: m.AIAssistantPage })),
  "/bulk-operations": () => import("@/pages/BulkOperationsPage").then((m) => ({ default: m.BulkOperationsPage })),
  "/webhooks": () => import("@/pages/WebhooksPage").then((m) => ({ default: m.WebhooksPage })),
  "/connections": () => import("@/pages/ConnectionsPage").then((m) => ({ default: m.ConnectionsPage })),
  "/users": () => import("@/pages/UsersPage").then((m) => ({ default: m.UsersPage })),
  "/audit": () => import("@/pages/AuditPage").then((m) => ({ default: m.AuditPage })),
  "/settings": () => import("@/pages/SettingsPage").then((m) => ({ default: m.SettingsPage })),
  "/profile": () => import("@/pages/ProfilePage").then((m) => ({ default: m.ProfilePage })),
} satisfies Record<string, () => Promise<{ default: ComponentType }>>

export type RoutePath = keyof typeof loaders

const RELOAD_FLAG = "ferrum:chunk-reload"

/** A tab left open across an upgrade still references the previous build's
 * hashed chunk names, which the new binary no longer serves — every lazy
 * page then failed with "Failed to fetch dynamically imported module" until
 * a manual refresh. Reload once to pick up the new index.html; the session
 * flag stops a genuinely broken chunk from reloading forever. */
function withStaleBuildRecovery<T>(load: () => Promise<T>): () => Promise<T> {
  return () =>
    load().then(
      (mod) => {
        try {
          sessionStorage.removeItem(RELOAD_FLAG)
        } catch {
          // storage blocked — recovery just can't de-duplicate
        }
        return mod
      },
      (err: unknown) => {
        let alreadyReloaded = true
        try {
          alreadyReloaded = sessionStorage.getItem(RELOAD_FLAG) === "1"
          if (!alreadyReloaded) sessionStorage.setItem(RELOAD_FLAG, "1")
        } catch {
          // storage blocked: without a flag we can't rule out a reload loop
        }
        if (!alreadyReloaded && typeof navigator !== "undefined" && navigator.onLine) {
          window.location.reload()
          return new Promise<T>(() => {}) // the page is going away
        }
        throw err
      },
    )
}

export const pages = Object.fromEntries(
  Object.entries(loaders).map(([path, load]) => [path, lazy(withStaleBuildRecovery(load))]),
) as Record<RoutePath, LazyExoticComponent<ComponentType>>

const prefetched = new Set<string>()

/** Starts downloading the chunk behind an in-app path. Safe to call on every
 * hover: each chunk is requested at most once, and a failure is swallowed
 * (the real navigation will surface it). */
export function prefetchRoute(to: string): void {
  const segment = `/${to.split(/[/?#]/)[1] ?? ""}`
  if (prefetched.has(segment)) return
  const load = (loaders as Record<string, (() => Promise<unknown>) | undefined>)[segment]
  if (!load) return
  prefetched.add(segment)
  load().catch(() => prefetched.delete(segment))
}
