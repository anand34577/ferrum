import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import type { AIChatMessage } from "@/lib/api"

/** One completed tool call, kept alongside the assistant message it belongs
 * to so the evidence for an answer (what was checked, and what it returned)
 * survives after streaming ends — not just visible transiently while the
 * response is still typing out. */
export interface ToolCallEntry {
  name: string
  ok: boolean
  args?: unknown
  result?: string
}

export interface DisplayMessage extends AIChatMessage {
  id: string
  toolCalls?: ToolCallEntry[]
  /** The model's chain-of-thought, kept separate from `content` so it can
   * render as a collapsible "Thinking" block instead of part of the answer —
   * see internal/api/ai_chat.go's `ferrum_reasoning` SSE envelope. */
  reasoning?: string
  /** Set when the request failed mid-stream (see ai_chat.go's
   * `ferrum_error`) — kept on the message itself, not just a toast, so the
   * failure stays visible in the transcript after the fact. */
  error?: string
}

export interface Conversation {
  id: string
  title: string
  modelId: string // an ai_provider_models row id (see lib/api.ts AIModel)
  messages: DisplayMessage[]
  updatedAt: string
}

const STORAGE_KEY = "ferrum.ai-assistant.conversations.v1"
const MAX_CONVERSATIONS = 50
// A long-running chat (lots of tool-call evidence attached to each answer)
// keeps growing forever otherwise — cap what's persisted per conversation so
// the blob doesn't creep toward the localStorage quota. Only trims what's
// saved for next time, not the live conversation still on screen.
const MAX_MESSAGES_PER_CONVERSATION = 200

function newId() {
  return Math.random().toString(36).slice(2) + Date.now().toString(36)
}

function load(): Conversation[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

let warnedAboutSaveFailure = false

function save(conversations: Conversation[]) {
  try {
    const trimmed = conversations.slice(0, MAX_CONVERSATIONS).map((c) =>
      c.messages.length > MAX_MESSAGES_PER_CONVERSATION
        ? { ...c, messages: c.messages.slice(-MAX_MESSAGES_PER_CONVERSATION) }
        : c,
    )
    localStorage.setItem(STORAGE_KEY, JSON.stringify(trimmed))
  } catch {
    // Private browsing / storage quota — the chat still works for this tab,
    // it just won't survive a reload. Once per session is enough to tell the
    // user without spamming a toast on every message while it stays over quota.
    if (!warnedAboutSaveFailure) {
      warnedAboutSaveFailure = true
      toast.error("Chat history is too large to save — older messages may not persist after you reload.")
    }
  }
}

/** Derives a short title from the first user message — same idea every
 * chat product uses so a conversation is recognizable in a sidebar list
 * without the user having to name it. */
function titleFrom(text: string): string {
  const trimmed = text.trim().replace(/\s+/g, " ")
  return trimmed.length > 48 ? trimmed.slice(0, 48) + "…" : trimmed || "New chat"
}

/**
 * Multi-conversation chat history for the AI Assistant, persisted to
 * localStorage — per-browser, not synced across devices or visible to
 * Claude/Ferrum's backend. This mirrors how every mainstream chat UI keeps
 * scratch conversation history: convenient, but not the system of record
 * (there's nothing here an admin or another device needs to see).
 */
export function useAIConversations(defaultModelId: string) {
  const [conversations, setConversations] = useState<Conversation[]>(load)
  const [activeId, setActiveId] = useState<string | null>(() => load()[0]?.id ?? null)

  useEffect(() => save(conversations), [conversations])

  const active = conversations.find((c) => c.id === activeId) ?? null

  const createConversation = useCallback(
    (modelId = defaultModelId) => {
      const conv: Conversation = { id: newId(), title: "New chat", modelId, messages: [], updatedAt: new Date().toISOString() }
      setConversations((prev) => [conv, ...prev])
      setActiveId(conv.id)
      return conv.id
    },
    [defaultModelId],
  )

  const deleteConversation = useCallback(
    (id: string) => {
      setConversations((prev) => prev.filter((c) => c.id !== id))
      setActiveId((cur) => (cur === id ? null : cur))
    },
    [],
  )

  /** Bulk counterpart to deleteConversation — for the sidebar's multi-select
   * mode, so clearing out a batch of old chats doesn't mean confirming and
   * clicking the trash icon once per conversation. */
  const deleteConversations = useCallback((ids: string[]) => {
    const doomed = new Set(ids)
    setConversations((prev) => prev.filter((c) => !doomed.has(c.id)))
    setActiveId((cur) => (cur && doomed.has(cur) ? null : cur))
  }, [])

  const updateConversation = useCallback((id: string, patch: Partial<Pick<Conversation, "title" | "modelId" | "messages">>) => {
    setConversations((prev) =>
      prev
        .map((c) => (c.id === id ? { ...c, ...patch, updatedAt: new Date().toISOString() } : c))
        // Most-recently-updated first, like every chat product's sidebar.
        .sort((a, b) => (a.id === id ? -1 : b.id === id ? 1 : 0)),
    )
  }, [])

  const setMessages = useCallback(
    (id: string, messages: DisplayMessage[]) => {
      const firstUser = messages.find((m) => m.role === "user")
      const conv = conversations.find((c) => c.id === id)
      const title = conv && conv.title !== "New chat" ? conv.title : firstUser ? titleFrom(firstUser.content) : "New chat"
      updateConversation(id, { messages, title })
    },
    [conversations, updateConversation],
  )

  return { conversations, active, activeId, setActiveId, createConversation, deleteConversation, deleteConversations, updateConversation, setMessages }
}
