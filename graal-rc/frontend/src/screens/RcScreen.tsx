// RcScreen is the main Remote Control surface shown after connecting to a
// server: a tabbed chat (always-on "RC Chat" + dynamic IRC channel tabs), a
// command input, an NC (script socket) status badge, a toggleable player list
// panel, and a chat-color settings dialog. Mirrors the reference client's
// TRemoteFrame.
import {useEffect, useRef, useState} from "react"
import {Events} from "@wailsio/runtime"
import {Activity, Bell, BellRing, BookmarkPlus, Command, LogOut, RotateCcw, Search, ScrollText, Send, Settings, Trash2, UserRound, WifiOff, X} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {ChatLine} from "@/components/features/chat/ChatLine"
import {ScriptHelpResult} from "@/components/features/chat/ScriptHelpResult"
import {RcSidebar} from "@/components/features/rc/RcSidebar"
import {ChangelogPopover} from "@/components/features/rc/ChangelogPopover"
import {GlobalSearchPalette} from "@/components/features/rc/GlobalSearchPalette"
import {NotificationCenterPopover} from "@/components/features/rc/NotificationCenterPopover"
import {useChat} from "@/hooks/useChat"
import {useChatAutocomplete} from "@/hooks/useChatAutocomplete"
import {useChatInputHistory} from "@/hooks/useChatInputHistory"
import {useChatSettings} from "@/hooks/useChatSettings"
import {usePlayers} from "@/hooks/usePlayers"
import {serverDisplay} from "@/lib/server"
import {formatLogLine} from "@/lib/chatLine"
import {rcService} from "@/services/rcService"
import type {AccountSummary, ChatMessage, ChatSettings, NCStatus, Player, ReconnectStatus} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"
import {usePrivateMessages} from "@/hooks/usePrivateMessages"
import {useCommandMacros} from "@/hooks/useCommandMacros"
import {useOperationalNotifications} from "@/hooks/useOperationalNotifications"

interface RcScreenProps {
  serverName: string
  accountName: string
  onDisconnect: () => void
}

interface RightsIdentityStatus {
  realAccount?: string
  communityName?: string
}

function ncLabel(s: NCStatus, playerCount: number, t: (key: string, vars?: Record<string, string | number>) => string): string {
  if (!s.hasNc) return t("rc.noNcServer")
  if (s.authenticated) {
    return t("rc.connected", {count: playerCount, suffix: playerCount === 1 ? "" : "s"})
  }
  if (s.connected) return t("rc.connecting")
  return t("rc.ncOff")
}

// ChatPane is one tab's message log + input. Uses a plain overflow-auto div
// (not the radix ScrollArea) so scrolling stays internal to the chat and we can
// drive stick-to-bottom ourselves: it only follows new messages while the user
// is already at the bottom; scrolling up even 1px pins the view so history can
// be read without being yanked back down. New messages are also appended to the
// .txt log when logging is enabled.
function ChatPane({
  messages,
  settings,
  onSend,
  history,
  players,
  serverName,
  commandMacros,
}: {
  messages: ChatMessage[]
  settings: ChatSettings
  onSend: (text: string) => Promise<boolean>
  history: ReturnType<typeof useChatInputHistory>
  players: Player[]
  serverName: string
  commandMacros: ReturnType<typeof useCommandMacros>
}) {
  const {t} = useLanguage()
  const [text, setText] = useState("")
  const [ghostOff, setGhostOff] = useState(false)
  const autocomplete = useChatAutocomplete(players)
  const [macrosOpen, setMacrosOpen] = useState(false)
  const [macroName, setMacroName] = useState("")
  const scrollRef = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const lastLogged = useRef(0)

  useEffect(() => {
    const el = scrollRef.current
    if (el && stick.current) el.scrollTop = el.scrollHeight
  }, [messages])

  useEffect(() => {
    const latest = messages.length ? messages[messages.length - 1].id : 0
    if (!settings.logChat || !settings.logDir) {
      lastLogged.current = latest
      return
    }
    for (const m of messages) {
      if (m.id <= lastLogged.current) continue
      rcService.appendChatLog(formatLogLine(m)).catch(() => {})
    }
    lastLogged.current = latest
  }, [messages, settings.logChat, settings.logDir])

  const onScroll = () => {
    const el = scrollRef.current
    if (!el) return
    stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (await onSend(text)) {
      history.record(text)
      setText("")
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <div ref={scrollRef} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto rounded-md border p-3">
        <div className="grid gap-1 font-mono text-sm">
          {messages.length === 0 ? (
            <p className="text-muted-foreground">{t("rc.noMessages")}</p>
          ) : (
            messages.map((m) => (
              <div key={m.id}>
                {m.scriptHelp ? (
                  <ScriptHelpResult query={m.text} entries={m.scriptHelp} />
                ) : (
                  <ChatLine message={m} settings={settings} />
                )}
              </div>
            ))
          )}
        </div>
      </div>
      <div className="flex items-center justify-end">
        <div className="relative">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 gap-1.5 px-2 text-xs"
            onClick={() => setMacrosOpen((value) => !value)}
            aria-expanded={macrosOpen}
            title={t("macros.title")}
          >
            <Command className="size-3.5" />{t("macros.title")}
          </Button>
          {macrosOpen && (
            <div className="bg-popover text-popover-foreground absolute right-0 bottom-full z-40 mb-2 w-[min(23rem,calc(100vw-2rem))] overflow-hidden rounded-lg border shadow-xl">
              <div className="flex items-center gap-2 border-b px-3 py-2">
                <Command className="text-primary size-4" />
                <div className="min-w-0 flex-1">
                  <p className="text-xs font-semibold">{t("macros.title")}</p>
                  <p className="text-muted-foreground text-[10px]">{t("macros.subtitle", {server: serverName})}</p>
                </div>
              </div>
              <div className="flex gap-1.5 border-b p-2">
                <Input
                  value={macroName}
                  onChange={(event) => setMacroName(event.target.value)}
                  placeholder={t("macros.namePlaceholder")}
                  className="h-8 text-xs"
                  maxLength={80}
                />
                <Button
                  type="button"
                  size="icon"
                  className="size-8 shrink-0"
                  disabled={!macroName.trim() || !text.trim()}
                  onClick={() => {
                    if (!commandMacros.save(macroName, text)) return
                    setMacroName("")
                  }}
                  title={t("macros.save")}
                  aria-label={t("macros.save")}
                >
                  <BookmarkPlus className="size-4" />
                </Button>
              </div>
              <div className="max-h-56 overflow-y-auto p-1">
                {commandMacros.macros.length === 0 ? (
                  <p className="text-muted-foreground px-2 py-4 text-center text-xs">{t("macros.empty")}</p>
                ) : commandMacros.macros.map((macro) => (
                  <div key={macro.id} className="group flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-accent">
                    <button
                      type="button"
                      className="min-w-0 flex-1 text-left"
                      onClick={() => {
                        setGhostOff(false)
                        setText(macro.command)
                        setMacrosOpen(false)
                      }}
                      title={macro.command}
                    >
                      <span className="block truncate text-xs font-medium">{macro.name}</span>
                      <span className="text-muted-foreground block truncate font-mono text-[10px]">{macro.command}</span>
                    </button>
                    <button
                      type="button"
                      className="text-muted-foreground hover:text-destructive shrink-0 rounded p-1 opacity-0 transition-opacity group-hover:opacity-100 focus:opacity-100"
                      onClick={() => commandMacros.remove(macro.id)}
                      title={t("macros.delete")}
                      aria-label={t("macros.delete")}
                    >
                      <Trash2 className="size-3.5" />
                    </button>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
      <form onSubmit={submit} className="flex gap-2">
        {/* Ghost-text completion: the suggestion renders behind the input as a
            transparent copy of the typed text (reserving its exact width) plus a
            muted suffix. The input paints its opaque text on top, so only the
            suffix reads as a grey hint — Tab accepts it. No pixel math: the
            transparent run is the same glyphs at the same font, so the suffix
            always starts where the caret is. */}
        <div className="relative flex-1">
          {(() => {
            const options = autocomplete.options(text)
            if (!options.length) return null
            const accountPhase = /^\/\S+\s/.test(text)
            return (
              <div
                role="listbox"
                aria-label={accountPhase ? t("rc.playerSuggestions") : t("rc.commandSuggestions")}
                className="bg-popover text-popover-foreground absolute right-0 bottom-full left-0 z-30 mb-2 max-h-64 overflow-y-auto rounded-md border p-1 shadow-lg"
              >
                <div className="text-muted-foreground flex items-center justify-between px-2 py-1 text-[11px]">
                  <span>{accountPhase ? t("rc.players") : t("rc.commands")}</span>
                  <span>{t("rc.tabToComplete")}</span>
                </div>
                {options.map((option) => (
                  <button
                    key={option}
                    type="button"
                    role="option"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => {
                      setGhostOff(false)
                      autocomplete.select(text, option, setText)
                    }}
                    className="hover:bg-accent hover:text-accent-foreground flex w-full items-center justify-between rounded-sm px-2 py-1.5 text-left font-mono text-xs transition-colors"
                  >
                    <span>{accountPhase ? option : `/${option}`}</span>
                    {!accountPhase && (
                      <span className="text-muted-foreground ml-3 font-sans text-[10px]">
                        {option === "open" || option === "openrights" || option === "opencomments" || option === "openaccess" || option === "openacc" || option === "openprofile" || option === "playerinfo" || option === "disconnect" || option === "reset" || option === "staffactivity" ? t("rc.playerType") : t("rc.commandType")}
                      </span>
                    )}
                  </button>
                ))}
              </div>
            )
          })()}
          {(() => {
            const ghost = !ghostOff ? autocomplete.suggest(text) : null
            if (!ghost) return null
            // Split at the first case-sensitive divergence between typed text
            // and the suggestion. The identical run renders transparent (reserves
            // its exact width); the rest renders muted. So a lowercase "r" typing
            // toward "Ruan" greys from the "R" onward, and Tab swaps in the
            // canonical casing.
            let div = 0
            const n = Math.min(text.length, ghost.length)
            while (div < n && text[div] === ghost[div]) div++
            return (
              <span
                aria-hidden
                className="pointer-events-none absolute inset-0 flex items-center overflow-hidden px-3 text-base whitespace-pre md:text-sm"
              >
                <span className="text-transparent">{text.slice(0, div)}</span>
                <span className="text-muted-foreground/60">{ghost.slice(div)}</span>
              </span>
            )
          })()}
          <Input
            className="relative z-10"
            value={text}
            onChange={(e) => {
              setGhostOff(false)
              setText(history.onTextChange(e.target.value))
            }}
            onKeyDown={(e) => {
              // Escape hides the ghost preview until the next edit.
              if (e.key === "Escape") {
                if (!ghostOff && autocomplete.suggest(text)) {
                  e.preventDefault()
                  setGhostOff(true)
                }
                return
              }
              // Tab completes the current token (Shift+Tab cycles back). Captured
              // only in a "/" command context so plain Tab still works elsewhere.
              if (e.key === "Tab" && text.startsWith("/")) {
                e.preventDefault()
                setGhostOff(false)
                if (autocomplete.complete(text, setText, e.shiftKey)) return
              }
              history.handleKeyDown(e, text, setText)
            }}
            placeholder={t("rc.messagePlaceholder")}
            autoComplete="off"
          />
        </div>
        <Button type="submit" size="icon" aria-label={t("common.send")} title={t("common.send")}><Send className="size-4" /></Button>
      </form>
    </div>
  )
}

export function RcScreen({serverName, accountName, onDisconnect}: RcScreenProps) {
  const {t} = useLanguage()
  const {tabs, activeChannel, setActiveChannel, send, reorderTabs} = useChat(rcService)
  const inputHistory = useChatInputHistory()
  const {settings} = useChatSettings()
  const [nc, setNc] = useState<NCStatus>({hasNc: false, connected: false, authenticated: false})
  const [profile, setProfile] = useState<AccountSummary | null>(null)
  const [rightsIdentity, setRightsIdentity] = useState<RightsIdentityStatus>({})
  const {players} = usePlayers(rcService, true)
  const {state: pmState} = usePrivateMessages()
  const dragIndex = useRef<number>(-1)
  const [changelogOpen, setChangelogOpen] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const [reconnect, setReconnect] = useState<ReconnectStatus | null>(null)
  const [notificationsOpen, setNotificationsOpen] = useState(false)
  const notificationCenter = useOperationalNotifications()

  useEffect(() => {
    let cancelled = false
    const tick = async () => {
      try {
        const s = await rcService.ncStatus()
        if (!cancelled && s) setNc(s)
      } catch {
        // ignore transient status failures
      }
    }
    tick()
    const handle = window.setInterval(tick, 1000)
    return () => {
      cancelled = true
      window.clearInterval(handle)
    }
  }, [])

  // Recovery state is pushed by the backend so the operator can keep the RC
  // context and decide whether to retry immediately or leave the session.
  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const state = await rcService.getReconnectStatus()
        if (!cancelled && state.active) setReconnect(state)
      } catch {
        // The status banner is optional; the normal connection surface remains usable.
      }
    }
    const read = (event: {data: string}) => {
      try {
        const state = JSON.parse(event.data) as ReconnectStatus
        if (!cancelled) setReconnect(state)
      } catch {
        // Ignore malformed status payloads.
      }
    }
    const offProgress = Events.On("rc:reconnect", read)
    const offFailed = Events.On("rc:reconnectFailed", read)
    const offReconnected = Events.On("rc:reconnected", () => {
      if (!cancelled) setReconnect(null)
    })
    void load()
    return () => {
      cancelled = true
      offProgress()
      offFailed()
      offReconnected()
    }
  }, [])

  // Fetch the active account's client-only profile (display name + photo) for
  // the top header.
  useEffect(() => {
    let cancelled = false
    if (!accountName) return
    rcService
      .getAccount(accountName)
      .then((p) => {
        if (!cancelled && p) setProfile(p)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [accountName])

  // The server's rights response is authoritative for the account label. It
  // may arrive after this screen mounts, so refresh it on the identity/cache
  // events as well as once during initial render.
  useEffect(() => {
    let cancelled = false
    setRightsIdentity({})

    const refreshIdentity = async () => {
      try {
        const value = await rcService.status()
        if (cancelled || !value || typeof value !== "object") return
        const status = value as RightsIdentityStatus
        setRightsIdentity({
          realAccount: typeof status.realAccount === "string" ? status.realAccount.trim() : "",
          communityName: typeof status.communityName === "string" ? status.communityName.trim() : "",
        })
      } catch {
        // The profile/account fallback remains usable while the session starts.
      }
    }

    void refreshIdentity()
    const off = Events.On("rc:evt", (event: {data: string}) => {
      try {
        const message = JSON.parse(event.data) as {name?: string}
        if (message.name === "rc:scriptIdentityChanged" || message.name === "rc:scriptPermissionsChanged") {
          void refreshIdentity()
        }
      } catch {
        // Ignore unrelated or malformed event payloads.
      }
    })
    return () => {
      cancelled = true
      off()
    }
  }, [accountName])

  const {label: displayServer} = serverDisplay(serverName)
  const commandMacros = useCommandMacros(displayServer || serverName)
  const realAccount = rightsIdentity.realAccount || ""
  const communityName = rightsIdentity.communityName || ""
  const rightsLabel = realAccount ? (communityName ? `${communityName} (${realAccount})` : realAccount) : ""
  const apelido = rightsLabel || profile?.displayName || accountName
  const latestUnread = pmState.conversations.find((conversation) => conversation.unread > 0)
  const previousUnread = useRef(0)
  const pmSound = useRef<HTMLAudioElement | null>(null)

  useEffect(() => {
    if (pmState.unreadTotal > previousUnread.current) {
      try {
        const sound = pmSound.current ?? new Audio("/sounds/itemget.wav")
        pmSound.current = sound
        sound.volume = 0.75
        sound.currentTime = 0
        sound.play().catch(() => {})
      } catch {
        // Audio can be unavailable when the app is running without an audio device.
      }
    }
    previousUnread.current = pmState.unreadTotal
  }, [pmState.unreadTotal])

  // Open a server-side text config editor (options/folder_config/flags). These
  // travel on the main socket, not NC, so they're available without script
  // rights; a no-rights/timeout fetch rejects and toasts instead of opening a
  // blank window.
  const openServerText = (kind: "options" | "folder_config" | "flags", label: string) => {
    rcService.openScriptEditor(kind, label).catch((err: unknown) => {
      toast.error(`${t("common.openFailed")}: ${label}`, {description: String(err)})
    })
  }

  // Push the log config to the backend whenever it changes so AppendChatLog /
  // AppendPmLog know whether (and where) to write.
  useEffect(() => {
    rcService.setChatLogConfig(settings.logChat, settings.logDir).catch(() => {})
  }, [settings.logChat, settings.logDir])
  useEffect(() => {
    rcService.setPmLogConfig(settings.pmLog, settings.pmLogDir).catch(() => {})
  }, [settings.pmLog, settings.pmLogDir])

  return (
    <div className="bg-background flex h-svh flex-col overflow-hidden">
      {/* Top header: account profile (client-only) + server/online count + global
          actions (Settings, Disconnect). Sits above the RC action header. */}
      <header className="flex items-center gap-3 border-b px-4 py-2">
        {profile?.photo ? (
          <img
            src={profile.photo}
            alt={apelido}
            className="size-8 rounded-full border object-cover"
          />
        ) : (
          <div className="bg-muted flex size-8 items-center justify-center rounded-full border">
            <UserRound className="text-muted-foreground size-4" />
          </div>
        )}
        <div className="flex flex-col leading-tight">
          <span className="text-sm font-semibold">{apelido}</span>
          <span className="text-muted-foreground text-xs">
            <span className="text-muted-foreground/70">{displayServer ? `${displayServer} · ` : ""}</span>
            {t("rc.playersOnline", {count: players.length, suffix: players.length === 1 ? "" : "s"})}
          </span>
        </div>
        <div className="relative ml-auto flex items-center gap-2">
          <Button
            variant="ghost"
            size="icon"
            className="relative"
            title={t("rc.privateMessages")}
            aria-label={t("rc.privateMessages")}
            onClick={() => latestUnread && rcService.openPlayerListPM(latestUnread.playerId)}
          >
            <Bell className="size-4" />
            {pmState.unreadTotal > 0 && (
              <span className="bg-destructive text-destructive-foreground absolute -top-0.5 -right-0.5 min-w-4 rounded-full px-1 text-[10px] leading-4">
                {pmState.unreadTotal > 99 ? "99+" : pmState.unreadTotal}
              </span>
            )}
          </Button>
          <div className="relative">
            <Button
              variant="ghost"
              size="icon"
              className="relative"
              title={t("notifications.title")}
              aria-label={t("notifications.title")}
              aria-expanded={notificationsOpen}
              onClick={() => setNotificationsOpen((value) => !value)}
            >
              <BellRing className="size-4" />
              {notificationCenter.unreadCount > 0 && (
                <span className="bg-primary text-primary-foreground absolute -top-0.5 -right-0.5 min-w-4 rounded-full px-1 text-[10px] leading-4">
                  {notificationCenter.unreadCount > 99 ? "99+" : notificationCenter.unreadCount}
                </span>
              )}
            </Button>
            <NotificationCenterPopover
              open={notificationsOpen}
              notifications={notificationCenter.notifications}
              unreadCount={notificationCenter.unreadCount}
              onClose={() => setNotificationsOpen(false)}
              onMarkRead={notificationCenter.markRead}
              onMarkAllRead={notificationCenter.markAllRead}
              onClear={notificationCenter.clear}
            />
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="relative"
            title={t("rc.changelog")}
            aria-label={t("rc.openChangelog")}
            aria-expanded={changelogOpen}
            onClick={() => setChangelogOpen((value) => !value)}
          >
            <ScrollText className="size-4" />
          </Button>
          <ChangelogPopover open={changelogOpen} onClose={() => setChangelogOpen(false)} />
          <Button variant="outline" size="sm" onClick={() => setSearchOpen(true)}>
            <Search />
            <span className="hidden sm:inline">{t("dashboard.search")}</span>
            <kbd className="text-muted-foreground hidden rounded border px-1.5 py-0.5 text-[10px] lg:inline">Ctrl K</kbd>
          </Button>
          <Button variant="ghost" size="sm" onClick={() => rcService.openSettings()}>
            <Settings />
            {t("rc.settings")}
          </Button>
          <Button variant="ghost" size="icon" title={t("diagnostics.open")} aria-label={t("diagnostics.open")} onClick={() => void rcService.openDiagnostics()}>
            <Activity className="size-4" />
          </Button>
          <Button variant="ghost" size="sm" onClick={onDisconnect}>
            <LogOut />
            {t("rc.disconnect")}
          </Button>
        </div>
      </header>

      {reconnect?.active && (
        <div className="border-b border-amber-500/30 bg-amber-500/10 px-4 py-2">
          <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-2 text-xs">
            <WifiOff className="size-4 text-amber-500" />
            <span className="font-medium">{t("reconnect.title")}</span>
            <span className="text-muted-foreground">{t("reconnect.attempt", {attempt: reconnect.attempt, max: reconnect.maxAttempts})}</span>
            {reconnect.lastError && <span className="text-muted-foreground min-w-0 truncate" title={reconnect.lastError}>{reconnect.lastError}</span>}
            <div className="ml-auto flex items-center gap-1.5">
              <Button variant="outline" size="sm" onClick={() => rcService.reconnectNow().catch((err) => toast.error(t("reconnect.failed"), {description: String(err)}))}>
                <RotateCcw className="size-3.5" />{t("reconnect.retryNow")}
              </Button>
              <Button variant="ghost" size="sm" onClick={() => rcService.cancelReconnect().catch((err) => toast.error(t("reconnect.failed"), {description: String(err)}))}>
                <X className="size-3.5" />{t("reconnect.cancel")}
              </Button>
            </div>
          </div>
        </div>
      )}

      <div className="flex min-h-0 flex-1">
        <RcSidebar
          ncLabel={ncLabel(nc, players.length, t)}
          ncConnected={nc.connected && nc.authenticated}
          openServerText={openServerText}
        />
        <div className="flex min-h-0 flex-1 flex-col p-3">
          <Tabs
            value={activeChannel}
            onValueChange={setActiveChannel}
            className="flex min-h-0 flex-1 flex-col"
          >
            <div className="flex items-center justify-between gap-2">
              <TabsList>
                {tabs.map((t, i) => (
                  <TabsTrigger
                    key={t.channel || "server"}
                    value={t.channel}
                    draggable
                    onDragStart={() => (dragIndex.current = i)}
                    onDragOver={(e) => e.preventDefault()}
                    onDrop={() => {
                      if (dragIndex.current >= 0) reorderTabs(dragIndex.current, i)
                      dragIndex.current = -1
                    }}
                    onDragEnd={() => (dragIndex.current = -1)}
                    className="cursor-grab active:cursor-grabbing"
                  >
                    {t.label}
                  </TabsTrigger>
                ))}
              </TabsList>
            </div>
            {tabs.map((t) => (
              <TabsContent key={t.channel || "server"} value={t.channel} className="mt-2 min-h-0 flex-1">
                <ChatPane
                  messages={t.messages}
                  settings={settings}
                  onSend={(text) => send(t.channel, text)}
                  history={inputHistory}
                  players={players}
                  serverName={displayServer || serverName}
                  commandMacros={commandMacros}
                />
              </TabsContent>
            ))}
          </Tabs>
        </div>
      </div>
      <GlobalSearchPalette
        open={searchOpen}
        players={players}
        onOpen={() => setSearchOpen(true)}
        onClose={() => setSearchOpen(false)}
        onOpenPlayers={() => rcService.openPlayerList()}
        onOpenScripts={() => rcService.openScriptManager()}
        onOpenFiles={() => rcService.openFileBrowser()}
        onOpenSync={() => rcService.openSyncReview()}
        onOpenDeployments={() => rcService.openDeploymentCenter()}
        onOpenDiagnostics={() => rcService.openDiagnostics()}
        onOpenSettings={() => rcService.openSettings()}
        onOpenPlayerPM={(player) => rcService.openPlayerListPM(player.id)}
      />
    </div>
  )
}
