import {CheckCheck, CircleAlert, Info, OctagonAlert, Trash2, X} from "lucide-react"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import type {OperationalNotification, OperationalNotificationLevel} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

function levelIcon(level: OperationalNotificationLevel) {
  if (level === "error") return <OctagonAlert className="text-destructive size-4 shrink-0" />
  if (level === "warning") return <CircleAlert className="size-4 shrink-0 text-amber-500" />
  if (level === "success") return <CheckCheck className="size-4 shrink-0 text-emerald-500" />
  return <Info className="text-primary size-4 shrink-0" />
}

function formatTime(timestamp: number): string {
  return new Date(timestamp).toLocaleTimeString([], {hour: "2-digit", minute: "2-digit"})
}

interface NotificationCenterPopoverProps {
  open: boolean
  notifications: OperationalNotification[]
  unreadCount: number
  onClose: () => void
  onMarkRead: (id: string) => void
  onMarkAllRead: () => void
  onClear: () => void
}

export function NotificationCenterPopover({open, notifications, unreadCount, onClose, onMarkRead, onMarkAllRead, onClear}: NotificationCenterPopoverProps) {
  const {t} = useLanguage()
  if (!open) return null

  return (
    <div className="bg-popover text-popover-foreground absolute top-full right-0 z-50 mt-2 flex w-[min(22rem,calc(100vw-2rem))] flex-col overflow-hidden rounded-lg border shadow-xl" role="dialog" aria-label={t("notifications.title")}>
      <div className="flex items-center gap-2 border-b px-3 py-2.5">
        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold">{t("notifications.title")}</p>
          <p className="text-muted-foreground text-[11px]">{t("notifications.subtitle")}</p>
        </div>
        {unreadCount > 0 && <Badge variant="secondary">{unreadCount}</Badge>}
        <Button variant="ghost" size="icon" className="size-7" onClick={onClose} aria-label={t("common.close")}><X className="size-4" /></Button>
      </div>
      <div className="flex items-center justify-between gap-2 border-b px-3 py-1.5">
        <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={onMarkAllRead} disabled={unreadCount === 0}>{t("notifications.markAllRead")}</Button>
        <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={onClear} disabled={notifications.length === 0}><Trash2 className="size-3.5" />{t("notifications.clear")}</Button>
      </div>
      <div className="max-h-[min(60vh,26rem)] overflow-y-auto">
        {notifications.length === 0 ? (
          <div className="text-muted-foreground flex min-h-28 items-center justify-center px-4 text-center text-xs">{t("notifications.empty")}</div>
        ) : (
          <div className="divide-y">
            {notifications.map((notification) => (
              <button
                key={notification.id}
                type="button"
                onClick={() => onMarkRead(notification.id)}
                className={`flex w-full gap-2.5 px-3 py-2.5 text-left transition-colors hover:bg-accent/60 ${notification.read ? "opacity-65" : "bg-accent/20"}`}
              >
                {levelIcon(notification.level)}
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-2">
                    <span className="truncate text-xs font-medium">{notification.title}</span>
                    <time className="text-muted-foreground ml-auto shrink-0 text-[10px]">{formatTime(notification.timestamp)}</time>
                  </span>
                  <span className="text-muted-foreground mt-0.5 block line-clamp-2 text-[11px]">{notification.message}</span>
                </span>
                {!notification.read && <span className="bg-primary mt-1.5 size-1.5 shrink-0 rounded-full" aria-label={t("notifications.unread")} />}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
