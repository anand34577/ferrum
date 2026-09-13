import { AnimatePresence } from "framer-motion"
import { lazy, Suspense, useEffect, type ReactNode, type ReactElement } from "react"
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom"
import { useQuery } from "@tanstack/react-query"
import { toast } from "sonner"
import { AppShell } from "@/components/layout/AppShell"
import { PageTransition } from "@/components/layout/PageTransition"
import { ErrorBoundary } from "@/components/ui/error-boundary"
import { CardSkeleton } from "@/components/ui/skeleton"
import { api, onErrorCode } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { SetupPage } from "@/pages/SetupPage"
import { LoginPage } from "@/pages/LoginPage"
import { ConsolePage } from "@/pages/ConsolePage"

const AIAssistantPage = lazy(() => import("@/pages/AIAssistantPage").then((m) => ({ default: m.AIAssistantPage })))
const AuditPage = lazy(() => import("@/pages/AuditPage").then((m) => ({ default: m.AuditPage })))
const ClusterPage = lazy(() => import("@/pages/ClusterPage").then((m) => ({ default: m.ClusterPage })))
const BackupsPage = lazy(() => import("@/pages/BackupsPage").then((m) => ({ default: m.BackupsPage })))
const ConnectionsPage = lazy(() => import("@/pages/ConnectionsPage").then((m) => ({ default: m.ConnectionsPage })))
const DashboardPage = lazy(() => import("@/pages/DashboardPage").then((m) => ({ default: m.DashboardPage })))
const OverviewPage = lazy(() => import("@/pages/OverviewPage").then((m) => ({ default: m.OverviewPage })))
const FirewallPage = lazy(() => import("@/pages/FirewallPage").then((m) => ({ default: m.FirewallPage })))
const HAPage = lazy(() => import("@/pages/HAPage").then((m) => ({ default: m.HAPage })))
const InventoryPage = lazy(() => import("@/pages/InventoryPage").then((m) => ({ default: m.InventoryPage })))
const NodeDetailPage = lazy(() => import("@/pages/NodeDetailPage").then((m) => ({ default: m.NodeDetailPage })))
const PoolsPage = lazy(() => import("@/pages/PoolsPage").then((m) => ({ default: m.PoolsPage })))
const PBSPage = lazy(() => import("@/pages/PBSPage").then((m) => ({ default: m.PBSPage })))
const SettingsPage = lazy(() => import("@/pages/SettingsPage").then((m) => ({ default: m.SettingsPage })))
const StoragePage = lazy(() => import("@/pages/StoragePage").then((m) => ({ default: m.StoragePage })))
const TasksPage = lazy(() => import("@/pages/TasksPage").then((m) => ({ default: m.TasksPage })))
const UsersPage = lazy(() => import("@/pages/UsersPage").then((m) => ({ default: m.UsersPage })))
const AlertsPage = lazy(() => import("@/pages/AlertsPage").then((m) => ({ default: m.AlertsPage })))
const TopologyPage = lazy(() => import("@/pages/TopologyPage").then((m) => ({ default: m.TopologyPage })))
const ProfilePage = lazy(() => import("@/pages/ProfilePage").then((m) => ({ default: m.ProfilePage })))
const BulkOperationsPage = lazy(() => import("@/pages/BulkOperationsPage").then((m) => ({ default: m.BulkOperationsPage })))
const WebhooksPage = lazy(() => import("@/pages/WebhooksPage").then((m) => ({ default: m.WebhooksPage })))

function PageFallback() {
  return (
    <div className="space-y-3" aria-busy>
      <CardSkeleton />
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <CardSkeleton key={i} />
        ))}
      </div>
    </div>
  )
}

// Suspense sits INSIDE the animated route element so a lazy chunk loading
// shows the fallback without cutting the outgoing page's exit animation.
function route(element: ReactElement, adminOnly = false): ReactNode {
  return (
    <PageTransition>
      {adminOnly ? (
        <RequireAdmin>
          <Suspense fallback={<PageFallback />}>{element}</Suspense>
        </RequireAdmin>
      ) : (
        <Suspense fallback={<PageFallback />}>{element}</Suspense>
      )}
    </PageTransition>
  )
}

/** Route-level guard mirroring the API's admin enforcement — non-admins
 * are redirected instead of landing on pages whose data calls 403. */
function RequireAdmin({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  if (!user?.isAdmin) return <Navigate to="/" replace />
  return <>{children}</>
}

/** The "/" route: redirects to this account's configured landing page (or
 * the org-wide default) when it isn't Fleet Overview, otherwise just renders
 * it. Shares the ["auth","preferences"] query with ThemeProvider, so this
 * costs no extra request. */
function HomeRoute() {
  const query = useQuery({
    queryKey: ["auth", "preferences"],
    queryFn: () => api.get<{ landingPage?: string }>("/auth/me/preferences"),
    staleTime: 60_000,
  })
  const landing = query.data?.landingPage
  if (landing && landing !== "/") return <Navigate to={landing} replace />
  return <OverviewPage />
}

/** A blocked-by-2FA-policy response (see requireTOTPEnrolled server-side)
 * sends the admin straight to Profile & Security to enroll, instead of a
 * wall of per-page error states across the app. */
function useEnforceTOTPEnrollment() {
  const navigate = useNavigate()
  const location = useLocation()
  useEffect(
    () =>
      onErrorCode((code) => {
        if (code === "totp_required" && location.pathname !== "/profile") {
          toast.error("Your administrator requires two-factor authentication — enable it below to continue.")
          navigate("/profile")
        }
      }),
    [navigate, location.pathname],
  )
}

export default function App() {
  const { user, needsSetup, loading } = useAuth()
  const location = useLocation()
  useEnforceTOTPEnrollment()

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center p-8" aria-busy>
        <div className="w-full max-w-3xl space-y-3">
          <CardSkeleton />
          <CardSkeleton />
        </div>
      </div>
    )
  }

  if (needsSetup) return <SetupPage />
  if (!user) return <LoginPage />

  // The console is a fullscreen, chrome-free view — it must not be wrapped
  // in the sidebar/topbar AppShell like every other authenticated route.
  if (location.pathname === "/console") return <ConsolePage />

  return (
    <AppShell>
      {/* Keyed by path: navigating away from a broken page clears the
          boundary, instead of trapping the user on the fallback forever. */}
      <ErrorBoundary key={location.pathname}>
        <AnimatePresence mode="wait">
          <Routes location={location} key={location.pathname}>
            <Route path="/" element={route(<HomeRoute />)} />
            <Route path="/dashboard" element={route(<DashboardPage />)} />
            <Route path="/inventory" element={route(<InventoryPage />)} />
            <Route path="/nodes/:connId/:node" element={route(<NodeDetailPage />)} />
            <Route path="/topology" element={route(<TopologyPage />)} />
            <Route path="/storage" element={route(<StoragePage />)} />
            <Route path="/pbs" element={route(<PBSPage />)} />
            <Route path="/pools" element={route(<PoolsPage />)} />
            <Route path="/backups" element={route(<BackupsPage />)} />
            <Route path="/ha" element={route(<HAPage />)} />
            <Route path="/cluster" element={route(<ClusterPage />, true)} />
            <Route path="/firewall" element={route(<FirewallPage />)} />
            <Route path="/alerts" element={route(<AlertsPage />)} />
            <Route path="/tasks" element={route(<TasksPage />)} />
            <Route path="/ai-assistant" element={route(<AIAssistantPage />)} />
            <Route path="/bulk-operations" element={route(<BulkOperationsPage />, true)} />
            <Route path="/webhooks" element={route(<WebhooksPage />, true)} />
            <Route path="/connections" element={route(<ConnectionsPage />, true)} />
            <Route path="/users" element={route(<UsersPage />, true)} />
            <Route path="/audit" element={route(<AuditPage />, true)} />
            <Route path="/settings" element={route(<SettingsPage />, true)} />
            <Route path="/profile" element={route(<ProfilePage />)} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </AnimatePresence>
      </ErrorBoundary>
    </AppShell>
  )
}
