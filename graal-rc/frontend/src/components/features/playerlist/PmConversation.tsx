import {type FormEvent, useEffect, useRef, useState} from "react"
import {Check, Copy, Send} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {useLanguage} from "@/hooks/useLanguage"
import {PlayerMentionText} from "@/components/features/chat/PlayerMention"
import type {PlayerMentionMatcher} from "@/lib/playerMentions"
import type {Player} from "@/types"
import {
  containsUnsafePrivateMessageMarkup,
  copyTextToClipboard,
  normalizePrivateMessageText,
} from "@/lib/privateMessage"

export interface PmLine {
  dir: "in" | "out"
  text: string
  ts: number
}

export interface PmTarget {
  id: number
  account: string
  nick: string
}

interface PmConversationProps {
  target: PmTarget
  lines: PmLine[]
  onSend: (message: string) => Promise<void>
  playerMatcher?: PlayerMentionMatcher
  onPlayerContext?: (player: Player, x: number, y: number) => void
}

function formatMessageTimestamp(timestamp: number) {
  if (!Number.isFinite(timestamp)) return null
  const date = new Date(timestamp)
  if (Number.isNaN(date.getTime())) return null
  return {
    label: date.toLocaleTimeString(undefined, {hour: "2-digit", minute: "2-digit"}),
    title: date.toLocaleString(),
    iso: date.toISOString(),
  }
}

export function PmConversation({target, lines, onSend, playerMatcher, onPlayerContext}: PmConversationProps) {
  const {t} = useLanguage()
  const [text, setText] = useState("")
  const [sending, setSending] = useState(false)
  const [copiedKey, setCopiedKey] = useState<string | null>(null)
  const viewportRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    setText("")
  }, [target.id])

  useEffect(() => {
    const element = viewportRef.current
    if (element) element.scrollTop = element.scrollHeight
  }, [lines, target.id])

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    const trimmed = text.trim()
    if (!trimmed || sending) return
    if (containsUnsafePrivateMessageMarkup(trimmed)) {
      toast.error(t("player.pmUnsafeContent"))
      return
    }
    setSending(true)
    try {
      await onSend(trimmed)
      setText("")
    } finally {
      setSending(false)
    }
  }

  const copyMessage = async (key: string, message: string) => {
    try {
      await copyTextToClipboard(message)
      setCopiedKey(key)
      toast.success(t("player.pmCopied"))
    } catch {
      toast.error(t("player.pmCopyFailed"))
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div ref={viewportRef} className="bg-muted/20 min-h-0 flex-1 overflow-y-auto px-4 py-3">
        {lines.length === 0 ? (
          <p className="text-muted-foreground flex h-full items-center justify-center text-center text-xs">
            {t("common.noMessages")}
          </p>
        ) : (
          <div className="flex flex-col">
            {lines.map((line, index) => {
              const previous = lines[index - 1]
              const sameSide = !!previous && previous.dir === line.dir
              const outgoing = line.dir === "out"
              const message = normalizePrivateMessageText(line.text)
              const messageKey = `${line.ts}-${index}`
              const timestamp = formatMessageTimestamp(line.ts)
              return (
                <div
                  key={messageKey}
                  className={`flex ${outgoing ? "justify-end" : "justify-start"} ${index === 0 || sameSide ? "mt-1" : "mt-3"}`}
                >
                  <div
                    className={outgoing
                      ? "bg-primary text-primary-foreground max-w-[84%] rounded-2xl rounded-br-md px-3 py-2 text-sm break-words"
                      : "bg-background max-w-[84%] rounded-2xl rounded-bl-md border px-3 py-2 text-sm break-words"}
                  >
                    <div className="whitespace-pre-wrap"><PlayerMentionText text={message} matcher={playerMatcher} translate={t} onOpenContext={onPlayerContext} /></div>
                    <div className={`mt-1 flex items-center justify-end gap-1 text-[10px] leading-none ${outgoing ? "text-primary-foreground/70" : "text-muted-foreground"}`}>
                      {timestamp && (
                        <time dateTime={timestamp.iso} title={timestamp.title} aria-label={t("player.pmTimestamp", {time: timestamp.label})}>
                          {timestamp.label}
                        </time>
                      )}
                      <button
                        type="button"
                        className="rounded-sm p-0.5 outline-none transition-colors hover:text-current focus-visible:ring-2 focus-visible:ring-current"
                        onClick={() => void copyMessage(messageKey, message)}
                        disabled={!message}
                        aria-label={t("player.pmCopyMessage")}
                        title={t("player.pmCopyMessage")}
                      >
                        {copiedKey === messageKey ? <Check className="size-3" /> : <Copy className="size-3" />}
                      </button>
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>
      <form onSubmit={submit} className="border-t p-3">
        <div className="flex items-center gap-2">
          <input
            className="bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring h-9 min-w-0 flex-1 rounded-md border px-3 text-sm outline-none focus-visible:ring-2"
            placeholder={t("player.replyPlaceholder")}
            value={text}
            onChange={(event) => setText(event.target.value)}
            autoFocus
            aria-label={t("player.replyPlaceholder")}
          />
          <Button type="submit" size="icon" disabled={sending || !text.trim()} aria-label={t("common.send")}>
            <Send className="size-4" />
          </Button>
        </div>
      </form>
    </div>
  )
}
