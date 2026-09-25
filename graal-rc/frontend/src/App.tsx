// App is the shell/orchestrator: it holds the view state machine and routes
// between the account-select, add-account, and server-list screens. Login
// intents go through useSession; the saved-account list lives in useAccounts.
// Destructive/login actions are gated by a confirmation dialog.
import {useCallback, useEffect, useRef, useState} from "react"
import {toast} from "sonner"
import {Events} from "@wailsio/runtime"

import {ConfirmDialog} from "@/components/ConfirmDialog"
import {AppWindowFrame} from "@/components/AppWindowFrame"
import {Button} from "@/components/ui/button"
import {EMPTY_RECOVERY, parseRecoveryStatus} from "@/lib/connectionRecovery"
import {rcService} from "@/services/rcService"
import {useAccounts} from "@/hooks/useAccounts"
import {useSession} from "@/hooks/useSession"
import {AccountSelectScreen} from "@/screens/AccountSelectScreen"
import {AddAccountScreen} from "@/screens/AddAccountScreen"
import {FileBrowserBackupWindowScreen} from "@/screens/FileBrowserBackupWindowScreen"
import {FileBrowserWindowScreen} from "@/screens/FileBrowserWindowScreen"
import {PlayerListWindowScreen} from "@/screens/PlayerListWindowScreen"
import {PmWindowScreen} from "@/screens/PmWindowScreen"
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
import {ChatLinkWindowScreen} from "@/screens/ChatLinkWindowScreen"
import {APP_VERSION} from "@/lib/appVersion"

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
  const [appVersion, setAppVersion] = useState(APP_VERSION)
  const [sessionNickname, setSessionNickname] = useState(() => {
    if (typeof window === "undefined") return ""
    return window.localStorage.getItem(NICKNAME_STORAGE_KEY) ?? ""
  })
  const updateCheckStarted = useRef(false)

  useEffect(() => {
    let active = true
    void rcService.getAppVersion().then((version) => {
      const normalized = version?.trim()
      if (active && normalized) setAppVersion(normalized)
    }).catch(() => {
      // The embedded frontend version remains a safe fallback while bindings load.
    })
    return () => { active = false }
  }, [])

  useEffect(() => {
    if (!language.configured) return
    if (updateCheckStarted.current) return
    updateCheckStarted.current = true
    let cancelled = false
    void rcService.checkForUpdates().then((info) => {
      if (cancelled || !info.updateAvailable || !info.downloadAvailable) return
      const automaticUpdate = info.platform === "windows" || info.platform === "ubuntu"
      toast.info(language.t("update.availableTitle", {version: info.latestVersion}), {
        description: language.t(automaticUpdate ? "update.availableDescription" : "update.manualDescription", {current: info.currentVersion}),
        duration: Infinity,
        action: {
          label: language.t(automaticUpdate ? "update.downloadAction" : "update.saveAction"),
          onClick: () => {
            toast.info(language.t(automaticUpdate ? "update.downloadingTitle" : "update.savingTitle", {version: info.latestVersion}), {
              description: language.t(automaticUpdate ? "update.downloadingDescription" : "update.savingDescription"),
              duration: Infinity,
            })
            if (automaticUpdate) {
              void rcService.installUpdate().catch((error) => {
                const message = error instanceof Error ? error.message : String(error)
                toast.error(language.t("update.failedTitle"), {description: message})
              })
              return
            }
            void rcService.saveUpdate().then((savedPath) => {
              if (!savedPath) return
              toast.success(language.t("update.savedTitle"), {
                description: language.t("update.savedDescription", {path: savedPath}),
              })
            }).catch((error) => {
              const message = error instanceof Error ? error.message : String(error)
              toast.error(language.t("update.failedTitle"), {description: message})
            })
          },
        },
      })
    }).catch(() => {
      // Release checks are best effort. The current RC remains usable offline.
    })
    return () => { cancelled = true }
  }, [language.configured, language.t])

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
  const [recovery, setRecovery] = useState(EMPTY_RECOVERY)
  const recoveryRevision = useRef(-1)
  const lifecycleSequence = useRef(0)

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

  useEffect(() => {
    let cancelled = false
    const accept = (value: unknown) => {
      const status = parseRecoveryStatus(value)
      if (cancelled || !status || status.revision <= recoveryRevision.current) return
      recoveryRevision.current = status.revision
      setRecovery(status)
      if (status.phase === "failed") {
        void rcService.getConnectionRecovery().then((current) => {
          if (cancelled || current.revision !== status.revision || current.phase !== "failed") return
          session.reset()
          setView("select")
          void accounts.refresh()
          toast.error(language.t("recovery.failed"), {description: status.reason})
        }).catch((error) => toast.error(String(error)))
      } else if (status.phase === "connected") {
        toast.success(language.t("recovery.connected"))
      }
    }
    const off = Events.On("rc:recovery", (event: {data: string}) => {
      try { accept(JSON.parse(event.data)) } catch { /* malformed event */ }
    })
    void rcService.getConnectionRecovery().then(accept).catch(() => {})
    return () => { cancelled = true; off() }
  }, [language.t, session.reset, accounts.refresh])

  // Unexpected server disconnects and terminal native event-pump failures
  // arrive through the ordered rc:evt envelope. Clear the live session so the
  // user cannot keep interacting with a dead handle, then show the reason on
  // the login screen.
  useEffect(() => {
    const off = Events.On("rc:evt", (event: {data: string}) => {
      try {
        const payload = JSON.parse(event.data) as {seq?: number; name?: string; data?: unknown[]}
        if (!["rc:connected", "rc:disconnected", "rc:pumpError"].includes(payload.name ?? "")) return
        if (typeof payload.seq === "number") {
          if (payload.seq <= lifecycleSequence.current) return
          lifecycleSequence.current = payload.seq
        }
        if (payload.name === "rc:connected") {
          // The native connection callback is emitted as soon as the main RC
          // socket authenticates. Do not keep the server picker waiting for
          // secondary rights/player-cache requests to finish.
          session.markConnected(session.selectedIndex, typeof payload.data?.[0] === "string" ? payload.data[0] : undefined)
          setView("rc")
          return
        }
        if (payload.name !== "rc:disconnected" && payload.name !== "rc:pumpError") return
        const reason = typeof payload.data?.[0] === "string" ? payload.data[0] : language.t("toast.disconnectedByServer")
        if (payload.data?.[1] === true) {
          if (typeof payload.data?.[2] === "number" && payload.data[2] <= recoveryRevision.current) return
          setRecovery((previous) => ({...previous, active: true, reason}))
          return
        }
        void (async () => {
          await returnToLogin()
          toast.error(language.t("toast.connectionLost"), {description: reason})
        })()
      } catch {
        // Ignore malformed lifecycle events; the session remains usable.
      }
    })
    return off
  }, [language.t, returnToLogin, session.markConnected, session.selectedIndex])

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

  if (recovery.active) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="w-full max-w-md space-y-4 rounded-lg border bg-card p-6">
          <div role="status" aria-live="polite" className="space-y-2">
            <h2 className="text-lg font-semibold">{language.t("recovery.title")}</h2>
            <p className="text-sm text-muted-foreground">{language.t("recovery.progress", {attempt: recovery.attempt, total: recovery.maxAttempts})}</p>
            <p className="text-sm text-muted-foreground">{language.t("recovery.description")}</p>
          </div>
          <Button variant="outline" onClick={() => void returnToLogin().catch((error) => toast.error(String(error)))}>{language.t("common.cancel")}</Button>
        </div>
      </div>
    )
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
        version={appVersion}
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
            ? language.t("login.confirmDeleteTitle", {account: pending.account})
            : language.t("login.confirmLoginTitle", {account: pending?.account ?? ""})
        }
        description={
          pending?.kind === "delete"
            ? language.t("login.confirmDeleteDescription")
            : language.t("login.confirmLoginDescription")
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
  const {t} = useLanguage()
  if (typeof window === "undefined") return <Shell />
  const hash = window.location.hash
  const route = hash.startsWith("#pm")
    ? {title: t("window.privateMessage"), content: <PmWindowScreen />}
    : hash.startsWith("#players")
      ? {title: t("window.playerList"), content: <PlayerListWindowScreen />}
    : hash.startsWith("#chat-link")
      ? {title: t("window.chatLink"), content: <ChatLinkWindowScreen />}
    : hash.startsWith("#rights")
      ? {title: t("window.rights"), content: <RightsWindowScreen />}
      : hash.startsWith("#attrs")
        ? {title: t("window.attributes"), content: <AttrsWindowScreen />}
        : hash.startsWith("#banhistory") || hash.startsWith("#staffactivity")
          ? {title: t("window.playerRecords"), content: <PlayerTextRecordWindowScreen />}
          : hash.startsWith("#ban")
            ? {title: t("window.access"), content: <BanWindowScreen />}
            : hash.startsWith("#comments")
              ? {title: t("window.comments"), content: <CommentsWindowScreen />}
              : hash.startsWith("#file-backup")
                ? {title: t("window.fileBrowserBackup"), content: <FileBrowserBackupWindowScreen />}
                : hash.startsWith("#files")
                  ? {title: t("window.fileBrowser"), content: <FileBrowserWindowScreen />}
                : hash.startsWith("#scripts")
                  ? {title: t("window.scriptManager"), content: <ScriptManagerWindowScreen />}
    : hash.startsWith("#settings")
      ? {title: t("window.settings"), content: <SettingsWindowScreen />}
      : hash.startsWith("#plugins")
        ? {title: t("window.plugins"), content: <PluginManagerWindowScreen />}
      : hash.startsWith("#plugin-docs")
        ? {title: t("window.pluginDocumentation"), content: <PluginDocumentationWindowScreen />}
      : hash.startsWith("#plugin-ui")
        ? {title: t("window.pluginUI"), content: <PluginUIWindowScreen />}
                      : hash.startsWith("#sync")
                      ? {title: t("window.syncReview"), content: <SyncReviewWindowScreen />}
                      : hash.startsWith("#deployments")
                        ? {title: t("window.changeHistory"), content: <DeploymentCenterWindowScreen />}
                      : hash.startsWith("#editor")
                        ? {title: t("window.scriptEditor"), content: <ScriptEditorWindowScreen />}
                        : hash.startsWith("#textfile")
                          ? {title: t("window.textEditor"), content: <TextEditorWindowScreen />}
                          : hash.startsWith("#sqlite")
                            ? {title: t("window.sqliteExplorer"), content: <SqliteExplorerWindowScreen />}
                            : {title: t("window.appTitle"), content: <Shell />}
  return <AppWindowFrame title={route.title}>{route.content}</AppWindowFrame>
}

export default App
