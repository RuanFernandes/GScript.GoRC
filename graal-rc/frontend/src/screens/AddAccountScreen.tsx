// AddAccountScreen composes the visual chrome around LoginForm for the add
// flow. On successful login the backend persists the account; the caller (App)
// then routes to the server list. The Back button cancels back to the select
// screen.
import {ArrowLeft} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Card, CardContent, CardDescription, CardHeader, CardTitle} from "@/components/ui/card"
import {LoginForm} from "@/components/features/login/LoginForm"
import type {LoginRequest} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

interface AddAccountScreenProps {
  busy: boolean
  nickname: string
  onLogin: (req: LoginRequest) => Promise<boolean> | boolean
  onCancel: () => void
}

export function AddAccountScreen({busy, nickname, onLogin, onCancel}: AddAccountScreenProps) {
  const {t} = useLanguage()
  return (
    <div className="flex min-h-svh items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">{t("login.addAccount")}</CardTitle>
          <CardDescription>
            {t("login.signInStaff")} {t("login.sessionNickname")} <strong>{nickname || t("common.notSet")}</strong>.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <LoginForm busy={busy} onLogin={onLogin} />
          <Button type="button" variant="ghost" className="w-full" onClick={onCancel} disabled={busy}>
            <ArrowLeft />
            {t("common.back")}
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}
