// AccountSelectScreen lists saved accounts (nickname/account) and lets the user
// pick one to log in or remove it. With no saved accounts it shows only an
// Add Account button. Pure presentational — all intents arrive via props.
import {Loader2, Plus, Trash2, UserRound} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Card, CardContent, CardDescription, CardHeader, CardTitle} from "@/components/ui/card"
import {ScrollArea} from "@/components/ui/scroll-area"
import type {AccountSummary} from "@/types"

interface AccountSelectScreenProps {
  accounts: AccountSummary[]
  loading: boolean
  busy: boolean
  onSelect: (accountName: string) => void
  onRemove: (accountName: string) => void
  onAdd: () => void
}

export function AccountSelectScreen({
  accounts,
  loading,
  busy,
  onSelect,
  onRemove,
  onAdd,
}: AccountSelectScreenProps) {
  return (
    <div className="flex min-h-svh items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">Graal Remote Control</CardTitle>
          <CardDescription>Select an account to sign in.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          {loading ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="animate-spin" />
            </div>
          ) : accounts.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-6 text-center">
              <UserRound className="text-muted-foreground" />
              <p className="text-sm text-muted-foreground">No saved accounts.</p>
            </div>
          ) : (
            <ScrollArea className="max-h-72">
              <div className="grid gap-2 pr-2">
                {accounts.map((account) => (
                  <div
                    key={account.account}
                    className="flex items-center gap-2 rounded-md border p-2 transition-colors hover:bg-accent"
                  >
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => onSelect(account.account)}
                      className="flex flex-1 flex-col items-start gap-0.5 text-left disabled:opacity-50"
                    >
                      <span className="text-sm font-medium">
                        {account.nickname || account.account}
                      </span>
                      <span className="text-xs text-muted-foreground">{account.account}</span>
                    </button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-muted-foreground"
                      disabled={busy}
                      aria-label={`Remove ${account.account}`}
                      onClick={() => onRemove(account.account)}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                ))}
              </div>
            </ScrollArea>
          )}

          <Button type="button" className="w-full" onClick={onAdd} disabled={busy}>
            <Plus />
            Add Account
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}
