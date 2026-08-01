// RcSidebar — expandable icon rail replacing the second top action bar.
// Collapsed (default): icons only, ~48px wide. Expands on hover to show labels.
// Pure CSS animation (width + opacity) — no JS animation lib, light and fast.
// Labels fade in when the rail expands; the rail stays overflow-visible so
// anchored popovers are not clipped.
import {useState, type ComponentType} from "react"
import {Code2, Flag, FolderOpen, FolderTree, Pin, PinOff, SlidersHorizontal, Users} from "lucide-react"

import {SyncPopover} from "@/components/features/sync/SyncPopover"
import {rcService} from "@/services/rcService"
import {useLanguage} from "@/hooks/useLanguage"

export interface RailItemProps {
  icon: ComponentType<{className?: string}>
  label: string
  onClick: () => void
  badge?: React.ReactNode
}

// RailItem is one sidebar row. The label is always rendered but hidden by
// opacity when collapsed; it fades in as the rail expands.
export function RailItem({icon: Icon, label, onClick, badge}: RailItemProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={label}
      aria-label={label}
      className="flex h-11 w-full items-center gap-3 px-3 text-sm font-medium transition-colors hover:bg-accent focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring active:bg-accent"
    >
      <Icon className="size-4 shrink-0" />
      <span className="sidebar-label flex-1 whitespace-nowrap text-left">{label}</span>
      {badge}
    </button>
  )
}

export interface RcSidebarProps {
  ncLabel: string
  ncConnected: boolean
  openServerText: (kind: "options" | "folder_config" | "flags", title: string) => void
}

export function RcSidebar({ncLabel: nc, ncConnected, openServerText}: RcSidebarProps) {
  const {t} = useLanguage()
  const [pinned, setPinned] = useState(() => {
    if (typeof window === "undefined") return false
    return window.localStorage.getItem("graal-rc:sidebarPinned") === "true"
  })

  const togglePinned = () => {
    setPinned((current) => {
      const next = !current
      window.localStorage.setItem("graal-rc:sidebarPinned", String(next))
      return next
    })
  }

  return (
    <aside className={`group flex shrink-0 flex-col overflow-visible border-r transition-[width] duration-150 ease-out ${pinned ? "sidebar-pinned w-56" : "w-12 hover:w-56"}`}>
      {/* NC status chip at the top */}
      <div className="flex h-11 items-center gap-3 px-3">
        <span
          title={nc || "NC status"}
          aria-label={nc || "NC status"}
          className={`size-2.5 shrink-0 rounded-full ${
            ncConnected ? "bg-emerald-500" : "bg-muted-foreground/40"
          }`}
        />
        <span className="sidebar-label text-muted-foreground truncate text-xs font-medium tracking-wide">
          {nc}
        </span>
      </div>
      <div className="h-px bg-border" />

      <nav className="flex flex-col py-1">
        <RailItem icon={Code2} label={t("sidebar.scripts")} onClick={() => rcService.openScriptManager()} />
        <SyncPopover rail />
        <RailItem icon={FolderOpen} label={t("sidebar.files")} onClick={() => rcService.openFileBrowser()} />
        <RailItem icon={Users} label={t("sidebar.players")} onClick={() => rcService.openPlayerList()} />
      </nav>
      <div className="h-px bg-border" />

      <nav className="flex flex-col py-1">
        <RailItem
          icon={SlidersHorizontal}
          label={t("sidebar.serverOptions")}
          onClick={() => openServerText("options", t("sidebar.serverOptions"))}
        />
        <RailItem
          icon={FolderTree}
          label={t("sidebar.folderConfig")}
          onClick={() => openServerText("folder_config", t("sidebar.folderConfig"))}
        />
        <RailItem
          icon={Flag}
          label={t("sidebar.serverFlags")}
          onClick={() => openServerText("flags", t("sidebar.serverFlags"))}
        />
      </nav>
      <div className="mt-auto border-t border-border pt-1">
        <button
          type="button"
          onClick={togglePinned}
          aria-pressed={pinned}
          title={pinned ? t("sidebar.unpin") : t("sidebar.pin")}
          aria-label={pinned ? t("sidebar.unpin") : t("sidebar.pin")}
          className="flex h-11 w-full items-center gap-3 px-3 text-sm font-medium transition-colors hover:bg-accent focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring active:bg-accent"
        >
          {pinned ? <PinOff className="size-4 shrink-0" /> : <Pin className="size-4 shrink-0" />}
          <span className="sidebar-label flex-1 whitespace-nowrap text-left">{pinned ? t("sidebar.unpin") : t("sidebar.pin")}</span>
        </button>
      </div>
    </aside>
  )
}
