// AccountSelectScreen lists saved accounts and lets the user pick one to log in,
// remove it, rename it (client-only display label), or set an avatar photo. With
// no saved accounts it shows only an Add Account button. Pure presentational —
// all intents arrive via props.
import {useRef, useState} from "react"
import {ImagePlus, Loader2, Pencil, Plus, Trash2, UserRound} from "lucide-react"

import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import {Button} from "@/components/ui/button"
import {Card, CardContent, CardDescription, CardHeader, CardTitle} from "@/components/ui/card"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import {ScrollArea} from "@/components/ui/scroll-area"
import type {AccountSummary} from "@/types"

interface AccountSelectScreenProps {
  accounts: AccountSummary[]
  loading: boolean
  busy: boolean
  onSelect: (accountName: string) => void
  onRemove: (accountName: string) => void
  onAdd: () => void
  onRename: (accountName: string, displayName: string) => void | Promise<void>
  onSetType: (accountName: string, accountType: string) => void | Promise<void>
  onPhoto: (accountName: string, dataURL: string) => void | Promise<void>
}

// AccountTypeBadge renders the account type as a colored chip: Classic =
// light green, Reborn = brown. Shown in the account chooser.
function AccountTypeBadge({type}: {type: string}) {
  const isReborn = type === "Reborn"
  return (
    <span
      className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${
        isReborn ? "bg-amber-800/50 text-amber-200" : "bg-green-700/40 text-green-200"
      }`}
    >
      {isReborn ? "Reborn" : "Classic"}
    </span>
  )
}

function Avatar({photo, name, size = 36}: {photo?: string; name: string; size?: number}) {
  if (photo) {
    return (
      <img
        src={photo}
        alt={name}
        style={{width: size, height: size}}
        className="rounded-full border object-cover"
      />
    )
  }
  return (
    <div
      style={{width: size, height: size}}
      className="bg-muted flex items-center justify-center rounded-full border"
    >
      <UserRound style={{width: size * 0.55, height: size * 0.55}} className="text-muted-foreground" />
    </div>
  )
}

export function AccountSelectScreen({
  accounts,
  loading,
  busy,
  onSelect,
  onRemove,
  onAdd,
  onRename,
  onSetType,
  onPhoto,
}: AccountSelectScreenProps) {
  const [renaming, setRenaming] = useState<string | null>(null)
  const [display, setDisplay] = useState("")
  const [editType, setEditType] = useState<"Classic" | "Reborn">("Classic")
  const fileRef = useRef<HTMLInputElement>(null)
  const photoTarget = useRef<string | null>(null)

  const startRename = (acc: AccountSummary) => {
    setRenaming(acc.account)
    setDisplay(acc.displayName || acc.nickname || "")
    setEditType(acc.type === "Reborn" ? "Reborn" : "Classic")
  }

  const confirmRename = async () => {
    if (renaming) {
      await Promise.all([onRename(renaming, display.trim()), onSetType(renaming, editType)])
    }
    setRenaming(null)
  }

  const pickPhoto = (acc: AccountSummary) => {
    photoTarget.current = acc.account
    fileRef.current?.click()
  }

  const onFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    const target = photoTarget.current
    photoTarget.current = null
    e.target.value = "" // allow re-picking the same file
    if (!file || !target) return
    const dataURL = await new Promise<string>((resolve, reject) => {
      const r = new FileReader()
      r.onload = () => resolve(r.result as string)
      r.onerror = () => reject(new Error("read failed"))
      r.readAsDataURL(file)
    })
    void onPhoto(target, dataURL)
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">Graal Remote Control</CardTitle>
          <CardDescription>Select an account to sign in.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <input ref={fileRef} type="file" accept="image/*" className="hidden" onChange={onFile} />

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
                    <Avatar photo={account.photo} name={account.account} />
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => onSelect(account.account)}
                      className="flex flex-1 flex-col items-start gap-0.5 text-left disabled:opacity-50"
                    >
                      <span className="flex items-center gap-1.5">
                        <span className="text-sm font-medium">
                          {account.displayName || account.nickname || account.account}
                        </span>
                        <AccountTypeBadge type={account.type} />
                      </span>
                      <span className="text-xs text-muted-foreground">{account.account}</span>
                    </button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-muted-foreground"
                      disabled={busy}
                      aria-label={`Photo for ${account.account}`}
                      onClick={() => pickPhoto(account)}
                    >
                      <ImagePlus />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-muted-foreground"
                      disabled={busy}
                      aria-label={`Rename ${account.account}`}
                      onClick={() => startRename(account)}
                    >
                      <Pencil />
                    </Button>
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

      <AlertDialog open={renaming !== null} onOpenChange={(v) => !v && setRenaming(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Rename account</AlertDialogTitle>
            <AlertDialogDescription>
              A client-only label shown in the account list and RC header. It does not change your
              nickname or username.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="display-name">Display name</Label>
            <Input
              id="display-name"
              value={display}
              autoFocus
              onChange={(e) => setDisplay(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") confirmRename()
              }}
            />
          </div>

          <div className="grid gap-2">
            <Label>Account type</Label>
            <div className="grid grid-cols-2 gap-2">
              {(["Classic", "Reborn"] as const).map((t) => (
                <button
                  key={t}
                  type="button"
                  onClick={() => setEditType(t)}
                  className={`rounded-md border px-3 py-1.5 text-sm transition-colors ${
                    editType === t
                      ? t === "Reborn"
                        ? "border-amber-700 bg-amber-800/40 text-amber-200"
                        : "border-green-600 bg-green-700/30 text-green-200"
                      : "hover:bg-accent"
                  }`}
                >
                  {t}
                </button>
              ))}
            </div>
            <p className="text-muted-foreground text-xs">
              Selects the listserver used at login.
            </p>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setRenaming(null)}>
              Cancel
            </Button>
            <Button onClick={confirmRename}>Save</Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
