import {useMemo} from "react"

import {useLanguage} from "@/hooks/useLanguage"

function readLink(): string | null {
  if (typeof window === "undefined") return null
  const hash = window.location.hash
  const queryStart = hash.indexOf("?")
  if (queryStart < 0) return null

  const raw = new URLSearchParams(hash.slice(queryStart + 1)).get("u")
  if (!raw) return null

  try {
    const parsed = new URL(raw)
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return null
    return parsed.toString()
  } catch {
    return null
  }
}

export function ChatLinkWindowScreen() {
  const {t} = useLanguage()
  const link = useMemo(readLink, [])

  if (!link) {
    return (
      <div className="bg-background text-muted-foreground flex h-full items-center justify-center p-6 text-sm">
        {t("chat.linkOpenFailed")}
      </div>
    )
  }

  return (
    <iframe
      className="block h-full min-h-0 w-full border-0 bg-white"
      src={link}
      title={t("window.chatLink")}
    />
  )
}
