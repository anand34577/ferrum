import "katex/dist/katex.min.css"
import { Check, Copy } from "lucide-react"
import ReactMarkdown from "react-markdown"
import rehypeKatex from "rehype-katex"
import remarkGfm from "remark-gfm"
import remarkMath from "remark-math"
import { memo } from "react"
import { toast } from "sonner"
import { useCopiedFlag } from "@/lib/useCopiedFlag"
import { cn } from "@/lib/utils"

/**
 * Full CommonMark + GFM (tables, strikethrough, task lists, autolinks) +
 * LaTeX ($inline$ and $$block$$, via KaTeX) rendering for assistant replies.
 * Every element is restyled to the app's design tokens rather than trusting
 * react-markdown's default (unstyled) output.
 *
 * Memoized on `text`: AIAssistantPage re-renders its whole message list on
 * every streamed token (see runCompletion), and without this every already-
 * committed message's markdown — tables, KaTeX, GFM — got fully re-parsed
 * from scratch on every single token of whatever's currently streaming,
 * which is what made scrolling/typing feel frozen mid-response in anything
 * but a brand-new conversation.
 */
export const Markdown = memo(function Markdown({ text }: { text: string }) {
  return (
    <div className="markdown-body space-y-2.5 text-sm leading-relaxed [overflow-wrap:anywhere]">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex]}
        components={{
          p: ({ children }) => <p className="leading-relaxed">{children}</p>,
          a: ({ children, href }) => (
            <a href={href} target="_blank" rel="noreferrer" className="text-brand-500 underline underline-offset-2 hover:text-brand-600">
              {children}
            </a>
          ),
          ul: ({ children }) => <ul className="list-disc space-y-0.5 pl-5">{children}</ul>,
          ol: ({ children }) => <ol className="list-decimal space-y-0.5 pl-5">{children}</ol>,
          li: ({ children, className }) => (
            <li className={cn(className?.includes("task-list-item") && "list-none pl-0 [&>input]:mr-1.5")}>{children}</li>
          ),
          h1: ({ children }) => <h1 className="mt-1 text-base font-semibold">{children}</h1>,
          h2: ({ children }) => <h2 className="mt-1 text-[0.95rem] font-semibold">{children}</h2>,
          h3: ({ children }) => <h3 className="mt-1 text-sm font-semibold">{children}</h3>,
          h4: ({ children }) => <h4 className="mt-1 text-sm font-semibold">{children}</h4>,
          hr: () => <hr className="border-[var(--border)]" />,
          blockquote: ({ children }) => (
            <blockquote className="border-l-2 border-[var(--border-strong)] pl-3 text-[var(--text-muted)]">{children}</blockquote>
          ),
          table: ({ children }) => (
            <div className="overflow-x-auto rounded-md border border-[var(--border)]">
              <table className="w-full border-collapse text-xs">{children}</table>
            </div>
          ),
          thead: ({ children }) => <thead className="bg-[var(--bg-muted)]">{children}</thead>,
          th: ({ children }) => <th className="border-b border-[var(--border)] px-2.5 py-1.5 text-left font-semibold">{children}</th>,
          td: ({ children }) => <td className="border-b border-[var(--border)] px-2.5 py-1.5 align-top last:border-b-0">{children}</td>,
          code: ({ className, children, ...props }) => {
            const inline = !/language-/.test(className ?? "") && !String(children).includes("\n")
            if (inline) {
              return (
                <code className="rounded-sm bg-[var(--bg-muted)] px-1 py-0.5 font-mono text-[0.9em]" {...props}>
                  {children}
                </code>
              )
            }
            const lang = /language-(\w+)/.exec(className ?? "")?.[1] ?? ""
            return <CodeBlock lang={lang} code={String(children).replace(/\n$/, "")} />
          },
          pre: ({ children }) => <>{children}</>,
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
})

function CodeBlock({ lang, code }: { lang: string; code: string }) {
  const [copied, flashCopied] = useCopiedFlag()
  function copy() {
    navigator.clipboard.writeText(code).then(() => {
      flashCopied()
      toast.success("Copied to clipboard")
    }).catch(() => toast.error("Could not copy to clipboard"))
  }
  return (
    <div className="overflow-hidden rounded-md border border-[var(--border)]">
      <div className="flex items-center justify-between bg-[var(--bg-muted)] px-3 py-1.5 text-xs text-[var(--text-muted)]">
        <span className="font-mono">{lang || "text"}</span>
        <button type="button" onClick={copy} className="inline-flex items-center gap-1 hover:text-[var(--text)]">
          {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />} {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <pre className="overflow-x-auto bg-[var(--bg-surface)] px-3 py-2.5 text-xs leading-relaxed">
        <code>{code}</code>
      </pre>
    </div>
  )
}
