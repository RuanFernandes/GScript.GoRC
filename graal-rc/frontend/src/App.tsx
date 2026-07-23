// App is the shell/orchestrator: it holds the view state machine and routes
// between the account-select, add-account, and server-list screens. Login
// intents go through useSession; the saved-account list lives in useAccounts.
// Destructive/login actions are gated by a confirmation dialog.
import {useEffect, useState} from "react"
import {toast} from "sonner"

import {ConfirmDialog} from "@/components/ConfirmDialog"
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
import {SqliteExplorerWindowScreen} from "@/screens/SqliteExplorerWindowScreen"
import {TextEditorWindowScreen} from "@/screens/TextEditorWindowScreen"
import type {AppView, LoginRequest} from "@/types"

type PendingConfirm =
  | {kind: "login"; account: string}
  | {kind: "delete"; account: string}
  | null

// Shell is the main window's orchestrator (select/add/serverlist/rc). The
// external player-list window renders its own screen via the App router below.
function Shell() {
  const session = useSession(rcService)
  const accounts = useAccounts(rcService)
  const [view, setView] = useState<AppView>("select")
  const [pending, setPending] = useState<PendingConfirm>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)

  const handleAddAccount = async (req: LoginRequest): Promise<boolean> => {
    if (await session.addAccount(req)) {
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
    await session.connect(index)
  }

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
      if (await session.loginWithAccount(account)) setView("serverlist")
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
      toast.success(`Removed ${account}`)
      await accounts.refresh()
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      toast.error("Failed to remove account", {description: message})
    } finally {
      setConfirmBusy(false)
      setPending(null)
    }
  }

  const cancelConfirm = () => setPending(null)

  if (view === "add") {
    return (
      <AddAccountScreen
        busy={session.busy}
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
        onSelect={(accountName) => setPending({kind: "login", account: accountName})}
        onRemove={(accountName) => setPending({kind: "delete", account: accountName})}
        onAdd={() => setView("add")}
        onRename={async (accountName, displayName) => {
          try {
            await rcService.renameAccount(accountName, displayName)
            await accounts.refresh()
            toast.success("Account renamed")
          } catch (err) {
            toast.error("Rename failed", {description: String(err)})
          }
        }}
        onPhoto={async (accountName, dataURL) => {
          try {
            await rcService.setAccountPhoto(accountName, dataURL)
            await accounts.refresh()
            toast.success("Photo updated")
          } catch (err) {
            toast.error("Photo update failed", {description: String(err)})
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
        confirmLabel={pending?.kind === "delete" ? "Delete" : "Log in"}
        cancelLabel="Cancel"
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
  if (hash.startsWith("#players")) return <PlayerListWindowScreen />
  if (hash.startsWith("#rights")) return <RightsWindowScreen />
  if (hash.startsWith("#attrs")) return <AttrsWindowScreen />
  if (hash.startsWith("#banhistory")) return <PlayerTextRecordWindowScreen />
  if (hash.startsWith("#staffactivity")) return <PlayerTextRecordWindowScreen />
  if (hash.startsWith("#ban")) return <BanWindowScreen />
  if (hash.startsWith("#comments")) return <CommentsWindowScreen />
  if (hash.startsWith("#files")) return <FileBrowserWindowScreen />
  if (hash.startsWith("#scripts")) return <ScriptManagerWindowScreen />
  if (hash.startsWith("#settings")) return <SettingsWindowScreen />
  if (hash.startsWith("#editor")) return <ScriptEditorWindowScreen />
  if (hash.startsWith("#textfile")) return <TextEditorWindowScreen />
  if (hash.startsWith("#sqlite")) return <SqliteExplorerWindowScreen />
  return <Shell />
}

export default App
