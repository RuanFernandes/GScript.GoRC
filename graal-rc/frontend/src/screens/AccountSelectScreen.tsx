// AccountSelectScreen lists saved accounts and lets the user pick one to log in,
// remove it, rename it (client-only display label), or set an avatar photo. With
// no saved accounts it shows only an Add Account button. Pure presentational —
// all intents arrive via props.
import {useRef, useState} from "react"
import {ImagePlus, Info, Loader2, Pencil, Plus, Trash2, UserRound} from "lucide-react"

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
import {useLanguage} from "@/hooks/useLanguage"

interface AccountSelectScreenProps {
  accounts: AccountSummary[]
  loading: boolean
  busy: boolean
  version: string
  nickname: string
  onNicknameChange: (nickname: string) => void
  onSelect: (accountName: string, nickname: string) => void
  onRemove: (accountName: string) => void
  onAdd: () => void
  onRename: (accountName: string, displayName: string) => void | Promise<void>
  onPhoto: (accountName: string, dataURL: string) => void | Promise<void>
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
  version,
  onSelect,
  onRemove,
  onAdd,
  onRename,
  onPhoto,
  nickname,
  onNicknameChange,
}: AccountSelectScreenProps) {
  const {t} = useLanguage()
  const [renaming, setRenaming] = useState<string | null>(null)
  const [display, setDisplay] = useState("")
  const fileRef = useRef<HTMLInputElement>(null)
  const photoTarget = useRef<string | null>(null)

  const startRename = (acc: AccountSummary) => {
    setRenaming(acc.account)
    setDisplay(acc.displayName || acc.account || "")
  }

  const confirmRename = async () => {
    if (renaming) {
      await onRename(renaming, display.trim())
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
          <CardTitle className="text-xl">Graal Remote Control (v{version})</CardTitle>
          <CardDescription>{t("login.selectAccount")}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <input ref={fileRef} type="file" accept="image/*" className="hidden" onChange={onFile} />

          <div className="grid gap-2">
            <Label htmlFor="session-nickname">{t("login.sessionNickname")}</Label>
            <Input
              id="session-nickname"
              placeholder={t("login.nicknamePlaceholder")}
              required
              autoComplete="nickname"
              value={nickname}
              onChange={(e) => onNicknameChange(e.target.value)}
            />
            <div className="text-muted-foreground flex items-start gap-2 text-xs">
              <Info className="mt-0.5 size-3.5 shrink-0" />
              <span>{t("login.nicknameHelp")}</span>
            </div>
          </div>

          {loading ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="animate-spin" />
            </div>
          ) : accounts.length === 0 ? (
            <div className="flex flex-col items-center gap-3 py-6 text-center">
              <UserRound className="text-muted-foreground" />
              <p className="text-sm text-muted-foreground">{t("login.noAccounts")}</p>
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
                      onClick={() => onSelect(account.account, nickname)}
                      className="flex flex-1 flex-col items-start gap-0.5 text-left disabled:opacity-50"
                    >
                      <span className="flex items-center gap-1.5">
                        <span className="text-sm font-medium">
                          {account.displayName || account.account}
                        </span>
                      </span>
                      <span className="text-xs text-muted-foreground">{account.account}</span>
                    </button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-muted-foreground"
                      disabled={busy}
                      aria-label={t("login.photoFor", {name: account.account})}
                      onClick={() => pickPhoto(account)}
                    >
                      <ImagePlus />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-muted-foreground"
                      disabled={busy}
                      aria-label={t("login.renameFor", {name: account.account})}
                      onClick={() => startRename(account)}
                    >
                      <Pencil />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-muted-foreground"
                      disabled={busy}
                      aria-label={t("login.removeFor", {name: account.account})}
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
            {t("login.add")}
          </Button>
        </CardContent>
      </Card>

      <AlertDialog open={renaming !== null} onOpenChange={(v) => !v && setRenaming(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("login.rename")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("login.renameDescription")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="display-name">{t("login.displayName")}</Label>
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

          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setRenaming(null)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={confirmRename}>{t("common.save")}</Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
