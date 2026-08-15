import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import {useLanguage} from "@/hooks/useLanguage"

interface ScriptSyncRequiredDialogProps {
  open: boolean
  onClose: () => void
  onConfirmDisable?: () => void
}

export function ScriptSyncRequiredDialog({open, onClose, onConfirmDisable}: ScriptSyncRequiredDialogProps) {
  const {t} = useLanguage()
  const isDisableConfirmation = Boolean(onConfirmDisable)

  return (
    <AlertDialog open={open} onOpenChange={(next) => !next && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(isDisableConfirmation ? "sync.disableTitle" : "sync.requiredTitle")}</AlertDialogTitle>
          <AlertDialogDescription>{t(isDisableConfirmation ? "sync.disableDescription" : "sync.requiredDescription")}</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="flex justify-end">
          {isDisableConfirmation ? (
            <div className="flex gap-2">
              <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
              <AlertDialogAction onClick={onConfirmDisable}>{t("sync.disableConfirm")}</AlertDialogAction>
            </div>
          ) : (
            <AlertDialogAction>{t("common.ok")}</AlertDialogAction>
          )}
        </div>
      </AlertDialogContent>
    </AlertDialog>
  )
}
