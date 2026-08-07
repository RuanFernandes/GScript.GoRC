// App is the shell/orchestrator: it holds the view state machine and routes
// between the account-select, add-account, and server-list screens. Login
// intents go through useSession; the saved-account list lives in useAccounts.
// Destructive/login actions are gated by a confirmation dialog.
import {useCallback, useEffect, useRef, useState} from "react"
import {toast} from "sonner"
import {Events} from "@wailsio/runtime"

import {ConfirmDialog} from "@/components/ConfirmDialog"
import {AppWindowFrame} from "@/components/AppWindowFrame"
import {rcService} from "@/services/rcService"
import {useAccounts} from "@/hooks/useAccounts"
import {useSession} from "@/hooks/useSession"
import {AccountSelectScreen} from "@/screens/AccountSelectScreen"
import {AddAccountScreen} from "@/screens/AddAccountScreen"
import {FileBrowserWindowScreen} from "@/screens/FileBrowserWindowScreen"
import {PlayerListWindowScreen} from "@/screens/PlayerListWindowScreen"
import {RightsWindowScreen} from "@/screens/RightsWindowScreen"
import {AttrsWindowScreen} from "@/screens/AttrsWindowScreen"
import {BanWindowScreen} from "@/screens/BanWindowScreen"
import {CommentsWindowScreen} from "@/screens/CommentsWindowScreen"
import {PlayerTextRecordWindowScreen} from "@/screens/PlayerTextRecordWindowScreen"
import {RcScreen} from "@/screens/RcScreen"
import {ScriptEditorWindowScreen} from "@/screens/ScriptEditorWindowScreen"
import {ScriptManagerWindowScreen} from "@/screens/ScriptManagerWindowScreen"
import {ServerListScreen} from "@/screens/ServerListScreen"
import {SettingsWindowScreen} from "@/screens/SettingsWindowScreen"
import {SyncReviewWindowScreen} from "@/screens/SyncReviewWindowScreen"
import {DeploymentCenterWindowScreen} from "@/screens/DeploymentCenterWindowScreen"
import {DiagnosticsWindowScreen} from "@/screens/DiagnosticsWindowScreen"
import {SqliteExplorerWindowScreen} from "@/screens/SqliteExplorerWindowScreen"
import {TextEditorWindowScreen} from "@/screens/TextEditorWindowScreen"
import type {AppView, LoginRequest} from "@/types"
import type {PluginNotification, PluginNotificationLevel} from "@/plugins/types"
import {useLanguage} from "@/hooks/useLanguage"
import {LanguageWelcomeScreen} from "@/screens/LanguageWelcomeScreen"
import {pluginRuntime} from "@/plugins/runtime"
import {PluginManagerWindowScreen} from "@/screens/PluginManagerWindowScreen"
import {PluginDocumentationWindowScreen} from "@/screens/PluginDocumentationWindowScreen"
import {PluginUIWindowScreen} from "@/screens/PluginUIWindowScreen"

type PendingConfirm =
  | {kind: "login"; account: string}
  | {kind: "delete"; account: string}
  | null

const NICKNAME_STORAGE_KEY = "graal-rc:sessionNickname"

// Shell is the main window's orchestrator (select/add/serverlist/rc). The
// external player-list window renders its own screen via the App router below.
function Shell() {
  const session = useSession(rcService)
  const accounts = useAccounts(rcService)
  const language = useLanguage()
  const [view, setView] = useState<AppView>("select")
  const [pending, setPending] = useState<PendingConfirm>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)
  const [sessionNickname, setSessionNickname] = useState(() => {
    if (typeof window === "undefined") return ""
    return window.localStorage.getItem(NICKNAME_STORAGE_KEY) ?? ""
  })

  useEffect(() => {
    void pluginRuntime.start()
    return () => { void pluginRuntime.stop() }
  }, [])

  useEffect(() => {
    const onNotification = (event: Event) => {
      const detail = (event as CustomEvent<PluginNotification & {level?: PluginNotificationLevel}>).detail
      if (!detail?.message?.trim()) return
      const title = detail.title?.trim() || detail.message
      const options = {description: detail.title?.trim() ? detail.message : undefined, duration: detail.durationMs}
      switch (detail.level) {
        case "success": toast.success(title, options); break
        case "warning": toast.warning(title, options); break
        case "error": toast.error(title, options); break
        default: toast.info(title, options); break
      }
    }
    window.addEventListener("gorc:plugin-notification", onNotification)
    return () => window.removeEventListener("gorc:plugin-notification", onNotification)
  }, [])

  useEffect(() => {
    window.localStorage.setItem(NICKNAME_STORAGE_KEY, sessionNickname)
  }, [sessionNickname])
  const returningToLogin = useRef(false)

  const handleAddAccount = async (req: LoginRequest): Promise<boolean> => {
    if (await session.addAccount(req, sessionNickname.trim())) {
      await accounts.refresh()
      setView("serverlist")
      return true
    }
    return false
  }

  const handleLogout = async () => {
    await session.logout()
    await accounts.refresh()
    setView("select")
  }

  // Connect to a server; on success leave the server list for the RC screen.
  const handleServerConnect = async (index: number) => {
    const connected = await session.connect(index)
    if (!connected) {
      await returnToLogin()
    }
  }

  const returnToLogin = useCallback(async () => {
    if (returningToLogin.current) return
    returningToLogin.current = true
    try {
      await session.logout()
      await accounts.refresh()
      setView("select")
    } finally {
      returningToLogin.current = false
    }
  }, [accounts.refresh, session.logout])

  // Unexpected server disconnects arrive through the ordered rc:evt envelope.
  // The backend owns bounded recovery; keep the RC surface mounted while it
  // retries so the operator can see progress or cancel it explicitly.
  useEffect(() => {
    const offEnvelope = Events.On("rc:evt", (event: {data: string}) => {
      try {
        const payload = JSON.parse(event.data) as {name?: string; data?: unknown[]}
        if (payload.name !== "rc:disconnected" && payload.name !== "rc:pumpError") return
        const reason = typeof payload.data?.[0] === "string" ? payload.data[0] : language.t("toast.disconnectedByServer")
        toast.warning(language.t("toast.reconnecting"), {description: reason})
      } catch {
        // Ignore malformed lifecycle events; the recovery status remains usable.
      }
    })
    const offReconnected = Events.On("rc:reconnected", () => {
      toast.success(language.t("toast.reconnected"))
    })
    const offFailed = Events.On("rc:reconnectFailed", (event: {data: string}) => {
      let description = language.t("toast.connectionLost")
      try {
        const payload = JSON.parse(event.data) as {lastError?: string}
        if (payload.lastError) description = payload.lastError
      } catch {
        // Keep the generic description for malformed status payloads.
      }
      void (async () => {
        await returnToLogin()
        toast.error(language.t("toast.reconnectFailed"), {description})
      })()
    })
    const offCancelled = Events.On("rc:reconnectCancelled", () => {
      void returnToLogin()
    })
    return () => {
      offEnvelope()
      offReconnected()
      offFailed()
      offCancelled()
    }
  }, [returnToLogin, language.t])

  // State-driven safety net: the moment a server is connected (connectedServer
  // becomes non-empty), ensure we are on the RC screen regardless of which code
  // path completed the connect.
  useEffect(() => {
    if (session.connectedServer && view === "serverlist") setView("rc")
  }, [session.connectedServer, view])

  // Disconnect from the RC screen drops the whole session (grclib has no
  // game-only leave), so we return to the account-select screen.
  const handleRcDisconnect = async () => {
    await session.logout()
    await accounts.refresh()
    setView("select")
  }

  // Confirm dialog handlers: the list screen only stages an intent; the dialog
  // resolves it.
  const confirmLogin = async () => {
    if (pending?.kind !== "login") return
    const account = pending.account
    setConfirmBusy(true)
    try {
      if (await session.loginWithAccount(account, sessionNickname.trim())) setView("serverlist")
    } finally {
      setConfirmBusy(false)
      setPending(null)
    }
  }

  const confirmDelete = async () => {
    if (pending?.kind !== "delete") return
    const account = pending.account
    setConfirmBusy(true)
    try {
      await rcService.removeAccount(account)
      toast.success(language.t("login.accountRemoved", {name: account}))
      await accounts.refresh()
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      toast.error(language.t("login.removeFailed"), {description: message})
    } finally {
      setConfirmBusy(false)
      setPending(null)
    }
  }

  const cancelConfirm = () => setPending(null)

  if (language.needsLanguage) {
    return <LanguageWelcomeScreen language={language.language} onConfirm={language.setLanguage} />
  }

  if (view === "add") {
    return (
      <AddAccountScreen
        busy={session.busy}
        nickname={sessionNickname}
        onLogin={handleAddAccount}
        onCancel={() => setView("select")}
      />
    )
  }

  if (view === "serverlist" && session.phase === "ready") {
    return (
      <ServerListScreen
        servers={session.servers}
        selectedIndex={session.selectedIndex}
        statusText={session.statusText}
        busy={session.busy}
        onSelect={session.select}
        onConnect={handleServerConnect}
        onRefresh={session.refresh}
        onLogout={handleLogout}
      />
    )
  }

  if (view === "rc") {
    return (
      <RcScreen
        serverName={session.connectedServer}
        accountName={session.activeAccount}
        onDisconnect={handleRcDisconnect}
      />
    )
  }

  return (
    <>
      <AccountSelectScreen
        accounts={accounts.accounts}
        loading={accounts.loading}
        busy={session.busy}
        nickname={sessionNickname}
        onNicknameChange={setSessionNickname}
        onSelect={(accountName, nickname) => {
          if (!nickname.trim()) {
            toast.error(language.t("login.nicknameRequired"), {description: language.t("login.nicknameRequiredDescription")})
            return
          }
          setSessionNickname(nickname)
          setPending({kind: "login", account: accountName})
        }}
        onRemove={(accountName) => setPending({kind: "delete", account: accountName})}
        onAdd={() => {
          if (!sessionNickname.trim()) {
            toast.error(language.t("login.nicknameRequired"), {description: language.t("login.nicknameRequiredDescription")})
            return
          }
          setView("add")
        }}
        onRename={async (accountName, displayName) => {
          try {
            await rcService.renameAccount(accountName, displayName)
            await accounts.refresh()
            toast.success(language.t("login.accountRenamed"))
          } catch (err) {
            toast.error(language.t("login.renameFailed"), {description: String(err)})
          }
        }}
        onPhoto={async (accountName, dataURL) => {
          try {
            await rcService.setAccountPhoto(accountName, dataURL)
            await accounts.refresh()
            toast.success(language.t("login.photoUpdated"))
          } catch (err) {
            toast.error(language.t("login.photoUpdateFailed"), {description: String(err)})
          }
        }}
      />
      <ConfirmDialog
        open={pending !== null}
        busy={confirmBusy}
        destructive={pending?.kind === "delete"}
        title={
          pending?.kind === "delete"
            ? `Delete account "${pending.account}"?`
            : `Log in as "${pending?.account ?? ""}"?`
        }
        description={
          pending?.kind === "delete"
            ? "This removes the saved account from this device. You can add it again later."
            : "This will connect to the listserver with this account."
        }
        confirmLabel={pending?.kind === "delete" ? language.t("common.delete") : language.t("common.logIn")}
        cancelLabel={language.t("common.cancel")}
        onConfirm={pending?.kind === "delete" ? confirmDelete : confirmLogin}
        onCancel={cancelConfirm}
      />
    </>
  )
}

// App is the window router: external windows load the SPA at a hash route and
// render their dedicated screen; the main window gets Shell.
function App() {
  if (typeof window === "undefined") return <Shell />
  const hash = window.location.hash
  const route = hash.startsWith("#players")
    ? {title: "Player List", content: <PlayerListWindowScreen />}
    : hash.startsWith("#rights")
      ? {title: "Rights", content: <RightsWindowScreen />}
      : hash.startsWith("#attrs")
        ? {title: "Attributes", content: <AttrsWindowScreen />}
        : hash.startsWith("#banhistory") || hash.startsWith("#staffactivity")
          ? {title: "Player Records", content: <PlayerTextRecordWindowScreen />}
          : hash.startsWith("#ban")
            ? {title: "Access", content: <BanWindowScreen />}
            : hash.startsWith("#comments")
              ? {title: "Comments", content: <CommentsWindowScreen />}
              : hash.startsWith("#files")
                ? {title: "File Browser", content: <FileBrowserWindowScreen />}
                : hash.startsWith("#scripts")
                  ? {title: "Script Manager", content: <ScriptManagerWindowScreen />}
    : hash.startsWith("#settings")
      ? {title: "Settings", content: <SettingsWindowScreen />}
      : hash.startsWith("#plugins")
        ? {title: "Plugins", content: <PluginManagerWindowScreen />}
      : hash.startsWith("#plugin-docs")
        ? {title: "Plugin Documentation", content: <PluginDocumentationWindowScreen />}
      : hash.startsWith("#plugin-ui")
        ? {title: "Plugin UI", content: <PluginUIWindowScreen />}
                      : hash.startsWith("#sync")
                      ? {title: "Sync Review", content: <SyncReviewWindowScreen />}
                      : hash.startsWith("#deployments")
                        ? {title: "Change History", content: <DeploymentCenterWindowScreen />}
                      : hash.startsWith("#diagnostics")
                        ? {title: "Diagnostics", content: <DiagnosticsWindowScreen />}
                      : hash.startsWith("#editor")
                        ? {title: "Script Editor", content: <ScriptEditorWindowScreen />}
                        : hash.startsWith("#textfile")
                          ? {title: "Text Editor", content: <TextEditorWindowScreen />}
                          : hash.startsWith("#sqlite")
                            ? {title: "SQLite Explorer", content: <SqliteExplorerWindowScreen />}
                            : {title: "Graal Remote Control", content: <Shell />}
  return <AppWindowFrame title={route.title}>{route.content}</AppWindowFrame>
}

export default App
