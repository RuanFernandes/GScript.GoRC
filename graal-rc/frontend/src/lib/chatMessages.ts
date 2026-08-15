import type {ChatMessage} from "@/types"

export type DisplayChatMessage = ChatMessage & {repeatCount?: number}

interface MessageGroup {
  message: ChatMessage
  count: number
  lastIndex: number
}

const REPEAT_LOOKBACK = 3

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
 * Collapses equal chat text only when the previous occurrence is in the last
 * three chat rows. Each stale occurrence starts a new group, so a message
 * from much farther back can never be merged into the current row.
 */
export function mergeRepeatedMessages(messages: ChatMessage[]): DisplayChatMessage[] {
  const activeGroups = new Map<string, MessageGroup>()
  const groups: MessageGroup[] = []

  messages.forEach((message, index) => {
    const key = messageKey(message)
    if (!key) {
      groups.push({message, count: 1, lastIndex: index})
      return
    }

    const activeGroup = activeGroups.get(key)
    if (activeGroup && index - activeGroup.lastIndex <= REPEAT_LOOKBACK) {
      activeGroup.message = message
      activeGroup.count += 1
      activeGroup.lastIndex = index
      return
    }

    const group = {message, count: 1, lastIndex: index}
    activeGroups.set(key, group)
    groups.push(group)
  })

  return groups
    .sort((left, right) => left.lastIndex - right.lastIndex)
    .map(({message, count}) => count > 1 ? {...message, repeatCount: count} : message)
}
