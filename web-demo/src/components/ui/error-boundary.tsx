import { Component, type ErrorInfo, type ReactNode } from "react"

interface ErrorBoundaryProps {
  children: ReactNode
  /** Compact fallback for embedded regions (widgets); default covers pages. */
  variant?: "page" | "widget"
}

/** Keeps one broken region from taking down the page — used around each
 * dashboard widget and around lazy page content. */
export class ErrorBoundary extends Component<ErrorBoundaryProps, { error: Error | null }> {
  state = { error: null as Error | null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Render error:", error, info.componentStack)
  }

  render() {
    if (!this.state.error) return this.props.children
    if (this.props.variant === "widget") {
      return (
        <div className="flex h-full min-h-24 flex-col items-center justify-center gap-1 p-4 text-center" role="alert">
          <p className="text-xs font-medium text-[var(--status-error)]">This widget failed to render</p>
          <p className="max-w-60 truncate text-[11px] text-[var(--text-faint)]">{this.state.error.message}</p>
        </div>
      )
    }
    return (
      <div className="flex flex-col items-center justify-center gap-2 p-12 text-center" role="alert">
        <p className="text-sm font-medium">Something went wrong on this page</p>
        <p className="max-w-md text-xs text-[var(--text-muted)]">{this.state.error.message}</p>
        <button
          className="mt-2 rounded-md border border-[var(--border)] px-3 py-1.5 text-xs font-medium hover:bg-[var(--bg-muted)]"
          onClick={() => this.setState({ error: null })}
        >
          Try again
        </button>
      </div>
    )
  }
}
