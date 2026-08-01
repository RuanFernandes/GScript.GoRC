// RcSidebar — expandable icon rail replacing the second top action bar.
// Collapsed (default): icons only, ~48px wide. Expands on hover to show labels.
// Pure CSS animation (width + opacity) — no JS animation lib, light and fast.
// Labels fade in when the rail expands; the rail stays overflow-visible so
// anchored popovers are not clipped.
import type {ComponentType} from "react"
import {Code2, Flag, FolderOpen, FolderTree, SlidersHorizontal, Users} from "lucide-react"

import {SyncPopover} from "@/components/features/sync/SyncPopover"
import {rcService} from "@/services/rcService"

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
  return (
    <aside className="group flex w-12 shrink-0 flex-col overflow-visible border-r transition-[width] duration-150 ease-out hover:w-56">
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
        <RailItem icon={Code2} label="Scripts" onClick={() => rcService.openScriptManager()} />
        <SyncPopover rail />
        <RailItem icon={FolderOpen} label="Files" onClick={() => rcService.openFileBrowser()} />
        <RailItem icon={Users} label="Players" onClick={() => rcService.openPlayerList()} />
      </nav>
      <div className="h-px bg-border" />

      <nav className="flex flex-col py-1">
        <RailItem
          icon={SlidersHorizontal}
          label="Server Options"
          onClick={() => openServerText("options", "Server Options")}
        />
        <RailItem
          icon={FolderTree}
          label="Folder Config"
          onClick={() => openServerText("folder_config", "Folder Config")}
        />
        <RailItem
          icon={Flag}
          label="Server Flags"
          onClick={() => openServerText("flags", "Server Flags")}
        />
      </nav>
    </aside>
  )
}
