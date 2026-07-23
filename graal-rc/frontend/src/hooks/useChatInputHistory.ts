import {useRef} from "react"

// useChatInputHistory powers arrow-up/down recall of previously sent lines in
// the chat input, like a typical terminal/IRC client: ArrowUp walks back toward
// the oldest entry, ArrowDown walks forward, and moving past the newest entry
// restores the in-progress draft the user had typed before recalling.
//
// History is global across tabs: there is one input surface and one sender, so a
// single shared ring is the natural model. Kept in refs — the input component
// owns the visible text state; this hook only mutates it via setText on key
// events, so no re-render of the history itself is needed.
const MAX_HISTORY = 100

export function useChatInputHistory() {
  // Oldest first, newest last. Sending a line appends here (deduped against the
  // immediately preceding entry so a repeat doesn't clutter recall).
  const history = useRef<string[]>([])
  // Index into history while navigating. null == not navigating (showing draft).
  const cursor = useRef<number | null>(null)
  // The text the user had typed before they pressed ArrowUp, restored on the way
  // back down past the newest entry.
  const draft = useRef("")

  const record = (text: string) => {
    const value = text.trim()
    if (!value) return
    const prev = history.current
    if (prev.length > 0 && prev[prev.length - 1] === value) return
    prev.push(value)
    if (prev.length > MAX_HISTORY) prev.splice(0, prev.length - MAX_HISTORY)
    cursor.current = null
  }

  // handleKeyDown returns true when it consumed the event (ArrowUp/ArrowDown
  // while navigating history) so the caller can preventDefault.
  const handleKeyDown = (
    e: React.KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>,
    text: string,
    setText: (value: string) => void
  ): boolean => {
    const items = history.current
    if (e.key === "ArrowUp") {
      if (items.length === 0) return false
      e.preventDefault()
      // First press of Up from the live input: snapshot the draft, point at the
      // newest entry.
      if (cursor.current === null) {
        draft.current = text
        cursor.current = items.length - 1
      } else if (cursor.current > 0) {
        cursor.current -= 1
      }
      setText(items[cursor.current])
      return true
    }
    if (e.key === "ArrowDown") {
      if (cursor.current === null) return false
      e.preventDefault()
      if (cursor.current < items.length - 1) {
        cursor.current += 1
        setText(items[cursor.current])
      } else {
        // Past the newest entry: back to the in-progress draft.
        cursor.current = null
        setText(draft.current)
      }
      return true
    }
    return false
  }

  // Any edit not driven by arrow navigation resets navigation, so typing or
  // pasting after a recall starts fresh from the new text.
  const onTextChange = (text: string) => {
    if (cursor.current !== null) cursor.current = null
    return text
  }

  return {record, handleKeyDown, onTextChange}
}
