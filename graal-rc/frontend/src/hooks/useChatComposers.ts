// useChatComposers keeps what the user typed in each chat tab, and which tabs
// have a send in flight. It lives above the tabs, which unmount when inactive.
import {useCallback, useRef, useState} from "react"

import {composerAfterSend, createSendGuard, emptyComposer, type Composer} from "@/lib/sendGuard"

export interface ChatComposers {
  composer: (channel: string) => Composer
  update: (channel: string, patch: (current: Composer) => Partial<Composer>) => void
  isSending: (channel: string) => boolean
  // submit sends the channel's current composer. On success it calls onSent and
  // clears the composer unless it was edited in the meantime.
  submit: (channel: string, send: () => Promise<boolean>, onSent: () => void) => Promise<void>
}

export function useChatComposers(): ChatComposers {
  const [drafts, setDrafts] = useState<Record<string, Composer>>({})
  const [sending, setSending] = useState<ReadonlySet<string>>(() => new Set())
  const [guard] = useState(() => createSendGuard((channel, busy) => {
    setSending((current) => {
      const next = new Set(current)
      if (busy) next.add(channel)
      else next.delete(channel)
      return next
    })
  }))
  const draftsRef = useRef(drafts)
  draftsRef.current = drafts

  const composer = useCallback((channel: string) => drafts[channel] ?? emptyComposer(), [drafts])

  const update = useCallback((channel: string, patch: (current: Composer) => Partial<Composer>) => {
    setDrafts((all) => {
      const current = all[channel] ?? emptyComposer()
      return {...all, [channel]: {...current, ...patch(current)}}
    })
  }, [])

  const submit = useCallback(async (channel: string, send: () => Promise<boolean>, onSent: () => void) => {
    const submitted = draftsRef.current[channel] ?? emptyComposer()
    if (await guard.run(channel, send) !== "sent") return
    onSent()
    setDrafts((all) => composerAfterSend(all, channel, submitted))
  }, [guard])

  const isSending = useCallback((channel: string) => sending.has(channel), [sending])

  return {composer, update, isSending, submit}
}
