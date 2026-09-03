import { useQuery, useQueryClient } from "@tanstack/react-query"
import { createContext, type ReactNode, useContext, useEffect, useMemo } from "react"
import { api, ApiError, onUnauthorized, type User } from "./api"

interface AuthState {
  user: User | null
  needsSetup: boolean
  loading: boolean
  refresh: () => void
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

  // When an API call hits a 401 while we believed we were logged in, the
  // session has expired mid-use: clear the cache so the user lands on the
  // login page instead of a wall of error states. A 401 with no cached user
  // is the normal signed-out state (the initial /auth/me probe) — clearing
  // there would wipe the cache, refetch, 401 again, and loop forever.
  useEffect(
    () =>
      onUnauthorized(() => {
        if (queryClient.getQueryData<User>(["auth", "me"])) {
          queryClient.clear()
        }
      }),
    [queryClient],
  )

  // Memoized so unrelated re-renders of the provider don't re-render every
  // useAuth consumer (the refresh closure is stable across renders).
  const value = useMemo<AuthState>(
    () => ({
      user: meQuery.data ?? null,
      needsSetup: setupQuery.data?.needsSetup ?? false,
      loading: setupQuery.isLoading || (setupQuery.data?.needsSetup === false && meQuery.isLoading),
      refresh: () => {
        queryClient.invalidateQueries({ queryKey: ["auth"] })
      },
    }),
    [meQuery.data, meQuery.isLoading, setupQuery.data?.needsSetup, setupQuery.isLoading, queryClient],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error("useAuth must be used within AuthProvider")
  return ctx
}
