import {useEffect, useState} from "react"
import {Copy, Minus, Square, X} from "lucide-react"
import {Window} from "@wailsio/runtime"
import {useLanguage} from "@/hooks/useLanguage"

interface AppWindowFrameProps {
  title: string
  children: React.ReactNode
}

const noDragStyle = {"--wails-draggable": "no-drag"} as React.CSSProperties

export function AppWindowFrame({title, children}: AppWindowFrameProps) {
  const {t} = useLanguage()
  const [maximised, setMaximised] = useState(false)
  const macOS = isMacOS()

  useEffect(() => {
    Window.IsMaximised().then(setMaximised).catch(() => {})
  }, [])

  const toggleMaximise = async () => {
    await Window.ToggleMaximise()
    setMaximised((value) => !value)
  }

  return (
    <div className="app-window-shell bg-background h-svh overflow-hidden">
      <header
        className={`bg-card/30 relative flex h-12 items-center border-b select-none ${macOS ? "px-3" : "pl-4"}`}
        style={{"--wails-draggable": "drag"} as React.CSSProperties}
        onDoubleClick={toggleMaximise}
      >
        {macOS && (
          <MacWindowControls
            maximised={maximised}
            onToggleMaximise={toggleMaximise}
          />
        )}
        <div className={macOS ? "absolute left-1/2 min-w-0 max-w-[50%] -translate-x-1/2" : "min-w-0"}>
          <h1 className="truncate text-sm font-semibold tracking-tight">{title}</h1>
        </div>
        {!macOS && (
          <div className="ml-auto flex h-full items-stretch" style={noDragStyle}>
            <WindowButton label={t("window.minimize")} style={noDragStyle} onClick={() => Window.Minimise()}>
              <Minus className="size-4" />
            </WindowButton>
            <WindowButton
              label={maximised ? t("window.restore") : t("window.maximize")}
              style={noDragStyle}
              onClick={toggleMaximise}
            >
              {maximised ? <Copy className="size-3.5" /> : <Square className="size-3.5" />}
            </WindowButton>
            <WindowButton label={t("window.close")} style={noDragStyle} onClick={() => Window.Close()} destructive>
              <X className="size-4" />
            </WindowButton>
          </div>
        )}
      </header>
      <div className="app-window-content h-[calc(100%_-_3rem)] min-h-0 overflow-hidden">{children}</div>
    </div>
  )
}

function isMacOS(): boolean {
  if (typeof navigator === "undefined") return false
  return /Mac|iPhone|iPad|iPod/i.test(`${navigator.platform} ${navigator.userAgent}`)
}

function MacWindowControls({
  maximised,
  onToggleMaximise,
}: {
  maximised: boolean
  onToggleMaximise: () => void | Promise<void>
}) {
  const {t} = useLanguage()

  return (
    <div className="flex items-center gap-2" style={noDragStyle}>
      <MacWindowButton label={t("window.close")} tone="close" onClick={() => Window.Close()}>
        <X className="size-2.5" />
      </MacWindowButton>
      <MacWindowButton label={t("window.minimize")} tone="minimise" onClick={() => Window.Minimise()}>
        <Minus className="size-2.5" />
      </MacWindowButton>
      <MacWindowButton
        label={maximised ? t("window.restore") : t("window.maximize")}
        tone="maximise"
        onClick={onToggleMaximise}
      >
        {maximised ? <Copy className="size-2.5" /> : <Square className="size-2.5" />}
      </MacWindowButton>
    </div>
  )
}

function MacWindowButton({
  label,
  tone,
  onClick,
  children,
}: {
  label: string
  tone: "close" | "minimise" | "maximise"
  onClick: () => void | Promise<void>
  children: React.ReactNode
}) {
  const {t} = useLanguage()
  const toneClass = {
    close: "bg-[#ff5f57]",
    minimise: "bg-[#febc2e]",
    maximise: "bg-[#28c840]",
  }[tone]

  return (
    <button
      type="button"
      aria-label={t("window.buttonLabel", {label})}
      title={label}
      style={noDragStyle}
      onClick={onClick}
      className="group relative flex size-4 items-center justify-center rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1"
    >
      <span aria-hidden="true" className={`absolute size-3 rounded-full shadow-sm ring-1 ring-black/15 ${toneClass}`} />
      <span
        aria-hidden="true"
        className="relative z-10 text-black/65 opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100"
      >
        {children}
      </span>
    </button>
  )
}

function WindowButton({
  label,
  style,
  onClick,
  destructive = false,
  children,
}: {
  label: string
  style: React.CSSProperties
  onClick: () => void | Promise<void>
  destructive?: boolean
  children: React.ReactNode
}) {
  const {t} = useLanguage()

  return (
    <button
      type="button"
      aria-label={t("window.buttonLabel", {label})}
      title={label}
      style={style}
      onClick={onClick}
      className={
        destructive
          ? "text-muted-foreground hover:bg-destructive hover:text-destructive-foreground flex w-11 items-center justify-center transition-colors"
          : "text-muted-foreground hover:bg-accent hover:text-foreground flex w-11 items-center justify-center transition-colors"
      }
    >
      {children}
    </button>
  )
}
