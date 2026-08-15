import type {ChatMessage} from "@/types"

export type DisplayChatMessage = ChatMessage & {repeatCount?: number}

interface MessageGroup {
  message: ChatMessage
  count: number
  lastIndex: number
}

function messageKey(message: ChatMessage): string | null {
  // Script-help rows are interactive results, not plain chat text. Keeping
  // them separate avoids hiding a different result behind the same query.
  if (message.scriptHelp) return null

  // Empty server lines are intentional spacing in the chat feed. They must
  // remain individual rows instead of becoming a misleading repeat counter.
  const normalizedText = message.text.trim()
  if (!normalizedText) return null

  // The source and channel are part of the rendered line. Two equal strings
  // from RC and NC must remain distinguishable to the operator.
  return JSON.stringify([message.channel, message.source, normalizedText])
}

/**
 * Collapses equal chat text within one tab and places each collapsed row at
 * the last occurrence. This keeps the newest context visible while counting
 * duplicates that were separated by other lines, like a console counter.
 */
export function mergeRepeatedMessages(messages: ChatMessage[]): DisplayChatMessage[] {
  const groups = new Map<string, MessageGroup>()
  const unique: MessageGroup[] = []

  messages.forEach((message, index) => {
    const key = messageKey(message)
    if (!key) {
      unique.push({message, count: 1, lastIndex: index})
      return
    }

    const existing = groups.get(key)
    if (existing) {
      existing.message = message
      existing.count += 1
      existing.lastIndex = index
      return
    }

    const group = {message, count: 1, lastIndex: index}
    groups.set(key, group)
    unique.push(group)
  })

  return unique
    .sort((left, right) => left.lastIndex - right.lastIndex)
    .map(({message, count}) => count > 1 ? {...message, repeatCount: count} : message)
}
