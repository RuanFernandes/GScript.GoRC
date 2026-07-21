import {useEffect} from "react"
import {Toaster as Sonner, type ToasterProps} from "sonner"

const Toaster = ({...props}: ToasterProps) => {
  useEffect(() => {
    document.documentElement.classList.add("dark")
  }, [])

  return (
    <Sonner
      theme="dark"
      className="toaster group"
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
        } as React.CSSProperties
      }
      {...props}
    />
  )
}

export {Toaster}
