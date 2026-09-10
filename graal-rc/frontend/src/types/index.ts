// Domain types shared across the frontend. The Go-model types are re-exported
// from the generated v3 bindings so feature code depends on our own barrel.

export type {Server, Player} from "../../bindings/graal-rc/rclib/models"
export type {NCStatus} from "../../bindings/graal-rc/internal/connection/models"
export type {Status as SessionStatus} from "../../bindings/graal-rc/internal/connection/models"
export type {RightsData, AttrsData, BanData, CommentsData, ScriptLists} from "../../bindings/graal-rc/internal/connection/models"
export type {AccountSummary, LoginRequest} from "../../bindings/graal-rc/models"
export type {Weapon, Class, NPC, ScriptReply} from "../../bindings/graal-rc/rclib/models"
export type {FileBrowserFolder, FileBrowserEntry} from "../../bindings/graal-rc/rclib/models"
export type {
  TableSchema as SqliteSchema,
  Column as SqliteColumn,
  ForeignKey as SqliteForeignKey,
  Changes as SqliteChanges,
  CellUpdate as SqliteCellUpdate,
} from "../../bindings/graal-rc/internal/sqlite/models"
export type {
  CodingSettings,
  ChatSettings,
  ChatSettingsState,
  AppTheme,
  AppThemeStore,
  RemoteTheme,
  RemoteChangelog,
  UpdateInfo,
  FileBrowserConfig,
  FileBrowserBackupResult,
  SqliteInfo,
  SqliteResult,
  SqliteTable,
  MCPAgentStatus,
  MCPSetupResult,
  ScriptGalleryProject,
  ScriptGalleryScript,
  ScriptGalleryIdentity,
  ScriptGalleryAuthState,
} from "../../bindings/graal-rc/models"
export type {
  SyncConfig,
  SyncStatus,
  SyncProgress,
  ReviewItem as SyncReviewItem,
  ScriptPair as SyncScriptPair,
  State as SyncState,
} from "../../bindings/graal-rc/internal/sync/models"
export type {Entry as AuditEntry} from "../../bindings/graal-rc/internal/audit/models"
export type {Backup as DeploymentBackup, BackupDiff as DeploymentBackupDiff} from "../../bindings/graal-rc/internal/deploy/models"
export type {ChangeRetentionSettings} from "../../bindings/graal-rc/models"
import type {GsFunction} from "@/lib/gscriptApi"
export type {GsFunction}

export interface PMLine { direction: "in" | "out"; text: string; timestamp: number }
export interface PMConversation { playerId: number; account: string; nick: string; unread: number; lines: PMLine[] }
export interface PMState { conversations: PMConversation[]; unreadTotal: number }
export interface CustomTheme { key: string; name: string; definition: string }

export type OperationalNotificationLevel = "info" | "success" | "warning" | "error"
export interface OperationalNotification {
  id: string
  level: OperationalNotificationLevel
  title: string
  message: string
  timestamp: number
  read: boolean
}

export type CommandMacroParameterType = "text" | "number" | "boolean"

export interface CommandMacroParameter {
  name: string
  type: CommandMacroParameterType
}

export interface CommandMacro {
  id: string
  name: string
  command: string
  parameters?: CommandMacroParameter[]
  createdAt: number
  updatedAt: number
}

export interface CommandMacroStore {
  macros: CommandMacro[]
  exists: boolean
}

// A single chat line. channel "" = server (RC) chat; otherwise the IRC channel.
// source drives the prefix tag and coloring: "rc" ([RC], on_message), "nc"
// ([NC], on_serverdata type=nc_message), "irc" ([IRC], on_irc_message), or
// "system" (other server data, gray). scriptHelp, when set, renders the line as
// a /scripthelp2 result list (hoverable function reference).
export interface ChatMessage {
  id: number
  channel: string
  text: string
  source: "rc" | "nc" | "irc" | "system"
  ts: number
  /** Account/community alias recognized as a ping for the current session. */
  mentionTarget?: string
  scriptHelp?: GsFunction[]
}

// A chat tab: the always-present server chat (channel "") plus one per IRC
// channel seen via rc_on_irc_message.
export interface ChatTab {
  channel: string
  label: string
  messages: ChatMessage[]
}

// Finite set of top-level views the shell can render. Centralized so the
// router (App) is the only place that decides screen transitions.
export type AppView = "select" | "add" | "serverlist" | "rc"

// Script editor window kind parsed from the #editor?t=…&k=… URL. weapon/class/npc
// edit a script; npcflags edits flags; npcattr is read-only attributes.
// options/folder_config/flags are server-side text configs (main socket, not NC).
export type EditorKind =
  | "weapon"
  | "class"
  | "npc"
  | "npcflags"
  | "npcattr"
  | "options"
  | "folder_config"
  | "flags"

// Server-side text config kinds (subset of EditorKind) editable via SaveServerText.
export type ServerTextKind = "options" | "folder_config" | "flags"
