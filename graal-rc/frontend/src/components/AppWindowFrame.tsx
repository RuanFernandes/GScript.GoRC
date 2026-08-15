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
        className="bg-card/30 flex h-12 items-center border-b pl-4 select-none"
        style={{"--wails-draggable": "drag"} as React.CSSProperties}
        onDoubleClick={toggleMaximise}
      >
        <div className="min-w-0">
          <h1 className="truncate text-sm font-semibold tracking-tight">{title}</h1>
        </div>
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
      </header>
      <div className="app-window-content h-[calc(100%_-_3rem)] min-h-0 overflow-hidden">{children}</div>
    </div>
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
