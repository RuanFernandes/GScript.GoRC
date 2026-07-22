// Domain types shared across the frontend. The Go-model types are re-exported
// from the generated v3 bindings so feature code depends on our own barrel.

export type {Server, Player} from "../../bindings/graal-rc/rclib/models"
export type {NCStatus} from "../../bindings/graal-rc/internal/connection/models"
export type {Status as SessionStatus} from "../../bindings/graal-rc/internal/connection/models"
export type {AccountSummary, LoginRequest} from "../../bindings/graal-rc/models"
export type {Weapon, Class, NPC, ScriptReply} from "../../bindings/graal-rc/rclib/models"
export type {CodingSettings, RemoteTheme} from "../../bindings/graal-rc/models"

// A single chat line. channel "" = server (RC) chat; otherwise the IRC channel.
// source drives the prefix tag and coloring: "rc" ([RC], on_message), "nc"
// ([NC], on_serverdata type=nc_message), "irc" ([IRC], on_irc_message), or
// "system" (other server data, gray).
export interface ChatMessage {
  id: number
  channel: string
  text: string
  source: "rc" | "nc" | "irc" | "system"
  ts: number
}

// A chat tab: the always-present server chat (channel "") plus one per IRC
// channel seen via rc_on_irc_message.
export interface ChatTab {
  channel: string
  label: string
  messages: ChatMessage[]
}

// User-configurable chat colors + optional logging (persisted in localStorage).
export interface ChatSettings {
  timestamp: string
  rcPrefix: string
  ncPrefix: string
  ircPrefix: string
  speaker: string
  content: string
  logChat: boolean
  logDir: string
}

// Finite set of top-level views the shell can render. Centralized so the
// router (App) is the only place that decides screen transitions.
export type AppView = "select" | "add" | "serverlist" | "rc"

// Script editor window kind parsed from the #editor?t=…&k=… URL. weapon/class/npc
// edit a script; npcflags edits flags; npcattr is read-only attributes.
export type EditorKind = "weapon" | "class" | "npc" | "npcflags" | "npcattr"
