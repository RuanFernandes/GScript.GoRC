// Pure presentational login form. Knows nothing about the backend; it only
// reports the entered credentials through onLogin and reflects busy state.
import {useState} from "react"
import {Loader2, LogIn} from "lucide-react"

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
  const [nickname, setNickname] = useState(defaultValues?.nickname ?? "")
  const [account, setAccount] = useState(defaultValues?.account ?? "")
  const [password, setPassword] = useState(defaultValues?.password ?? "")

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    await onLogin({nickname, account, password})
  }

  return (
    <form onSubmit={handleSubmit} className="grid gap-4">
      <div className="grid gap-2">
        <Label htmlFor="nickname">Nickname</Label>
        <Input
          id="nickname"
          placeholder="Community name / display nick (e.g. Repinho)"
          value={nickname}
          autoComplete="username"
          onChange={(e) => setNickname(e.target.value)}
        />
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
          the account id, not your community name. The nickname above is what others see in-game.
        </p>
      </div>
      <div className="grid gap-2">
        <Label htmlFor="password">Password</Label>
        <Input
          id="password"
          type="password"
          placeholder="••••••••"
          required
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </div>
      <Button type="submit" disabled={busy} className="w-full">
        {busy ? <Loader2 className="animate-spin" /> : <LogIn />}
        Connect
      </Button>
    </form>
  )
}
