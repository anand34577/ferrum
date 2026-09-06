import { useCallback, useEffect, useState } from "react"
import type { AIChatMessage } from "@/lib/api"

export interface DisplayMessage extends AIChatMessage {
  id: string
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

function save(conversations: Conversation[]) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(conversations.slice(0, MAX_CONVERSATIONS)))
  } catch {
    // Private browsing / storage quota — the chat still works for this tab,
    // it just won't survive a reload. Not worth surfacing to the user.
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

  return { conversations, active, activeId, setActiveId, createConversation, deleteConversation, updateConversation, setMessages }
}
