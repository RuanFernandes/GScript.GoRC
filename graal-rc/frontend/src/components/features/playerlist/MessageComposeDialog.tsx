// MessageComposeDialog is the shared textarea modal behind the Mass PM and
// Admin Message toolbar buttons (mirrors getMessage / getAdminMessage in the
// reference C++ client's TPlayerList). `recipientLabel` describes the target
// ("all players", a single nick, …) and `sendLabel` the confirm button.
import {type FormEvent, useEffect, useRef, useState} from "react"

import {Modal} from "./Modal"
import {Button} from "@/components/ui/button"
import {useLanguage} from "@/hooks/useLanguage"
import {createSendGuard} from "@/lib/sendGuard"

interface MessageComposeDialogProps {
  open: boolean
  title: string
  recipientLabel: string
  placeholder?: string
  sendLabel?: string
  singleLine?: boolean
  onClose: () => void
  // Resolves true when the message was sent. The dialog stays open, with the
  // text, on false or rejection; the handler reports the reason.
  onSend: (message: string) => Promise<boolean>
}

export function MessageComposeDialog({
  open,
  title,
  recipientLabel,
  placeholder,
  sendLabel,
  singleLine = false,
  onClose,
  onSend,
}: MessageComposeDialogProps) {
  const {t} = useLanguage()
  const [text, setText] = useState("")
  const [sending, setSending] = useState(false)
  const ref = useRef<HTMLTextAreaElement | null>(null)
  const [guard] = useState(() => createSendGuard((_key, busy) => setSending(busy)))

  useEffect(() => {
    if (open) {
      setText("")
      // Focus after the modal mounts.
      const id = window.setTimeout(() => ref.current?.focus(), 0)
      return () => window.clearTimeout(id)
    }
  }, [open])

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const trimmed = text.trim()
    if (!trimmed) return
    if (await guard.run("compose", () => onSend(trimmed)) === "sent") onClose()
  }

  // Closing during a send would discard the text before the result is known.
  const requestClose = () => {
    if (!sending) onClose()
  }

  return (
    <Modal
      open={open}
      title={title}
      description={t("player.toRecipient", {recipient: recipientLabel})}
      onClose={requestClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={sending}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" form="message-compose-form" disabled={sending || !text.trim()}>
            {sending ? "…" : sendLabel ?? t("common.send")}
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
            readOnly={sending}
            onChange={(e) => setText(e.target.value)}
          />
        ) : (
          <textarea
            ref={ref}
            className="bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring min-h-[120px] w-full resize-none rounded-md border px-3 py-2 text-sm outline-none focus-visible:ring-2"
            placeholder={placeholder ?? t("rc.messagePlaceholder")}
            value={text}
            readOnly={sending}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) submit(e)
            }}
          />
        )}
        {!singleLine && (
          <p className="text-muted-foreground text-xs">{t("player.composeShortcut")}</p>
        )}
      </form>
    </Modal>
  )
}
