import { useQuery, useQueryClient } from "@tanstack/react-query"
import { createContext, type ReactNode, useContext, useEffect, useMemo } from "react"
import { api, ApiError, onUnauthorized, type User } from "./api"

interface AuthState {
  user: User | null
  needsSetup: boolean
  loading: boolean
  refresh: () => void
  /** Clears local auth state deterministically — does not wait for (or
   * depend on) a subsequent /auth/me round trip. See the comment above
   * signOut's body for why that matters. */
  signOut: () => void
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()

  const setupQuery = useQuery({
    queryKey: ["auth", "setup-status"],
    queryFn: () => api.get<{ needsSetup: boolean }>("/auth/setup-status"),
  })

  const meQuery = useQuery({
    queryKey: ["auth", "me"],
    queryFn: () => api.get<User>("/auth/me"),
    enabled: setupQuery.data?.needsSetup === false,
    retry: false,
    throwOnError: (err) => !(err instanceof ApiError && err.status === 401),
  })

  // signOut is the one place that actually clears local auth state — both
  // the explicit "Sign out" button and the 401 handler below funnel through
  // it, so there's exactly one code path to get right instead of two that
  // can drift.
  //
  // React Query keeps a query's last-good `data` around through a *failed*
  // refetch (only a new *successful* fetch, or removing the query from the
  // cache, replaces it) — invalidateQueries()'s "revalidate in the
  // background" model assumes the stale data is still fine to show while
  // that happens. That assumption is wrong for auth: after logout, the
  // stale "signed in as X" is never fine to keep showing, so this pins
  // ["auth","me"] to null directly instead of waiting for a refetch to 401
  // and hoping the cache update lands before the next render.
  function signOut() {
    // Only drop non-auth data here — clearing ["auth","setup-status"] too
    // (queryClient.clear() did) forces AuthProvider's own setup-status query
    // to refetch, which flips `loading` back to true while it's in flight.
    // That stranded the user on a loading (or failed-refetch) screen instead
    // of jumping straight to the Login page below.
    queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== "auth" })
    queryClient.setQueryData(["auth", "me"], null)
  }

  // A 401 on any API call while we believed we were logged in means the
  // session expired (or was revoked) mid-use — same cleanup as an explicit
  // sign-out. A 401 with no cached user is the normal signed-out state (the
  // initial /auth/me probe); reacting to that would loop forever.
  useEffect(
    () =>
      onUnauthorized(() => {
        if (queryClient.getQueryData<User>(["auth", "me"])) {
          signOut()
        }
      }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [queryClient],
  )

  // Memoized so unrelated re-renders of the provider don't re-render every
  // useAuth consumer (the refresh/signOut closures are stable across renders).
  const value = useMemo<AuthState>(
    () => ({
      user: meQuery.data ?? null,
      needsSetup: setupQuery.data?.needsSetup ?? false,
      loading: setupQuery.isLoading || (setupQuery.data?.needsSetup === false && meQuery.isLoading),
      refresh: () => {
        queryClient.invalidateQueries({ queryKey: ["auth"] })
      },
      signOut,
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [meQuery.data, meQuery.isLoading, setupQuery.data?.needsSetup, setupQuery.isLoading, queryClient],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error("useAuth must be used within AuthProvider")
  return ctx
}
