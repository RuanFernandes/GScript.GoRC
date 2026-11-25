// Shared rules for message inputs: one send at a time per input, and the typed
// text is only cleared after a successful send that it was not edited during.
import type {CommandMacro} from "../types"

export type SendOutcome = "sent" | "failed" | "busy"

export function createSendGuard(onBusyChange?: (key: string, busy: boolean) => void) {
  const inFlight = new Set<string>()
  return {
    // run calls send unless one is already running for key. send resolves true
    // on success; resolving false, rejecting or throwing is a failure.
    async run(key: string, send: () => Promise<boolean>): Promise<SendOutcome> {
      if (inFlight.has(key)) return "busy"
      inFlight.add(key)
      onBusyChange?.(key, true)
      try {
        return await send() ? "sent" : "failed"
      } catch {
        return "failed"
      } finally {
        inFlight.delete(key)
        onBusyChange?.(key, false)
      }
    },
  }
}

export function draftAfterSend(current: string, submitted: string): string {
  return current === submitted ? "" : current
}

// Composer is what the user has typed in one chat tab.
export interface Composer {
  text: string
  selectedMacro: CommandMacro | null
  macroValues: Record<string, string>
}

const EMPTY_COMPOSER: Composer = {text: "", selectedMacro: null, macroValues: {}}

export function emptyComposer(): Composer {
  return EMPTY_COMPOSER
}

export function composerAfterSend(
  drafts: Record<string, Composer>,
  key: string,
  submitted: Composer,
): Record<string, Composer> {
  const current = drafts[key] ?? EMPTY_COMPOSER
  const untouched = current.text === submitted.text
    && current.selectedMacro === submitted.selectedMacro
    && current.macroValues === submitted.macroValues
  return untouched ? {...drafts, [key]: emptyComposer()} : drafts
}
