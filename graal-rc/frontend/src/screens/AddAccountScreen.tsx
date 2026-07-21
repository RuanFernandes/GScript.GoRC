// AddAccountScreen composes the visual chrome around LoginForm for the add
// flow. On successful login the backend persists the account; the caller (App)
// then routes to the server list. The Back button cancels back to the select
// screen.
import {ArrowLeft} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Card, CardContent, CardDescription, CardHeader, CardTitle} from "@/components/ui/card"
import {LoginForm} from "@/components/features/login/LoginForm"
import type {LoginRequest} from "@/types"

interface AddAccountScreenProps {
  busy: boolean
  onLogin: (req: LoginRequest) => Promise<boolean> | boolean
  onCancel: () => void
}

export function AddAccountScreen({busy, onLogin, onCancel}: AddAccountScreenProps) {
  return (
    <div className="flex min-h-svh items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">Add Account</CardTitle>
          <CardDescription>
            Sign in with a staff account. It is saved on success.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <LoginForm busy={busy} onLogin={onLogin} />
          <Button type="button" variant="ghost" className="w-full" onClick={onCancel} disabled={busy}>
            <ArrowLeft />
            Back
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}
