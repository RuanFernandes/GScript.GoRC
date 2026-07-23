// RcScreen is the main Remote Control surface shown after connecting to a
// server: a tabbed chat (always-on "RC Chat" + dynamic IRC channel tabs), a
// command input, an NC (script socket) status badge, a toggleable player list
// panel, and a chat-color settings dialog. Mirrors the reference client's
// TRemoteFrame.
import {useEffect, useRef, useState} from "react"
import {Code2, Flag, FolderOpen, FolderTree, LogOut, Settings, SlidersHorizontal, UserRound, Users} from "lucide-react"
import {toast} from "sonner"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {ChatLine} from "@/components/features/chat/ChatLine"
import {ScriptHelpResult} from "@/components/features/chat/ScriptHelpResult"
import {useChat} from "@/hooks/useChat"
import {useChatInputHistory} from "@/hooks/useChatInputHistory"
import {useChatSettings} from "@/hooks/useChatSettings"
import {usePlayers} from "@/hooks/usePlayers"
import {serverDisplay} from "@/lib/server"
import {formatLogLine} from "@/lib/chatLine"
import {rcService} from "@/services/rcService"
import type {AccountSummary, ChatMessage, ChatSettings, NCStatus} from "@/types"

interface RcScreenProps {
  serverName: string
  accountName: string
  onDisconnect: () => void
}

function ncLabel(s: NCStatus): string {
  if (!s.hasNc) return "No NC server"
  if (s.authenticated) return "NC authenticated"
  if (s.connected) return "NC connecting"
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
}: {
  messages: ChatMessage[]
  settings: ChatSettings
  onSend: (text: string) => Promise<boolean>
  history: ReturnType<typeof useChatInputHistory>
}) {
  const [text, setText] = useState("")
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
        <Input
          value={text}
          onChange={(e) => setText(history.onTextChange(e.target.value))}
          onKeyDown={(e) => history.handleKeyDown(e, text, setText)}
          placeholder="Type a message or command…"
          autoComplete="off"
        />
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
  const apelido = profile?.displayName || profile?.nickname || accountName

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
            {displayServer ? `${displayServer}: ` : ""}
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

      <header className="flex items-center gap-3 border-b px-4 py-2.5">
        <Badge variant="secondary">{ncLabel(nc)}</Badge>
        <div className="ml-auto flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => rcService.openScriptManager()}>
            <Code2 />
            Scripts
          </Button>
          <Button variant="outline" size="sm" onClick={() => rcService.openFileBrowser()}>
            <FolderOpen />
            Files
          </Button>
          <Button variant="outline" size="sm" onClick={() => rcService.openPlayerList()}>
            <Users />
            Players
          </Button>
          <Button variant="outline" size="sm" onClick={() => openServerText("options", "Server Options")}>
            <SlidersHorizontal />
            Server Options
          </Button>
          <Button variant="outline" size="sm" onClick={() => openServerText("folder_config", "Folder Config")}>
            <FolderTree />
            Folder Config
          </Button>
          <Button variant="outline" size="sm" onClick={() => openServerText("flags", "Server Flags")}>
            <Flag />
            Server Flags
          </Button>
        </div>
      </header>

      <div className="flex min-h-0 flex-1 flex-col p-3">
        <Tabs value={activeChannel} onValueChange={setActiveChannel} className="flex min-h-0 flex-1 flex-col">
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
          {tabs.map((t) => (
            <TabsContent key={t.channel || "server"} value={t.channel} className="mt-2 min-h-0 flex-1">
              <ChatPane
                messages={t.messages}
                settings={settings}
                onSend={(text) => send(t.channel, text)}
                history={inputHistory}
              />
            </TabsContent>
          ))}
        </Tabs>
      </div>
    </div>
  )
}
