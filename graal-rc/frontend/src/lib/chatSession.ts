// Chat state of one server session: the tabs and their lines, plus the history
// set aside during an automatic reconnect, and the chat log writer.
import type {ChatMessage, ChatTab} from "../types"
import {formatLogLine} from "./chatLine.ts"

export const SERVER_CHANNEL = ""
const MAX_LINES_PER_TAB = 1000

export interface ChatSession {
  tabs: ChatTab[]
  // History of channels that a reconnect removed and has not rejoined yet.
  retained: Readonly<Record<string, ChatMessage[]>>
}

export function initialChatSession(serverLabel: string): ChatSession {
  return {tabs: [{channel: SERVER_CHANNEL, label: serverLabel, messages: []}], retained: {}}
}

function capped(messages: ChatMessage[]): ChatMessage[] {
  return messages.length > MAX_LINES_PER_TAB ? messages.slice(messages.length - MAX_LINES_PER_TAB) : messages
}

// openTab creates a channel tab, giving it back any retained history.
function openTab(state: ChatSession, channel: string): ChatSession {
  const {[channel]: history, ...retained} = state.retained
  return {tabs: [...state.tabs, {channel, label: channel, messages: history ?? []}], retained}
}

export function appendChatMessage(state: ChatSession, message: ChatMessage): ChatSession {
  const channel = message.channel
  const base = state.tabs.some((tab) => tab.channel === channel) ? state : openTab(state, channel)
  return {
    ...base,
    tabs: base.tabs.map((tab) => tab.channel === channel ? {...tab, messages: capped([...tab.messages, message])} : tab),
  }
}

// reconcileChannelTabs mirrors the tabs against the joined-channel snapshot.
export function reconcileChannelTabs(state: ChatSession, channels: string[]): ChatSession {
  const wanted = new Set(channels)
  let next: ChatSession = {
    ...state,
    tabs: state.tabs.filter((tab) => tab.channel === SERVER_CHANNEL || wanted.has(tab.channel)),
  }
  for (const channel of channels) {
    if (!next.tabs.some((tab) => tab.channel === channel)) next = openTab(next, channel)
  }
  return next
}

// retainChannelHistory is applied on a disconnect that will be retried. The
// backend then empties the joined-channel list, which must not erase history.
export function retainChannelHistory(state: ChatSession): ChatSession {
  const retained = {...state.retained}
  for (const tab of state.tabs) {
    if (tab.channel !== SERVER_CHANNEL && tab.messages.length > 0) retained[tab.channel] = tab.messages
  }
  return {...state, retained}
}

export function clearChannelMessages(state: ChatSession, channel: string): ChatSession {
  return {...state, tabs: state.tabs.map((tab) => tab.channel === channel ? {...tab, messages: []} : tab)}
}

export function moveChatTab(state: ChatSession, from: number, to: number): ChatSession {
  if (from === to || from < 0 || from >= state.tabs.length || to < 0 || to >= state.tabs.length) return state
  const tabs = [...state.tabs]
  const [moved] = tabs.splice(from, 1)
  tabs.splice(to, 0, moved)
  return {...state, tabs}
}

// createChatLineLogger returns the function to call once for every line as it
// arrives. Writes run one at a time so the file keeps the arrival order.
export function createChatLineLogger(isEnabled: () => boolean, append: (line: string) => PromiseLike<unknown>) {
  let tail: Promise<unknown> = Promise.resolve()
  return (message: ChatMessage): void => {
    if (!isEnabled()) return
    const line = formatLogLine(message)
    tail = tail.then(() => append(line)).catch(() => {})
  }
}
