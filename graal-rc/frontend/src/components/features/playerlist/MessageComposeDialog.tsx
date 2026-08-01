// MessageComposeDialog is the shared textarea modal behind the Mass PM and
// Admin Message toolbar buttons (mirrors getMessage / getAdminMessage in the
// reference C++ client's TPlayerList). `recipientLabel` describes the target
// ("all players", a single nick, …) and `sendLabel` the confirm button.
import {type FormEvent, useEffect, useRef, useState} from "react"

import {Modal} from "./Modal"
import {Button} from "@/components/ui/button"
import {useLanguage} from "@/hooks/useLanguage"

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
  const {t} = useLanguage()
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
      description={`Para: ${recipientLabel}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={sending}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" form="message-compose-form" disabled={sending || !text.trim()}>
            {sending ? "…" : sendLabel}
          </Button>
        </>
      }
    >
      <form id="message-compose-form" onSubmit={submit} className="flex flex-col gap-2">
        {singleLine ? (
          <input
            className="bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring h-10 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-2"
            placeholder={placeholder ?? t("rc.messagePlaceholder")}
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
        ) : (
          <textarea
            ref={ref}
            className="bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring min-h-[120px] w-full resize-none rounded-md border px-3 py-2 text-sm outline-none focus-visible:ring-2"
            placeholder={placeholder ?? t("rc.messagePlaceholder")}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) submit(e)
            }}
          />
        )}
        {!singleLine && (
          <p className="text-muted-foreground text-xs">Ctrl+Enter para enviar</p>
        )}
      </form>
    </Modal>
  )
}
