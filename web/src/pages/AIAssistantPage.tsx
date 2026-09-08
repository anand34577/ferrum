import { useQuery } from "@tanstack/react-query"
import {
  Activity,
  AlertTriangle,
  ArrowDown,
  Bot,
  Check,
  CheckCircle2,
  ChevronDown,
  Copy,
  History,
  Loader2,
  MessageSquarePlus,
  RotateCcw,
  Send,
  Sparkles,
  Square,
  Terminal,
  Trash2,
  User as UserIcon,
  Wrench,
  XCircle,
} from "lucide-react"
import { useEffect, useMemo, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { toast } from "sonner"
import { Markdown } from "@/components/ai/Markdown"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { EmptyState } from "@/components/ui/empty-state"
import { ErrorState } from "@/components/ui/error-state"
import { PageHeader } from "@/components/ui/page-header"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type ToolCallRecord, type UsableAIProvider } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { type Conversation, type DisplayMessage, type ToolCallEntry, useAIConversations } from "@/lib/useAIConversations"
import { cn, formatRelativeTime } from "@/lib/utils"

function newId() {
  return Math.random().toString(36).slice(2)
}

interface ToolActivity {
  name: string
  status: "running" | "ok" | "error"
  args?: unknown
  result?: string
}

const TOOL_LABELS: Record<string, string> = {
  list_connections: "Checking connections",
  list_nodes: "Checking nodes",
  get_node_status: "Checking node status",
  list_guests: "Listing guests",
  get_guest_status: "Checking guest status",
  guest_power_action: "Running power action",
  list_alerts: "Checking alerts",
  cluster_status: "Checking cluster status",
  list_storage: "Checking storage",
  list_pools: "Checking pools",
}

function toolLabel(name: string) {
  return TOOL_LABELS[name] ?? `Using ${name}`
}

/** Pretty-prints a raw JSON string for display, falling back to the raw
 * text if it doesn't parse (defensive only — the server always sends valid
 * JSON or omits the field entirely). */
function formatJSON(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

/** Parses one server-sent event line: either an OpenAI-shaped content delta,
 * or one of Ferrum's own envelopes reporting tool-call progress / a mid-
 * stream error (see internal/api/ai_chat.go). */
function parseSSELine(line: string): {
  delta?: string
  toolCall?: { name: string; args?: unknown }
  toolResult?: { name: string; ok: boolean; result?: string }
  error?: string
  done?: boolean
} {
  if (!line.startsWith("data:")) return {}
  const payload = line.slice(5).trim()
  if (payload === "[DONE]") return { done: true }
  try {
    const parsed = JSON.parse(payload)
    if (parsed.ferrum_error) return { error: parsed.ferrum_error }
    if (parsed.ferrum_tool_call) return { toolCall: { name: parsed.ferrum_tool_call.name, args: parsed.ferrum_tool_call.args } }
    if (parsed.ferrum_tool_result)
      return { toolResult: { name: parsed.ferrum_tool_result.name, ok: !!parsed.ferrum_tool_result.ok, result: parsed.ferrum_tool_result.result } }
    return { delta: parsed.choices?.[0]?.delta?.content ?? undefined }
  } catch {
    return {}
  }
}

interface SlashCommand {
  cmd: string
  label: string
  prompt?: string // omitted for purely-local commands like /help
}

const SLASH_COMMANDS: SlashCommand[] = [
  { cmd: "/health", label: "Overall fleet health summary", prompt: "Give me an overall health summary of the fleet — anything that needs attention right now?" },
  { cmd: "/nodes", label: "Status of every node", prompt: "Show me the current status of every node across all connections — CPU, memory, and uptime." },
  { cmd: "/guests", label: "List all VMs and containers", prompt: "List all VMs and containers with their current status and which node they're on." },
  { cmd: "/alerts", label: "Active alerts", prompt: "What alerts are currently active, and how severe are they?" },
  { cmd: "/storage", label: "Storage usage", prompt: "Show storage usage across all storage pools." },
  { cmd: "/pools", label: "Resource pools", prompt: "List all resource pools and their members." },
  { cmd: "/help", label: "List available commands" },
]

const SUGGESTIONS = [
  "What's the overall health of my fleet right now?",
  "List every VM and container that's currently stopped.",
  "Are there any active alerts I should know about?",
]

const HELP_TEXT = `I'm specialized for **Proxmox VE / Ferrum fleet management** — nodes, guests, storage, alerts, backups, HA, and cluster configuration. I can look up your real infrastructure data using tools, and (for admins) start/stop/reboot guests.

**Available commands:**
${SLASH_COMMANDS.filter((c) => c.prompt).map((c) => `- \`${c.cmd}\` — ${c.label}`).join("\n")}

Type a command, or just ask a question about your fleet in plain language.`

/** One flattened, selectable entry combining a provider and one of its
 * models — the Select shows "Provider · Model label" and stores the model
 * row's ID, which is all /ai/chat needs (it resolves the provider via FK). */
interface FlatModel {
  modelRowId: string
  providerName: string
  label: string
  isDefault: boolean
}

function flattenModels(providers: UsableAIProvider[]): FlatModel[] {
  return providers.flatMap((p) => p.models.map((m) => ({ modelRowId: m.id, providerName: p.providerName, label: m.label, isDefault: m.isDefault })))
}

export function AIAssistantPage() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const confirm = useConfirm()
  const [activityOpen, setActivityOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)

  const providersQuery = useQuery({
    queryKey: ["ai", "providers"],
    queryFn: () => api.get<UsableAIProvider[]>("/ai/providers"),
  })
  const providers = providersQuery.data ?? []
  const flatModels = useMemo(() => flattenModels(providers), [providers])
  const defaultModelId = flatModels.find((m) => m.isDefault)?.modelRowId ?? flatModels[0]?.modelRowId ?? ""

  const { conversations, active, activeId, setActiveId, createConversation, deleteConversation, updateConversation, setMessages } =
    useAIConversations(defaultModelId)

  const [input, setInput] = useState("")
  const [streamingText, setStreamingText] = useState<string | null>(null)
  const [toolActivity, setToolActivity] = useState<ToolActivity[]>([])
  const [streaming, setStreaming] = useState(false)
  const [slashIndex, setSlashIndex] = useState(0)
  const scrollRef = useRef<HTMLDivElement>(null)
  const abortRef = useRef<AbortController | null>(null)
  // ChatGPT/Claude-style "stick to bottom": auto-follow new tokens only
  // while the reader is already at (or near) the bottom. Scroll away to
  // reread something mid-stream and it stays put instead of yanking back
  // down on every token; send a new message and it resumes following.
  //
  // The ref is what the per-token follow effect reads (it has to be current
  // synchronously, before the next render); the state mirrors it purely so
  // the "Jump to latest" affordance can render.
  const stickToBottomRef = useRef(true)
  const [atBottom, setAtBottom] = useState(true)
  const [newBelow, setNewBelow] = useState(false)

  const modelId = active?.modelId || defaultModelId
  const selectedModel = flatModels.find((m) => m.modelRowId === modelId)
  const committedMessages = active?.messages ?? []
  const displayMessages: (DisplayMessage & { streaming?: boolean })[] =
    streaming ? [...committedMessages, { id: "__streaming__", role: "assistant", content: streamingText ?? "", streaming: true }] : committedMessages

  const slashMatches = useMemo(() => {
    if (!input.startsWith("/") || input.includes(" ") || input.includes("\n")) return []
    return SLASH_COMMANDS.filter((c) => c.cmd.startsWith(input.toLowerCase()))
  }, [input])

  useEffect(() => setSlashIndex(0), [slashMatches.length])

  // Anything within this many pixels of the bottom still counts as "at the
  // bottom": sub-pixel rounding, an image or code block that just changed
  // height, and a fast scroll's inertia all land a little short of an exact
  // match, and none of them mean the reader has taken the viewport over.
  const BOTTOM_THRESHOLD = 120

  /** Return to the latest content and hand scrolling back to auto-follow. */
  function follow(behavior: ScrollBehavior = "auto") {
    const el = scrollRef.current
    stickToBottomRef.current = true
    setAtBottom(true)
    setNewBelow(false)
    el?.scrollTo({ top: el.scrollHeight, behavior })
  }

  // Switching conversations should land at that conversation's latest
  // message immediately, not smooth-scroll from wherever the previous one
  // happened to be — and resume auto-follow for it. The rAF re-run is
  // because the incoming messages have not been laid out on this tick, so
  // scrollHeight is still the outgoing conversation's.
  useEffect(() => {
    follow()
    const raf = requestAnimationFrame(() => follow())
    return () => cancelAnimationFrame(raf)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeId])

  // Auto-follow. Assigning scrollTop, rather than starting a smooth scrollTo
  // per token, is what keeps this incremental: each render the content grew
  // a little and the viewport moves by that same little amount, instead of
  // restarting an animation towards a target that has already moved again.
  // While the reader is detached this touches nothing — the growing content
  // only raises the "there is more below" flag.
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    if (stickToBottomRef.current) el.scrollTop = el.scrollHeight
    else setNewBelow(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [displayMessages.length, streamingText, toolActivity.length])

  function handleScroll() {
    const el = scrollRef.current
    if (!el) return
    const near = el.scrollHeight - el.scrollTop - el.clientHeight <= BOTTOM_THRESHOLD
    stickToBottomRef.current = near
    setAtBottom(near)
    if (near) setNewBelow(false)
  }

  async function runCompletion(convId: string, history: DisplayMessage[], useModelId: string) {
    setStreaming(true)
    setStreamingText("")
    setToolActivity([])
    const controller = new AbortController()
    abortRef.current = controller
    let assistantText = ""
    let midStreamError: string | null = null
    // Mirrors the toolActivity state updates below, but as a plain array so
    // the finished tool calls can be attached to the committed message —
    // toolActivity itself is cleared once streaming ends (it's "what's
    // happening right now"), so without this the evidence for an answer
    // would vanish the moment the response finished.
    const toolLog: ToolCallEntry[] = []

    try {
      const res = await fetch("/api/v1/ai/chat", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        signal: controller.signal,
        body: JSON.stringify({
          modelId: useModelId || undefined,
          messages: history.map(({ role, content }) => ({ role, content })),
        }),
      })
      if (!res.ok || !res.body) {
        const data = await res.json().catch(() => undefined)
        throw new Error(data?.error ?? `Request failed (${res.status})`)
      }

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ""
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split("\n")
        buffer = lines.pop() ?? ""
        for (const line of lines) {
          const evt = parseSSELine(line)
          if (evt.delta) {
            assistantText += evt.delta
            setStreamingText(assistantText)
          } else if (evt.toolCall) {
            const { name, args } = evt.toolCall
            setToolActivity((prev) => [...prev, { name, args, status: "running" }])
            toolLog.push({ name, args, ok: true })
          } else if (evt.toolResult) {
            const { name, ok, result } = evt.toolResult
            setToolActivity((prev) => {
              const idx = [...prev].reverse().findIndex((t) => t.name === name && t.status === "running")
              if (idx === -1) return prev
              const realIdx = prev.length - 1 - idx
              const next = [...prev]
              next[realIdx] = { ...next[realIdx], status: ok ? "ok" : "error", result }
              return next
            })
            const logIdx = [...toolLog].reverse().findIndex((t) => t.name === name && t.result === undefined)
            if (logIdx !== -1) {
              const realIdx = toolLog.length - 1 - logIdx
              toolLog[realIdx] = { ...toolLog[realIdx], ok, result }
            }
          } else if (evt.error) {
            midStreamError = evt.error
          }
        }
      }
      if (midStreamError) throw new Error(midStreamError)
    } catch (err) {
      if (!(err instanceof Error && err.name === "AbortError")) {
        toast.error(err instanceof Error ? err.message : "The AI provider didn't respond")
      }
    } finally {
      setStreaming(false)
      setStreamingText(null)
      setToolActivity([])
      abortRef.current = null
      // Commit whatever came back — including a partial answer if the user
      // hit Stop midway, same as every mainstream chat product. Tool calls
      // are attached even when there's no final text yet (e.g. aborted
      // mid-loop) so the evidence of what was checked isn't lost.
      if (assistantText || toolLog.length > 0) {
        setMessages(convId, [
          ...history,
          { id: newId(), role: "assistant", content: assistantText, toolCalls: toolLog.length > 0 ? toolLog : undefined },
        ])
      }
    }
  }

  async function send(overrideText?: string) {
    const text = (overrideText ?? input).trim()
    if (!text || streaming) return
    setInput("")
    follow()

    const convId = activeId ?? createConversation(modelId)
    const userMsg: DisplayMessage = { id: newId(), role: "user", content: text }
    const history = [...committedMessages, userMsg]
    setMessages(convId, history)
    // Park the message just sent near the top of the viewport so the answer
    // has room to render beneath it instead of both hugging the bottom edge.
    // A thread too short to scroll that far simply stays bottom-anchored,
    // which is the right result there anyway.
    requestAnimationFrame(() =>
      scrollRef.current?.querySelector(`[data-mid="${userMsg.id}"]`)?.scrollIntoView({ block: "start" }),
    )
    await runCompletion(convId, history, modelId)
  }

  function runSlashCommand(command: SlashCommand) {
    setInput("")
    follow()
    if (!command.prompt) {
      // Purely local — answer immediately without involving the provider.
      const convId = activeId ?? createConversation(modelId)
      const userMsg: DisplayMessage = { id: newId(), role: "user", content: command.cmd }
      const helpMsg: DisplayMessage = { id: newId(), role: "assistant", content: HELP_TEXT }
      setMessages(convId, [...committedMessages, userMsg, helpMsg])
      return
    }
    send(command.prompt)
  }

  function stop() {
    abortRef.current?.abort()
  }

  async function regenerate() {
    if (!active || streaming) return
    const lastUserIdx = [...committedMessages].reverse().findIndex((m) => m.role === "user")
    if (lastUserIdx === -1) return
    const cutoff = committedMessages.length - lastUserIdx
    const history = committedMessages.slice(0, cutoff)
    follow()
    setMessages(active.id, history)
    await runCompletion(active.id, history, modelId)
  }

  async function handleDelete(id: string, title: string) {
    if (await confirm({ title: `Delete "${title}"?`, description: "This conversation is only stored in this browser and can't be recovered." })) {
      deleteConversation(id)
    }
  }

  function copyMessage(content: string) {
    navigator.clipboard.writeText(content).then(() => toast.success("Copied to clipboard"))
  }

  const lastMessageIsAssistant = committedMessages.at(-1)?.role === "assistant"

  if (providersQuery.isError) {
    return (
      <div className="space-y-4">
        <PageHeader title="AI Assistant" description="Specialized for Proxmox VE fleet management through Ferrum." icon={Sparkles} />
        <ErrorState title="Couldn't load AI providers" onRetry={providersQuery.refetch} />
      </div>
    )
  }

  if (providersQuery.isLoading) {
    return (
      <div className="space-y-4" aria-busy>
        <PageHeader title="AI Assistant" description="Specialized for Proxmox VE fleet management through Ferrum." icon={Sparkles} />
        <Skeleton className="h-96 w-full" />
      </div>
    )
  }

  if (flatModels.length === 0) {
    return (
      <div className="space-y-4">
        <PageHeader title="AI Assistant" description="Specialized for Proxmox VE fleet management through Ferrum." icon={Sparkles} />
        <EmptyState
          icon={Bot}
          title="No AI model configured"
          description={
            user?.isAdmin
              ? "Add an OpenAI-compatible provider and at least one model (OpenAI, Ollama, LM Studio, LocalAI, ...) in Settings to enable the assistant."
              : "Ask an admin to configure an AI provider and model in Settings to enable the assistant."
          }
          action={user?.isAdmin ? <Button onClick={() => navigate("/settings")}>Go to Settings</Button> : undefined}
        />
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col space-y-4">
      <PageHeader
        title="AI Assistant"
        description="Specialized for Proxmox VE fleet management — not a general-purpose chatbot."
        icon={Sparkles}
        actions={
          <Button size="sm" variant="secondary" onClick={() => setActivityOpen(true)}>
            <Activity className="h-3.5 w-3.5" /> Activity
          </Button>
        }
      />
      <ActivityPanel open={activityOpen} onOpenChange={setActivityOpen} />

      <div className="flex min-h-0 flex-1 gap-4 overflow-hidden">
        {/* Conversation history sidebar — desktop only; mobile reaches the
            same list through the History button in the chat panel's header. */}
        <div className="hidden w-60 shrink-0 flex-col gap-2 md:flex">
          <Button size="sm" variant="secondary" className="justify-start" onClick={() => createConversation(modelId)}>
            <MessageSquarePlus className="h-3.5 w-3.5" /> New chat
          </Button>
          <div className="flex-1 space-y-1 overflow-y-auto">
            <ConversationList
              conversations={conversations}
              activeId={activeId}
              onSelect={setActiveId}
              onDelete={handleDelete}
            />
          </div>
        </div>

        {/* Mobile conversation history — the sidebar above is hidden below
            md, so this is the only way to switch or delete a conversation
            on a narrow viewport. */}
        <Dialog open={historyOpen} onOpenChange={setHistoryOpen}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Conversations</DialogTitle>
            </DialogHeader>
            <Button size="sm" variant="secondary" className="justify-start" onClick={() => { createConversation(modelId); setHistoryOpen(false) }}>
              <MessageSquarePlus className="h-3.5 w-3.5" /> New chat
            </Button>
            <div className="max-h-[60vh] space-y-1 overflow-y-auto">
              <ConversationList
                conversations={conversations}
                activeId={activeId}
                onSelect={(id) => { setActiveId(id); setHistoryOpen(false) }}
                onDelete={handleDelete}
              />
            </div>
          </DialogContent>
        </Dialog>

        {/* Chat panel */}
        <Card className="flex flex-1 flex-col overflow-hidden">
          <div className="flex items-center justify-between gap-2 border-b border-[var(--border)] bg-[var(--bg-elevated)]/60 px-4 py-2.5 backdrop-blur-sm">
            <Select
              value={modelId}
              onValueChange={(v) => (active ? updateConversation(active.id, { modelId: v }) : undefined)}
            >
              <SelectTrigger
                className="w-auto max-w-[60%] min-w-0 gap-2 overflow-hidden sm:max-w-xs"
                title={selectedModel ? `${selectedModel.providerName} · ${selectedModel.label}` : undefined}
              >
                <SelectValue className="min-w-0 flex-1 truncate text-left" />
              </SelectTrigger>
              <SelectContent>
                {flatModels.map((m) => (
                  <SelectItem key={m.modelRowId} value={m.modelRowId}>
                    <span className="block max-w-[16rem] truncate">{m.providerName} · {m.label}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <div className="flex shrink-0 items-center gap-2">
              <Badge variant="brand" className="hidden sm:inline-flex">Fleet-focused</Badge>
              <Button size="sm" variant="ghost" className="md:hidden" onClick={() => setHistoryOpen(true)}>
                <History className="h-3.5 w-3.5" /> History
              </Button>
              <Button size="sm" variant="ghost" className="md:hidden" onClick={() => createConversation(modelId)}>
                <MessageSquarePlus className="h-3.5 w-3.5" /> New
              </Button>
            </div>
          </div>

          <CardContent className="relative flex-1 overflow-hidden p-0">
            <div ref={scrollRef} onScroll={handleScroll} className="h-full space-y-5 overflow-y-auto p-4">
              {displayMessages.length === 0 ? (
                <div className="flex h-full animate-in flex-col items-center justify-center gap-4 fade-in text-center duration-500">
                  <div className="flex h-12 w-12 items-center justify-center rounded-full bg-gradient-to-br from-brand-500/20 to-brand-600/5 text-brand-500 shadow-sm ring-1 ring-[var(--border)]">
                    <Sparkles className="h-6 w-6" />
                  </div>
                  <p className="max-w-sm text-sm text-[var(--text-muted)]">
                    Ask about your Proxmox fleet — nodes, guests, storage, alerts, backups. Type <code className="rounded-sm bg-[var(--bg-muted)] px-1 py-0.5 text-xs">/</code> for quick commands.
                  </p>
                  <div className="flex max-w-md flex-wrap justify-center gap-1.5">
                    {SUGGESTIONS.map((s) => (
                      <button
                        key={s}
                        type="button"
                        onClick={() => setInput(s)}
                        className="rounded-full border border-[var(--border)] bg-[var(--bg-surface)] px-3.5 py-1.5 text-xs text-[var(--text-muted)] shadow-sm transition-all hover:-translate-y-0.5 hover:border-brand-500/50 hover:text-[var(--text)] hover:shadow-md"
                      >
                        {s}
                      </button>
                    ))}
                  </div>
                </div>
              ) : (
                displayMessages.map((m, i) => (
                  <div
                    key={m.id}
                    data-mid={m.id}
                    className={cn(
                      "group flex scroll-mt-4 animate-in gap-3 fade-in slide-in-from-bottom-1 duration-300",
                      m.role === "user" && "flex-row-reverse",
                    )}
                  >
                    <div
                      className={cn(
                        "flex h-7 w-7 shrink-0 items-center justify-center rounded-full shadow-sm ring-1",
                        m.role === "user"
                          ? "bg-brand-600 text-white ring-brand-700/50"
                          : "bg-gradient-to-br from-brand-500/20 to-brand-600/10 text-brand-500 ring-[var(--border)]",
                      )}
                    >
                      {m.role === "user" ? <UserIcon className="h-3.5 w-3.5" /> : <Bot className="h-3.5 w-3.5" />}
                    </div>
                    <div className={cn("min-w-0 max-w-[75%]", m.role === "user" && "flex flex-col items-end")}>
                      {/* Live tool-call activity while this message is streaming, or —
                          once it's committed — the same evidence kept alongside it so
                          "what did it check" is never lost after the answer lands. */}
                      {m.streaming && toolActivity.length > 0 && (
                        <div className="mb-1.5 space-y-1">
                          {toolActivity.map((t, idx) => (
                            <ToolCallPill key={`${t.name}-${idx}`} name={t.name} status={t.status} args={t.args} result={t.result} />
                          ))}
                        </div>
                      )}
                      {!m.streaming && m.toolCalls && m.toolCalls.length > 0 && (
                        <div className="mb-1.5 space-y-1">
                          {m.toolCalls.map((t, idx) => (
                            <ToolCallPill key={`${t.name}-${idx}`} name={t.name} status={t.ok ? "ok" : "error"} args={t.args} result={t.result} />
                          ))}
                        </div>
                      )}
                      {/* An assistant message with no text (e.g. Stop hit before the
                          model produced any) has nothing to show beyond its tool
                          pills above — skip the bubble instead of rendering an empty box. */}
                      {(m.role !== "assistant" || m.content || m.streaming) && (
                        <div
                          className={cn(
                            "rounded-2xl border px-3.5 py-2.5 text-sm",
                            m.role === "user"
                              ? "border-brand-700 bg-brand-600 text-white shadow-sm"
                              : "border-[var(--border)] bg-[var(--bg-surface)] shadow-sm",
                          )}
                        >
                          {m.role === "assistant" ? (
                            m.content ? (
                              <>
                                <Markdown text={m.content} />
                                {m.streaming && <span className="ml-0.5 inline-block h-3.5 w-[2px] translate-y-0.5 animate-pulse bg-brand-500" aria-hidden />}
                              </>
                            ) : m.streaming && toolActivity.length === 0 ? (
                              <ThinkingDots />
                            ) : null
                          ) : (
                            <span className="whitespace-pre-wrap">{m.content}</span>
                          )}
                        </div>
                      )}
                      {m.role === "assistant" && !m.streaming && m.content && (
                        <div className="mt-1 flex items-center gap-2 opacity-0 transition-opacity group-hover:opacity-100">
                          <CopyMessageButton content={m.content} onCopy={copyMessage} />
                          {i === displayMessages.length - 1 && lastMessageIsAssistant && !streaming && (
                            <Button size="sm" variant="ghost" className="h-6 px-2 text-xs" onClick={regenerate}>
                              <RotateCcw className="h-3 w-3" /> Regenerate
                            </Button>
                          )}
                        </div>
                      )}
                    </div>
                  </div>
                ))
              )}
            </div>

            {/* Only once the reader has taken the viewport over. It both says
                "there is more below" and is the way back to auto-follow, so
                taking control is never a dead end. */}
            {!atBottom && displayMessages.length > 0 && (
              <button
                type="button"
                onClick={() => follow("smooth")}
                className="absolute bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 border border-[var(--border)] bg-[var(--bg-elevated)] py-1.5 pr-3 pl-2.5 text-xs text-[var(--text-muted)] shadow-md transition-colors hover:border-[var(--border-strong)] hover:text-[var(--text)] focus-visible:ring-2 focus-visible:ring-[var(--ring)] focus-visible:outline-none"
              >
                <ArrowDown className="h-3.5 w-3.5" aria-hidden />
                {newBelow ? "New messages" : "Jump to latest"}
                {newBelow && <span className="h-1.5 w-1.5 bg-brand-500" aria-hidden />}
              </button>
            )}
          </CardContent>

          <div className="relative border-t border-[var(--border)] p-3">
            {slashMatches.length > 0 && (
              <div className="absolute inset-x-3 bottom-full mb-1.5 overflow-hidden rounded-md border border-[var(--border)] bg-[var(--bg-elevated)] shadow-lg">
                {slashMatches.map((c, idx) => (
                  <button
                    key={c.cmd}
                    type="button"
                    onMouseEnter={() => setSlashIndex(idx)}
                    onClick={() => runSlashCommand(c)}
                    className={cn(
                      "flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm transition-colors",
                      idx === slashIndex ? "bg-[var(--bg-muted)]" : "hover:bg-[var(--bg-surface-hover)]",
                    )}
                  >
                    <Terminal className="h-3.5 w-3.5 shrink-0 text-[var(--text-muted)]" />
                    <span className="font-mono text-xs text-brand-500">{c.cmd}</span>
                    <span className="truncate text-xs text-[var(--text-muted)]">{c.label}</span>
                  </button>
                ))}
              </div>
            )}
            <div className="flex items-end gap-2 rounded-2xl border border-[var(--border)] bg-[var(--bg-surface)] p-1.5 pl-3.5 shadow-sm transition-colors focus-within:border-brand-500/60 focus-within:ring-2 focus-within:ring-[var(--ring)]">
              <textarea
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => {
                  if (slashMatches.length > 0) {
                    if (e.key === "ArrowDown") {
                      e.preventDefault()
                      setSlashIndex((i) => (i + 1) % slashMatches.length)
                      return
                    }
                    if (e.key === "ArrowUp") {
                      e.preventDefault()
                      setSlashIndex((i) => (i - 1 + slashMatches.length) % slashMatches.length)
                      return
                    }
                    if (e.key === "Tab" || e.key === "Enter") {
                      e.preventDefault()
                      runSlashCommand(slashMatches[slashIndex])
                      return
                    }
                    if (e.key === "Escape") {
                      setInput("")
                      return
                    }
                  }
                  if (e.key === "Enter" && !e.shiftKey) {
                    e.preventDefault()
                    send()
                  }
                }}
                placeholder="Ask about your fleet, or type / for commands… (Shift+Enter for a new line)"
                rows={1}
                className="max-h-32 flex-1 resize-none bg-transparent py-1.5 text-sm outline-none"
              />
              {streaming ? (
                <Button variant="destructive" className="shrink-0 rounded-full" size="icon" onClick={stop} title="Stop">
                  <Square className="h-3.5 w-3.5" />
                </Button>
              ) : (
                <Button className="shrink-0 rounded-full" size="icon" onClick={() => send()} disabled={!input.trim()} title="Send">
                  <Send className="h-3.5 w-3.5" />
                </Button>
              )}
            </div>
          </div>
        </Card>
      </div>
    </div>
  )
}

/**
 * "What has the AI done on my behalf" — every tool call this account has
 * triggered, via the chat's own tool loop or an external MCP client,
 * successful or not. Backed by GET /ai/activity (self-scoped; admins get a
 * system-wide equivalent in the Audit Log page).
 */
function ActivityPanel({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const query = useQuery({
    queryKey: ["ai", "activity"],
    queryFn: () => api.get<ToolCallRecord[]>("/ai/activity"),
    enabled: open,
    refetchInterval: open ? 5000 : false,
  })
  const records = query.data ?? []

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Activity className="h-4 w-4" /> Your AI &amp; MCP activity
          </DialogTitle>
        </DialogHeader>
        <div className="max-h-[60vh] space-y-1.5 overflow-y-auto">
          {query.isLoading ? (
            <div className="space-y-1.5" aria-busy>
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
              <Skeleton className="h-12 w-full" />
            </div>
          ) : records.length === 0 ? (
            <p className="py-6 text-center text-sm text-[var(--text-muted)]">No tool calls yet — anything the assistant looks up or does on your behalf will show up here.</p>
          ) : (
            records.map((r) => (
              <details key={r.id} className="group rounded-md border border-[var(--border)] px-3 py-2 text-sm">
                <summary className="flex cursor-pointer list-none items-start gap-2.5 [&::-webkit-details-marker]:hidden">
                  {r.ok ? <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0 text-[var(--status-ok)]" /> : <XCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-[var(--status-error)]" />}
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="font-mono text-xs">{toolLabel(r.tool)}</span>
                      <Badge variant={r.source === "mcp" ? "brand" : "outline"}>{r.source === "mcp" ? "MCP" : "Chat"}</Badge>
                      {r.username && <span className="text-xs text-[var(--text-muted)]">{r.username}</span>}
                    </div>
                    {r.error && <p className="mt-0.5 truncate text-xs text-[var(--status-error)]">{r.error}</p>}
                    <p className="mt-0.5 text-xs text-[var(--text-muted)]">{formatRelativeTime(r.createdAt)}</p>
                  </div>
                  {r.args && <ChevronDown className="mt-0.5 h-3.5 w-3.5 shrink-0 opacity-50 transition-transform group-open:rotate-180" />}
                </summary>
                {r.args && (
                  <pre className="mt-2 max-h-40 overflow-auto rounded-sm bg-[var(--bg-muted)] p-1.5 font-mono text-[11px] whitespace-pre-wrap break-all">
                    {formatJSON(r.args)}
                  </pre>
                )}
              </details>
            ))
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

/** The conversation-switcher list — shared by the desktop sidebar and the
 * mobile History dialog so they can never drift into two implementations. */
function ConversationList({
  conversations,
  activeId,
  onSelect,
  onDelete,
}: {
  conversations: Conversation[]
  activeId: string | null
  onSelect: (id: string) => void
  onDelete: (id: string, title: string) => void
}) {
  if (conversations.length === 0) {
    return <p className="px-2 py-4 text-center text-xs text-[var(--text-muted)]">No conversations yet</p>
  }
  return (
    <>
      {conversations.map((c) => (
        <button
          key={c.id}
          type="button"
          onClick={() => onSelect(c.id)}
          className={cn(
            "group relative flex w-full items-center justify-between gap-1 rounded-lg py-2 pr-2.5 pl-3.5 text-left text-sm transition-colors",
            c.id === activeId ? "bg-[var(--bg-muted)] text-[var(--text)]" : "text-[var(--text-muted)] hover:bg-[var(--bg-surface-hover)]",
          )}
        >
          {c.id === activeId && <span className="absolute top-1.5 bottom-1.5 left-0 w-[3px] rounded-full bg-brand-500" aria-hidden />}
          <span className="min-w-0 flex-1">
            <span className="block truncate">{c.title}</span>
            <span className="block text-[10px] text-[var(--text-muted)]">{formatRelativeTime(c.updatedAt)}</span>
          </span>
          <Trash2
            className="h-3 w-3 shrink-0 opacity-0 transition-opacity hover:text-[var(--status-error)] group-hover:opacity-100"
            onClick={(e) => {
              e.stopPropagation()
              onDelete(c.id, c.title)
            }}
          />
        </button>
      ))}
    </>
  )
}

/** A single tool-call's status, expandable to its arguments and result —
 * used both for the live in-progress list and for the ones persisted onto a
 * finished message, so "what did it check and what came back" is never a
 * dead end. Uses a native <details> element rather than a controlled-state
 * accordion: no JS needed to track which pill is open. */
function ToolCallPill({ name, status, args, result }: { name: string; status: "running" | "ok" | "error"; args?: unknown; result?: string }) {
  const hasDetails = args !== undefined || !!result
  const body = (
    <summary className="flex cursor-pointer list-none items-center gap-1.5 px-2.5 py-1 marker:content-none [&::-webkit-details-marker]:hidden">
      {status === "running" ? (
        <Loader2 className="h-3 w-3 shrink-0 animate-spin text-brand-500" />
      ) : status === "ok" ? (
        <CheckCircle2 className="h-3 w-3 shrink-0 text-[var(--status-ok)]" />
      ) : (
        <AlertTriangle className="h-3 w-3 shrink-0 text-[var(--status-error)]" />
      )}
      <Wrench className="h-3 w-3 shrink-0 opacity-60" />
      <span className="flex-1 truncate">{toolLabel(name)}</span>
      {hasDetails && <ChevronDown className="h-3 w-3 shrink-0 opacity-50 transition-transform group-open:rotate-180" />}
    </summary>
  )
  if (!hasDetails) {
    return <div className="group w-fit animate-in rounded-full border border-[var(--border)] bg-[var(--bg-surface)] fade-in text-xs text-[var(--text-muted)]">{body}</div>
  }
  return (
    <details className="group w-fit animate-in overflow-hidden rounded-2xl border border-[var(--border)] bg-[var(--bg-surface)] fade-in text-xs text-[var(--text-muted)] open:w-full">
      {body}
      <div className="space-y-2 border-t border-[var(--border)] px-2.5 py-2">
        {args !== undefined && (
          <div>
            <div className="mb-0.5 font-medium text-[var(--text-muted)]">Arguments</div>
            <pre className="max-h-40 overflow-auto rounded-md bg-[var(--bg-muted)] p-1.5 font-mono text-[11px] whitespace-pre-wrap break-all">
              {JSON.stringify(args, null, 2)}
            </pre>
          </div>
        )}
        {result && (
          <div>
            <div className="mb-0.5 font-medium text-[var(--text-muted)]">Result</div>
            <pre className="max-h-40 overflow-auto rounded-md bg-[var(--bg-muted)] p-1.5 font-mono text-[11px] whitespace-pre-wrap break-all">{result}</pre>
          </div>
        )}
      </div>
    </details>
  )
}

function ThinkingDots() {
  return (
    <span className="flex items-center gap-1 py-0.5" aria-label="Thinking">
      <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-[var(--text-muted)] [animation-delay:-0.3s]" />
      <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-[var(--text-muted)] [animation-delay:-0.15s]" />
      <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-[var(--text-muted)]" />
    </span>
  )
}

function CopyMessageButton({ content, onCopy }: { content: string; onCopy: (content: string) => void }) {
  const [copied, setCopied] = useState(false)
  return (
    <Button
      size="sm"
      variant="ghost"
      className="h-6 px-2 text-xs"
      onClick={() => {
        onCopy(content)
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
      }}
    >
      {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />} {copied ? "Copied" : "Copy"}
    </Button>
  )
}
