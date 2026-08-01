// Pure presentational login form. Knows nothing about the backend; it only
// reports the entered credentials through onLogin and reflects busy state.
import {useState} from "react"
import {Eye, EyeOff, Loader2, LogIn} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import type {LoginRequest} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

interface LoginFormProps {
  busy?: boolean
  defaultValues?: Partial<LoginRequest>
  onLogin: (req: LoginRequest) => Promise<boolean> | boolean
}

export function LoginForm({busy = false, defaultValues, onLogin}: LoginFormProps) {
  const {t} = useLanguage()
  const [profileName, setProfileName] = useState(defaultValues?.profileName ?? "")
  const [account, setAccount] = useState(defaultValues?.account ?? "")
  const [password, setPassword] = useState(defaultValues?.password ?? "")
  const [showPassword, setShowPassword] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    await onLogin({profileName, account, password})
  }

  return (
    <form onSubmit={handleSubmit} className="grid gap-4">
      <div className="grid gap-2">
        <Label htmlFor="profile-name">{t("login.profileName")}</Label>
        <Input
          id="profile-name"
          placeholder={t("login.profilePlaceholder")}
          value={profileName}
          autoComplete="organization"
          onChange={(e) => setProfileName(e.target.value)}
        />
        <p className="text-muted-foreground text-xs">
          {t("login.profileHelp")}
        </p>
      </div>
      <div className="grid gap-2">
        <Label htmlFor="account">{t("login.account")}</Label>
        <Input
          id="account"
          placeholder={t("login.accountPlaceholder")}
          required
          autoComplete="username"
          value={account}
          onChange={(e) => setAccount(e.target.value)}
        />
        <p className="text-muted-foreground text-xs">
          {t("login.accountHelp")} {t("login.nicknameHelp")}
        </p>
      </div>
      <div className="grid gap-2">
        <Label htmlFor="password">{t("login.password")}</Label>
        <div className="relative">
          <Input
            id="password"
            type={showPassword ? "text" : "password"}
            placeholder="••••••••"
            required
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="pr-10"
          />
          <button
            type="button"
            aria-label={showPassword ? t("login.hidePassword") : t("login.showPassword")}
            title={showPassword ? t("login.hidePassword") : t("login.showPassword")}
            onClick={() => setShowPassword((visible) => !visible)}
            className="text-muted-foreground hover:text-foreground absolute inset-y-0 right-0 flex w-10 items-center justify-center transition-colors"
          >
            {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
          </button>
        </div>
      </div>
      <Button type="submit" disabled={busy} className="w-full">
        {busy ? <Loader2 className="animate-spin" /> : <LogIn />}
        {t("login.connect")}
      </Button>
    </form>
  )
}
