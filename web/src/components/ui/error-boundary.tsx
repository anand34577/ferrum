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
        <p className="max-w-md text-xs text-[var(--text-muted)]">
          Try again, or switch to another page — the rest of Ferrum keeps working.
        </p>
        <button
          className="mt-2 rounded-md border border-[var(--border)] px-3 py-1.5 text-xs font-medium hover:bg-[var(--bg-muted)]"
          onClick={() => this.setState({ error: null })}
        >
          Try again
        </button>
        {/* The raw message still matters to the admin filing a bug — one
            click away instead of the headline. */}
        <details className="mt-3 max-w-md text-left">
          <summary className="cursor-pointer text-[11px] text-[var(--text-faint)] hover:text-[var(--text-muted)]">Technical details</summary>
          <pre className="mt-1.5 overflow-x-auto whitespace-pre-wrap break-words rounded-md border border-[var(--border)] bg-[var(--bg-muted)] p-2.5 font-mono text-[11px] text-[var(--text-muted)]">
            {this.state.error.message}
          </pre>
        </details>
      </div>
    )
  }
}
