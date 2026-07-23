// MessageComposeDialog is the shared textarea modal behind the Mass PM and
// Admin Message toolbar buttons (mirrors getMessage / getAdminMessage in the
// reference C++ client's TPlayerList). `recipientLabel` describes the target
// ("all players", a single nick, …) and `sendLabel` the confirm button.
import {type FormEvent, useEffect, useRef, useState} from "react"

import {Modal} from "./Modal"
import {Button} from "@/components/ui/button"

interface MessageComposeDialogProps {
  open: boolean
  title: string
  recipientLabel: string
  placeholder?: string
  sendLabel?: string
  singleLine?: boolean
  onClose: () => void
  onSend: (message: string) => Promise<void>
}

export function MessageComposeDialog({
  open,
  title,
  recipientLabel,
  placeholder,
  sendLabel = "Send",
  singleLine = false,
  onClose,
  onSend,
}: MessageComposeDialogProps) {
  const [text, setText] = useState("")
  const [sending, setSending] = useState(false)
  const ref = useRef<HTMLTextAreaElement | null>(null)

  useEffect(() => {
    if (open) {
      setText("")
      setSending(false)
      // Focus after the modal mounts.
      const id = window.setTimeout(() => ref.current?.focus(), 0)
      return () => window.clearTimeout(id)
    }
  }, [open])

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const trimmed = text.trim()
    if (!trimmed || sending) return
    setSending(true)
    try {
      await onSend(trimmed)
      onClose()
    } finally {
      setSending(false)
    }
  }

  return (
    <Modal
      open={open}
      title={title}
      description={`To: ${recipientLabel}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={sending}>
            Cancel
          </Button>
          <Button type="submit" form="message-compose-form" disabled={sending || !text.trim()}>
            {sending ? "Sending…" : sendLabel}
          </Button>
        </>
      }
    >
      <form id="message-compose-form" onSubmit={submit} className="flex flex-col gap-2">
        {singleLine ? (
          <input
            className="bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring h-10 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-2"
            placeholder={placeholder ?? "Message"}
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
        ) : (
          <textarea
            ref={ref}
            className="bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring min-h-[120px] w-full resize-none rounded-md border px-3 py-2 text-sm outline-none focus-visible:ring-2"
            placeholder={placeholder ?? "Type your message…"}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) submit(e)
            }}
          />
        )}
        {!singleLine && (
          <p className="text-muted-foreground text-xs">Ctrl+Enter to send</p>
        )}
      </form>
    </Modal>
  )
}
