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
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
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
