import {FormEvent, useState} from "react"
import {KeyRound, Loader2, LogIn, LogOut, UserPlus} from "lucide-react"
import {toast} from "sonner"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import {useLanguage} from "@/hooks/useLanguage"
import {rcService} from "@/services/rcService"
import type {ScriptGalleryAuthState} from "@/types"

type AuthMode = "login" | "register"

interface ScriptGalleryAuthPanelProps {
  state: ScriptGalleryAuthState | null
  loading: boolean
  onChanged: () => Promise<void>
}

export function ScriptGalleryAuthPanel({state, loading, onChanged}: ScriptGalleryAuthPanelProps) {
  const {t} = useLanguage()
  const [mode, setMode] = useState<AuthMode>("login")
  const [password, setPassword] = useState("")
  const [confirmation, setConfirmation] = useState("")
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!state?.identity.username) return
    if (password.length < 8) {
      toast.error(t("gallery.passwordTooShort"))
      return
    }
    if (mode === "register" && password !== confirmation) {
      toast.error(t("gallery.passwordMismatch"))
      return
    }
    setBusy(true)
    try {
      if (mode === "register") {
        await rcService.registerScriptGallery(password)
      } else {
        await rcService.loginScriptGallery(password)
      }
      setPassword("")
      setConfirmation("")
      await onChanged()
      toast.success(mode === "register" ? t("gallery.registered") : t("gallery.loggedIn"))
    } catch {
      toast.error(t("gallery.authFailed"))
    } finally {
      setBusy(false)
    }
  }

  const logout = async () => {
    setBusy(true)
    try {
      await rcService.logoutScriptGallery()
      await onChanged()
      toast.success(t("gallery.loggedOut"))
    } catch {
      toast.error(t("gallery.logoutFailed"))
    } finally {
      setBusy(false)
    }
  }

  if (loading || !state) {
    return (
      <div className="flex items-center gap-2 border-b bg-muted/10 px-4 py-2 text-xs text-muted-foreground">
        <Loader2 className="size-3.5 animate-spin" />{t("gallery.authLoading")}
      </div>
    )
  }

  if (state.authenticated) {
    return (
      <div className="flex flex-wrap items-center gap-2 border-b bg-primary/5 px-4 py-2">
        <KeyRound className="size-4 text-primary" />
        <span className="text-xs text-muted-foreground">{t("gallery.signedInAs")}</span>
        <Badge variant="outline">{state.displayName || state.username || state.identity.username}</Badge>
        <span className="text-muted-foreground min-w-0 flex-1 text-[11px]">{t("gallery.accountScoped")}</span>
        <Button variant="ghost" size="sm" onClick={() => void logout()} disabled={busy}>
          {busy ? <Loader2 className="animate-spin" /> : <LogOut />}{t("gallery.logOut")}
        </Button>
      </div>
    )
  }

  return (
    <div className="border-b bg-muted/10 px-4 py-3">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <KeyRound className="size-4 text-primary" />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium">{t("gallery.accountTitle")}</p>
          <p className="text-muted-foreground text-[11px]">{t("gallery.accountDescription")}</p>
        </div>
        <div className="flex rounded-md border p-0.5" role="tablist" aria-label={t("gallery.authMode")}>
          <button
            type="button"
            role="tab"
            aria-selected={mode === "login"}
            className={mode === "login" ? "rounded bg-background px-2.5 py-1 text-xs font-medium shadow-xs" : "rounded px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:text-foreground"}
            onClick={() => setMode("login")}
          >
            {t("gallery.logIn")}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mode === "register"}
            className={mode === "register" ? "rounded bg-background px-2.5 py-1 text-xs font-medium shadow-xs" : "rounded px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:text-foreground"}
            onClick={() => setMode("register")}
          >
            {t("gallery.register")}
          </button>
        </div>
      </div>
      <form className="grid gap-2 md:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)_minmax(0,1fr)_auto] md:items-end" onSubmit={(event) => void submit(event)}>
        <div className="space-y-1">
          <Label htmlFor="gallery-auth-username">{t("gallery.username")}</Label>
          <Input
            id="gallery-auth-username"
            value={state.identity.username}
            readOnly
            aria-readonly="true"
            title={t("gallery.usernameLocked")}
            className="bg-background/60"
          />
        </div>
        <div className="space-y-1">
          <Label htmlFor="gallery-auth-password">{t("gallery.password")}</Label>
          <Input
            id="gallery-auth-password"
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            autoComplete={mode === "register" ? "new-password" : "current-password"}
            minLength={8}
            maxLength={256}
            required
          />
        </div>
        {mode === "register" && (
          <div className="space-y-1">
            <Label htmlFor="gallery-auth-confirmation">{t("gallery.confirmPassword")}</Label>
            <Input
              id="gallery-auth-confirmation"
              type="password"
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              autoComplete="new-password"
              minLength={8}
              maxLength={256}
              required
            />
          </div>
        )}
        <Button type="submit" disabled={busy || !state.identity.username}>
          {busy ? <Loader2 className="animate-spin" /> : mode === "login" ? <LogIn /> : <UserPlus />}
          {mode === "login" ? t("gallery.logIn") : t("gallery.register")}
        </Button>
      </form>
      <p className="text-muted-foreground mt-2 text-[11px]">{t("gallery.usernameLockedDescription")}</p>
    </div>
  )
}
