import {useCallback, useEffect, useMemo, useState} from "react"
import {Events} from "@wailsio/runtime"

import {rcService} from "@/services/rcService"
import en from "@/locales/en.json"
import ptBR from "@/locales/pt-BR.json"
import es from "@/locales/es.json"

export type Language = "pt-BR" | "en" | "es"
type Dictionary = Record<string, string>

const STORAGE_FALLBACK = "graal-rc:language"
const dictionaries: Record<Language, Dictionary> = {en, "pt-BR": ptBR, es}

function interpolate(value: string, vars: Record<string, string | number>): string {
  return value.replace(/\{(\w+)\}/g, (_, key: string) => String(vars[key] ?? `{${key}}`))
}

export function useLanguage() {
  const [language, setLanguageState] = useState<Language>(() => {
    if (typeof window === "undefined") return "en"
    const fallback = window.localStorage.getItem(STORAGE_FALLBACK)
    return fallback === "pt-BR" || fallback === "es" ? fallback : "en"
  })
  const [configured, setConfigured] = useState(false)

  useEffect(() => {
    let cancelled = false
    rcService.getLanguage().then((value) => {
      if (!cancelled && (value === "pt-BR" || value === "en" || value === "es")) {
        setLanguageState(value)
        setConfigured(true)
      }
    }).catch(() => {})
    const off = Events.On("rc:language", (event: {data: string}) => {
      const value = event.data as Language
      if (value === "pt-BR" || value === "en" || value === "es") {
        setLanguageState(value)
        setConfigured(true)
      }
    })
    return () => { cancelled = true; off() }
  }, [])

  const setLanguage = useCallback((value: Language) => {
    setLanguageState(value)
    setConfigured(true)
    if (typeof window !== "undefined") window.localStorage.setItem(STORAGE_FALLBACK, value)
    rcService.setLanguage(value).catch(() => {})
  }, [])

  const t = useMemo(() => (key: string, vars: Record<string, string | number> = {}) => {
    const value = dictionaries[language][key] ?? dictionaries.en[key] ?? key
    return interpolate(value, vars)
  }, [language])

  return {language, setLanguage, configured, needsLanguage: !configured, t}
}
