// PmDialog is the per-player private-message conversation. It renders the
// thread (inbound from the player, outbound from us) plus a reply box that
// calls rc_send_private_message. Mirrors the reference client's PM window
// (received pane + reply pane + Send), but as a single in-memory thread.
import {type FormEvent, useEffect, useRef, useState} from "react"
import {Send} from "lucide-react"

import {Modal} from "./Modal"
import {Button} from "@/components/ui/button"

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

interface PmDialogProps {
  target: PmTarget | null
  lines: PmLine[]
  onClose: () => void
  onSend: (message: string) => Promise<void>
}

export function PmDialog({target, lines, onClose, onSend}: PmDialogProps) {
  const [text, setText] = useState("")
  const [sending, setSending] = useState(false)
  const viewportRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    setText("")
  }, [target?.id])

  // Snap to the newest line when the thread or target changes.
  useEffect(() => {
    const el = viewportRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [lines, target?.id])

  if (!target) return null
  const label = target.nick || target.account
  const desc = target.nick && target.nick !== target.account ? `${target.nick} (${target.account})` : target.account

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const trimmed = text.trim()
    if (!trimmed || sending) return
    setSending(true)
    try {
      await onSend(trimmed)
      setText("")
    } finally {
      setSending(false)
    }
  }

  return (
    <Modal
      open={!!target}
      title={`PM · ${label}`}
      description={desc}
      onClose={onClose}
      className="max-w-lg"
      footer={
        <form id="pm-reply-form" onSubmit={submit} className="flex w-full items-center gap-2">
          <input
            className="bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring h-9 flex-1 rounded-md border px-3 text-sm outline-none focus-visible:ring-2"
            placeholder="Reply…"
            value={text}
            onChange={(e) => setText(e.target.value)}
            autoFocus
          />
          <Button type="submit" size="icon" disabled={sending || !text.trim()} aria-label="Send">
            <Send className="size-4" />
          </Button>
        </form>
      }
    >
      <div
        ref={viewportRef}
        className="bg-muted/30 h-72 w-full overflow-y-auto rounded-md border"
      >
        {lines.length === 0 ? (
          <p className="text-muted-foreground p-4 text-center text-xs">No messages yet.</p>
        ) : (
          <div className="flex flex-col p-3">
            {lines.map((l, i) => {
              // Tight gap between consecutive same-side bubbles, larger break
              // when the speaker changes — mirrors how real chat threads read.
              const prev = lines[i - 1]
              const sameSide = !!prev && prev.dir === l.dir
              const out = l.dir === "out"
              return (
                <div
                  key={i}
                  className={`flex ${out ? "justify-end" : "justify-start"} ${i === 0 || sameSide ? "mt-0.5" : "mt-2.5"}`}
                >
                  <div
                    className={
                      out
                        ? "bg-primary text-primary-foreground max-w-[80%] rounded-2xl rounded-br-md px-3 py-1.5 text-sm"
                        : "bg-background max-w-[80%] rounded-2xl rounded-bl-md border px-3 py-1.5 text-sm"
                    }
                  >
                    {l.text}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </Modal>
  )
}
