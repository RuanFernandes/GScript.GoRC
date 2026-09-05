import {useEffect, useState} from "react"
import {EditorDraftController} from "@/lib/editorDrafts"
import {rcService} from "@/services/rcService"

export function useEditorDraft() {
  const [draftError, setDraftError] = useState<string | null>(null)
  const [draft] = useState(() => {
    const query = window.location.hash.split("?")[1] ?? ""
    const params = new URLSearchParams(query)
    return new EditorDraftController({
      token: params.get("d") ?? "",
      key: params.get("dk") ?? "",
      backend: rcService,
      storage: {
        getItem: (key) => window.localStorage.getItem(key),
        setItem: (key, value) => window.localStorage.setItem(key, value),
      },
      onError: setDraftError,
    })
  })

  useEffect(() => {
    const flush = () => { void draft.flush().catch(() => {}) }
    const visibility = () => { if (document.visibilityState === "hidden") flush() }
    window.addEventListener("pagehide", flush)
    document.addEventListener("visibilitychange", visibility)
    return () => {
      window.removeEventListener("pagehide", flush)
      document.removeEventListener("visibilitychange", visibility)
      flush()
    }
  }, [draft])
  return {draft, draftError}
}
