// RcScreen is the main Remote Control surface shown after connecting to a
// server: a tabbed chat (always-on "RC Chat" + dynamic IRC channel tabs), a
// command input, an NC (script socket) status badge, a toggleable player list
// panel, and a chat-color settings dialog. Mirrors the reference client's
// TRemoteFrame.
import {useEffect, useRef, useState} from "react"
import {LogOut, Settings, UserRound} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {ChatLine} from "@/components/features/chat/ChatLine"
import {ScriptHelpResult} from "@/components/features/chat/ScriptHelpResult"
import {RcSidebar} from "@/components/features/rc/RcSidebar"
import {useChat} from "@/hooks/useChat"
import {useChatAutocomplete} from "@/hooks/useChatAutocomplete"
import {useChatInputHistory} from "@/hooks/useChatInputHistory"
import {useChatSettings} from "@/hooks/useChatSettings"
import {usePlayers} from "@/hooks/usePlayers"
import {serverDisplay} from "@/lib/server"
import {formatLogLine} from "@/lib/chatLine"
import {rcService} from "@/services/rcService"
import type {AccountSummary, ChatMessage, ChatSettings, NCStatus, Player} from "@/types"

interface RcScreenProps {
  serverName: string
  accountName: string
  onDisconnect: () => void
}

function ncLabel(s: NCStatus, playerCount: number): string {
  if (!s.hasNc) return "No NC server"
  if (s.authenticated) {
    return `Connected · ${playerCount} player${playerCount === 1 ? "" : "s"}`
  }
  if (s.connected) return "Connecting"
  return "NC off"
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
}: {
  messages: ChatMessage[]
  settings: ChatSettings
  onSend: (text: string) => Promise<boolean>
  history: ReturnType<typeof useChatInputHistory>
  players: Player[]
}) {
  const [text, setText] = useState("")
  const [ghostOff, setGhostOff] = useState(false)
  const autocomplete = useChatAutocomplete(players)
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
            <p className="text-muted-foreground">No messages yet.</p>
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
                aria-label={accountPhase ? "Player suggestions" : "Command suggestions"}
                className="bg-popover text-popover-foreground absolute right-0 bottom-full left-0 z-30 mb-2 max-h-64 overflow-y-auto rounded-md border p-1 shadow-lg"
              >
                <div className="text-muted-foreground flex items-center justify-between px-2 py-1 text-[11px]">
                  <span>{accountPhase ? "Players" : "Commands"}</span>
                  <span>Tab to complete</span>
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
                        {option === "open" || option === "openrights" || option === "opencomments" || option === "openaccess" || option === "openacc" || option === "openprofile" || option === "playerinfo" || option === "disconnect" || option === "reset" || option === "staffactivity" ? "player" : "command"}
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
            placeholder="Message or /command · Tab to complete"
            autoComplete="off"
          />
        </div>
        <Button type="submit">Send</Button>
      </form>
    </div>
  )
}

export function RcScreen({serverName, accountName, onDisconnect}: RcScreenProps) {
  const {tabs, activeChannel, setActiveChannel, send, reorderTabs} = useChat(rcService)
  const inputHistory = useChatInputHistory()
  const {settings} = useChatSettings()
  const [nc, setNc] = useState<NCStatus>({hasNc: false, connected: false, authenticated: false})
  const [profile, setProfile] = useState<AccountSummary | null>(null)
  const {players} = usePlayers(rcService, true)
  const dragIndex = useRef<number>(-1)

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

  const {label: displayServer} = serverDisplay(serverName)
  const apelido = profile?.displayName || accountName

  // Open a server-side text config editor (options/folder_config/flags). These
  // travel on the main socket, not NC, so they're available without script
  // rights; a no-rights/timeout fetch rejects and toasts instead of opening a
  // blank window.
  const openServerText = (kind: "options" | "folder_config" | "flags", label: string) => {
    rcService.openScriptEditor(kind, label).catch((err: unknown) => {
      toast.error(`Couldn't open ${label}`, {description: String(err)})
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
            {players.length} player{players.length === 1 ? "" : "s"} online
          </span>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={() => rcService.openSettings()}>
            <Settings />
            Settings
          </Button>
          <Button variant="ghost" size="sm" onClick={onDisconnect}>
            <LogOut />
            Disconnect
          </Button>
        </div>
      </header>

      <div className="flex min-h-0 flex-1">
        <RcSidebar
          ncLabel={ncLabel(nc, players.length)}
          ncConnected={nc.connected && nc.authenticated}
          openServerText={openServerText}
        />
        <div className="flex min-h-0 flex-1 flex-col p-3">
        <Tabs value={activeChannel} onValueChange={setActiveChannel} className="flex min-h-0 flex-1 flex-col">
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
              />
            </TabsContent>
          ))}
        </Tabs>
        </div>
      </div>
    </div>
  )
}
