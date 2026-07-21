// App is the shell/orchestrator: it holds the view state machine and routes
// between the account-select, add-account, and server-list screens. Login
// intents go through useSession; the saved-account list lives in useAccounts.
// Destructive/login actions are gated by a confirmation dialog.
import {useState} from "react"
import {toast} from "sonner"

import {ConfirmDialog} from "@/components/ConfirmDialog"
import {rcService} from "@/services/rcService"
import {useAccounts} from "@/hooks/useAccounts"
import {useSession} from "@/hooks/useSession"
import {AccountSelectScreen} from "@/screens/AccountSelectScreen"
import {AddAccountScreen} from "@/screens/AddAccountScreen"
import {ServerListScreen} from "@/screens/ServerListScreen"
import type {AppView, LoginRequest} from "@/types"

type PendingConfirm =
  | {kind: "login"; account: string}
  | {kind: "delete"; account: string}
  | null

function App() {
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
        onConnect={session.connect}
        onRefresh={session.refresh}
        onLogout={handleLogout}
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

export default App
