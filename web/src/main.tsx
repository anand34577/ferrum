import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { BrowserRouter } from "react-router-dom"
import { MotionConfig } from "framer-motion"
import { Toaster } from "sonner"
import App from "./App.tsx"
import "./index.css"
import { AuthProvider } from "./lib/auth"
import { ThemeProvider } from "./lib/theme"
import { ConfirmProvider } from "@/components/ui/confirm-dialog"
import { TooltipProvider } from "@/components/ui/tooltip"

const queryClient = new QueryClient({
  // Realtime-by-default: every query polls unless it opts out (pass
  // `refetchInterval: false`) or sets its own tighter interval — fleet state
  // changes on its own schedule, not the viewer's, so a screen left open
  // must never just go stale. Refetch-on-focus catches the common case (the
  // tab was backgrounded past its interval) immediately on return instead of
  // waiting out the rest of the interval.
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: true, refetchInterval: 20_000 } },
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <MotionConfig reducedMotion="user">
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        {/* Theme sits inside AuthProvider: the preference is loaded from and
            saved to the signed-in user's account in the database. */}
        <ThemeProvider>
          <ConfirmProvider>
            <TooltipProvider delayDuration={300} skipDelayDuration={200}>
              <BrowserRouter>
                <App />
              </BrowserRouter>
            </TooltipProvider>
          </ConfirmProvider>
          <Toaster richColors position="top-right" />
        </ThemeProvider>
      </AuthProvider>
    </QueryClientProvider>
    </MotionConfig>
  </StrictMode>,
)
