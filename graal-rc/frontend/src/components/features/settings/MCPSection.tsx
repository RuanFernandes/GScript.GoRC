import {useEffect, useMemo, useState} from "react"
import {Bot, Check, Copy, ExternalLink, FilePenLine, LoaderCircle, ShieldCheck, Terminal} from "lucide-react"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {rcService} from "@/services/rcService"
import type {MCPAgentStatus} from "@/types"

type Translator = (key: string, vars?: Record<string, string | number>) => string

const agents = [
  {id: "claude-code", label: "Claude Code", tone: "text-orange-400", mark: "CC"},
  {id: "codex", label: "Codex", tone: "text-emerald-400", mark: "CX"},
  {id: "opencode", label: "OpenCode", tone: "text-sky-400", mark: "OC"},
]

function setupPrompt(agent: string) {
  return `Configure the Graal RC MCP integration for ${agent}.

MCP endpoint: http://127.0.0.1:8765/mcp
Transport: HTTP JSON-RPC

This MCP is specifically for Graal development, including GraalScript and GS2.
When helping with GraalScript or GS2 code, use it to inspect RC Chat runtime
errors and read relevant remote scripts through the Graal File Browser when
the user asks for that context.

Register this server as "graal-rc" in your MCP configuration. Then remember these instructions for future Graal RC tasks:
- Use get_rc_chat to inspect the latest 50 RC Chat messages with timestamps when debugging.
- Ask before using send_rc_chat for any consequential or public message.
- Use filebrowser_list, filebrowser_cd, filebrowser_search, and filebrowser_read to inspect remote UTF-8 text files through the RC File Browser.
- The server is local-only and is available while the Graal RC desktop client is running.

After configuring it, verify the connection by listing the MCP tools.`
}

export function MCPSection({t}: {t: Translator}) {
  const [statuses, setStatuses] = useState<MCPAgentStatus[]>([])
  const [busy, setBusy] = useState("")
  const [copied, setCopied] = useState("")
  const [error, setError] = useState("")

  const refresh = () => rcService.getMCPAgentStatuses().then((value) => setStatuses(value ?? [])).catch(() => setError(t("settings.mcpLoadFailed")))
  useEffect(() => { void refresh() }, [])

  const copyPrompt = async (agent: string) => {
    try {
      await navigator.clipboard.writeText(setupPrompt(agent))
      setCopied(agent)
      window.setTimeout(() => setCopied(""), 1800)
    } catch {
      setError(t("settings.mcpCopyFailed"))
    }
  }

  const setup = async (agent: string) => {
    setBusy(agent)
    setError("")
    try {
      await rcService.setupMCP(agent)
      await refresh()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("settings.mcpSetupFailed"))
    } finally {
      setBusy("")
    }
  }

  const openFile = async (agent: string) => {
    setError("")
    try {
      await rcService.openMCPAgentFile(agent)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("settings.mcpOpenFailed"))
    }
  }

  const statusFor = useMemo(() => new Map(statuses.map((status) => [status.name, status])), [statuses])

  return (
    <div className="mx-auto grid max-w-3xl gap-5">
      <div>
        <div className="flex items-center gap-2"><Terminal className="text-primary size-5" /><h2 className="text-lg font-semibold tracking-tight">{t("settings.mcp")}</h2></div>
        <p className="text-muted-foreground mt-1 text-sm">{t("settings.mcpDescription")}</p>
      </div>

      <div className="border-primary/25 bg-primary/5 flex items-start gap-3 rounded-lg border p-4">
        <ShieldCheck className="text-primary mt-0.5 size-5 shrink-0" />
        <div className="grid gap-1 text-sm"><p className="font-medium">{t("settings.mcpLocalOnly")}</p><p className="text-muted-foreground text-xs leading-relaxed">{t("settings.mcpLocalOnlyDescription")}</p><code className="text-primary mt-1 text-xs">http://127.0.0.1:8765/mcp</code></div>
      </div>

      <div className="grid gap-3">
        {agents.map((agent) => {
          const status = statusFor.get(agent.id)
          const configured = status?.configured ?? false
          return <div key={agent.id} className="border-border bg-card/40 grid gap-3 rounded-lg border p-4 sm:grid-cols-[1fr_auto] sm:items-center">
            <div className="flex min-w-0 items-center gap-3">
              <div className={`flex size-10 shrink-0 items-center justify-center rounded-md border bg-background font-mono text-xs font-bold ${agent.tone}`}>{agent.mark}</div>
              <div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><p className="font-medium">{agent.label}</p>{configured && <Badge variant="secondary" className="gap-1"><Check className="size-3" />{t("settings.mcpConfigured")}</Badge>}</div><p className="text-muted-foreground mt-1 truncate font-mono text-[11px]">{status?.path ?? t("settings.mcpDetecting")}</p></div>
            </div>
            <div className="flex flex-wrap gap-2 sm:justify-end"><Button variant="ghost" size="sm" className="gap-1.5" disabled={!status?.exists} onClick={() => void openFile(agent.id)}><FilePenLine className="size-3.5" />{t("settings.mcpOpen")}</Button><Button variant="outline" size="sm" className="gap-1.5" onClick={() => void copyPrompt(agent.label)}>{copied === agent.id ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}{copied === agent.id ? t("settings.mcpCopied") : t("settings.mcpCopyPrompt")}</Button><Button size="sm" className="gap-1.5" disabled={busy === agent.id} onClick={() => void setup(agent.id)}>{busy === agent.id ? <LoaderCircle className="size-3.5 animate-spin" /> : <Bot className="size-3.5" />}{configured ? t("settings.mcpUpdate") : t("settings.mcpSetup")}</Button></div>
          </div>
        })}
      </div>

      {error && <p className="text-destructive text-xs">{error}</p>}

      <div className="border-border bg-muted/25 grid gap-3 rounded-lg border p-4"><div className="flex items-center gap-2"><ExternalLink className="text-muted-foreground size-4" /><p className="text-sm font-medium">{t("settings.mcpManualTitle")}</p></div><p className="text-muted-foreground text-xs leading-relaxed">{t("settings.mcpManualDescription")}</p><pre className="bg-background overflow-auto rounded-md border p-3 font-mono text-xs leading-relaxed">{setupPrompt("your AI")}</pre><Button variant="outline" size="sm" className="w-fit gap-1.5" onClick={() => void copyPrompt("your AI")}>{copied === "your AI" ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}{copied === "your AI" ? t("settings.mcpCopied") : t("settings.mcpCopyPrompt")}</Button></div>
    </div>
  )
}
