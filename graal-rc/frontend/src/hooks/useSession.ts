// useSession owns the live listserver session: servers, selection, and the
// connect/refresh/logout intents. Login intents (loginWithAccount/addAccount)
// go through here too so busy state and error toasts stay centralized. Screens
// stay presentational; this hook is the only place that mutates session state.
import {useCallback, useMemo, useState} from "react"
import {toast} from "sonner"

import type {RcService} from "@/services/rcService"
import type {LoginRequest, Server} from "@/types"
import {serverDisplay} from "@/lib/server"
import {useLanguage} from "@/hooks/useLanguage"

export type SessionPhase = "idle" | "ready"

export interface UseSessionResult {
  phase: SessionPhase
  servers: Server[]
  selectedIndex: number
  statusText: string
  busy: boolean
  connectedServer: string
  activeAccount: string
  select: (index: number) => void
  loginWithAccount: (accountName: string, nickname: string) => Promise<boolean>
  addAccount: (req: LoginRequest, nickname: string) => Promise<boolean>
  refresh: () => Promise<void>
  connect: (index: number) => Promise<boolean>
  logout: () => Promise<void>
}

export function useSession(service: RcService): UseSessionResult {
  const {t} = useLanguage()
  const [phase, setPhase] = useState<SessionPhase>("idle")
  const [servers, setServers] = useState<Server[]>([])
  const [selectedIndex, setSelectedIndex] = useState(0)
  const [statusText, setStatusText] = useState("")
  const [busy, setBusy] = useState(false)
  const [connectedServer, setConnectedServer] = useState("")
  const [activeAccount, setActiveAccount] = useState("")

  const run = useCallback(async <T,>(failureLabel: string, fn: () => Promise<T>): Promise<T | null> => {
    setBusy(true)
    try {
      return await fn()
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      toast.error(failureLabel, {description: message})
      return null
    } finally {
      setBusy(false)
    }
  }, [])

  const enterReady = useCallback((list: Server[], account: string) => {
    setServers(list)
    setSelectedIndex(0)
    setPhase("ready")
    setStatusText(t("session.serversAvailable", {count: list.length}))
    toast.success(t("session.loggedInAs", {account}))
  }, [t])

  const loginWithAccount = useCallback(
    async (accountName: string, nickname: string): Promise<boolean> => {
      setStatusText(t("session.connectingListserver"))
      const result = await run(t("session.loginFailed"), () => service.loginWithAccount(accountName, nickname))
      if (!result) {
        setStatusText("")
        return false
      }
      setActiveAccount(accountName)
      enterReady(result, accountName)
      return true
    },
    [run, service, enterReady, t]
  )

  const addAccount = useCallback(
    async (req: LoginRequest, nickname: string): Promise<boolean> => {
      setStatusText(t("session.connectingListserver"))
      const result = await run(t("session.loginFailed"), () => service.addAccount(req, nickname))
      if (!result) {
        setStatusText("")
        return false
      }
      setActiveAccount(req.account)
      enterReady(result, req.account)
      return true
    },
    [run, service, enterReady, t]
  )

  const refresh = useCallback(async (): Promise<void> => {
    const result = await run(t("session.refreshFailed"), () => service.getServers())
    if (!result) return
    setServers(result)
    setSelectedIndex(0)
    setStatusText(t("session.serversAvailable", {count: result.length}))
    toast.success(t("session.serverListRefreshed"))
  }, [run, service, t])

  const connect = useCallback(
    async (index: number): Promise<boolean> => {
      const server = servers[index]
      const label = server ? serverDisplay(server.name).label : t("session.serverFallback", {index})
      setStatusText(t("session.connectingTo", {server: label}))
      // connectToServer is a void/error-only binding: Wails resolves it to null
      // on success, so "no throw" (not a null return value) is the success
      // signal. Don't route through run() — its null return collides with the
      // legitimate null resolution.
      setBusy(true)
      try {
        await service.connectToServer(index)
        setConnectedServer(label)
        setStatusText(t("session.connectedTo", {server: label}))
        toast.success(t("session.connectedTo", {server: label}))
        return true
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        toast.error(t("session.connectFailed", {server: label}), {description: message})
        setStatusText("")
        return false
      } finally {
        setBusy(false)
      }
    },
    [service, servers, t]
  )

  const logout = useCallback(async (): Promise<void> => {
    await service.logout()
    setPhase("idle")
    setServers([])
    setSelectedIndex(0)
    setStatusText("")
    setConnectedServer("")
    setActiveAccount("")
  }, [service])

  const select = useCallback((index: number) => setSelectedIndex(index), [])

  return useMemo(
    () => ({
      phase,
      servers,
      selectedIndex,
      statusText,
      busy,
      connectedServer,
      activeAccount,
      select,
      loginWithAccount,
      addAccount,
      refresh,
      connect,
      logout,
    }),
    [phase, servers, selectedIndex, statusText, busy, connectedServer, activeAccount, select, loginWithAccount, addAccount, refresh, connect, logout]
  )
}
