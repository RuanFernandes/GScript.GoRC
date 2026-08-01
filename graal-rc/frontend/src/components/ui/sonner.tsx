import {useEffect} from "react"
import {Toaster as Sonner, type ToasterProps} from "sonner"

const Toaster = ({...props}: ToasterProps) => {
  useEffect(() => {
    document.documentElement.classList.add("dark")
  }, [])

  return (
    <Sonner
      theme="dark"
      richColors
      className="toaster group"
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
          "--error-bg": "var(--destructive)",
          "--error-text": "var(--destructive-foreground)",
          "--error-border": "var(--destructive)",
        } as React.CSSProperties
      }
      {...props}
    />
  )
}

export {Toaster}
