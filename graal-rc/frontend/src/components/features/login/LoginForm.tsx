// Pure presentational login form. Knows nothing about the backend; it only
// reports the entered credentials through onLogin and reflects busy state.
import {useState} from "react"
import {Eye, EyeOff, Loader2, LogIn} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import type {LoginRequest} from "@/types"

interface LoginFormProps {
  busy?: boolean
  defaultValues?: Partial<LoginRequest>
  onLogin: (req: LoginRequest) => Promise<boolean> | boolean
}

export function LoginForm({busy = false, defaultValues, onLogin}: LoginFormProps) {
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
        <Label htmlFor="profile-name">Profile name</Label>
        <Input
          id="profile-name"
          placeholder="Account profile name"
          value={profileName}
          autoComplete="organization"
          onChange={(e) => setProfileName(e.target.value)}
        />
        <p className="text-muted-foreground text-xs">
          Used to choose the account profile/listserver. Leave empty for the default Graal listserver.
        </p>
      </div>
      <div className="grid gap-2">
        <Label htmlFor="account">Account</Label>
        <Input
          id="account"
          placeholder="Account id (e.g. Graal5766947) — NOT the community name"
          required
          autoComplete="username"
          value={account}
          onChange={(e) => setAccount(e.target.value)}
        />
        <p className="text-muted-foreground text-xs">
          Login identity. Player queries (rights/attrs/ban/comments) use this exact value, so it must be
          the account id, not your community name. The session nickname is configured on the account selection screen.
        </p>
      </div>
      <div className="grid gap-2">
        <Label htmlFor="password">Password</Label>
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
            aria-label={showPassword ? "Hide password" : "Show password"}
            title={showPassword ? "Hide password" : "Show password"}
            onClick={() => setShowPassword((visible) => !visible)}
            className="text-muted-foreground hover:text-foreground absolute inset-y-0 right-0 flex w-10 items-center justify-center transition-colors"
          >
            {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
          </button>
        </div>
      </div>
      <Button type="submit" disabled={busy} className="w-full">
        {busy ? <Loader2 className="animate-spin" /> : <LogIn />}
        Connect
      </Button>
    </form>
  )
}
