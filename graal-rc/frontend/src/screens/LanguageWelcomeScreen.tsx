import {useState} from "react"
import {Globe2, Languages, ShieldCheck, Sparkles} from "lucide-react"

import type {Language} from "@/hooks/useLanguage"
import {Button} from "@/components/ui/button"

const copy: Record<Language, {eyebrow: string; title: string; body: string; continue: string; saved: string}> = {
  en: {
    eyebrow: "Welcome to GoRC",
    title: "Your control room, your language.",
    body: "GoRC brings server chat, scripts, players and files into one focused workspace. Choose a language to get started.",
    continue: "Continue in English",
    saved: "Your choice is saved on this computer and can be changed later in Settings.",
  },
  "pt-BR": {
    eyebrow: "Bem-vindo ao GoRC",
    title: "Sua central de controle, no seu idioma.",
    body: "O GoRC reúne chat, scripts, jogadores e arquivos do servidor em um só lugar. Escolha um idioma para começar.",
    continue: "Continuar em português",
    saved: "Sua escolha será salva neste computador e poderá ser alterada depois em Configurações.",
  },
  es: {
    eyebrow: "Bienvenido a GoRC",
    title: "Tu centro de control, en tu idioma.",
    body: "GoRC reúne el chat, scripts, jugadores y archivos del servidor en un solo espacio. Elige un idioma para empezar.",
    continue: "Continuar en español",
    saved: "Tu elección se guardará en este equipo y podrás cambiarla después en Configuración.",
  },
}

export function LanguageWelcomeScreen({language, onConfirm}: {language: Language; onConfirm: (language: Language) => void}) {
  const [selectedLanguage, setSelectedLanguage] = useState(language)
  const text = copy[selectedLanguage]
  return (
    <main className="bg-background flex min-h-svh items-center justify-center overflow-auto p-6">
      <div className="w-full max-w-2xl">
        <div className="mb-8 flex items-center gap-3 text-sm font-semibold tracking-tight">
          <div className="bg-primary/12 text-primary flex size-9 items-center justify-center rounded-lg"><Globe2 className="size-5" /></div>
          GoRC
        </div>
        <section className="border-border bg-card/40 overflow-hidden rounded-2xl border shadow-sm">
          <div className="p-7 sm:p-10">
            <div className="text-primary mb-4 flex items-center gap-2 text-xs font-semibold tracking-wide"><Sparkles className="size-3.5" />{text.eyebrow}</div>
            <h1 className="max-w-xl text-3xl font-semibold tracking-tight text-balance sm:text-4xl">{text.title}</h1>
            <p className="text-muted-foreground mt-4 max-w-xl text-sm leading-6">{text.body}</p>
            <div className="mt-8 grid gap-3 sm:grid-cols-3">
              {(["pt-BR", "en", "es"] as Language[]).map((option) => {
                const selected = selectedLanguage === option
                return (
                  <button key={option} type="button" onClick={() => setSelectedLanguage(option)} className={`group rounded-xl border p-4 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${selected ? "border-primary bg-primary/10" : "border-border hover:bg-accent"}`}>
                    <div className="mb-5 flex items-center justify-between"><Languages className={`size-4 ${selected ? "text-primary" : "text-muted-foreground"}`} />{selected && <ShieldCheck className="text-primary size-4" />}</div>
                    <p className="text-sm font-semibold">{option === "pt-BR" ? "Português" : option === "en" ? "English" : "Español"}</p>
                    <p className="text-muted-foreground mt-1 text-xs">{option === "pt-BR" ? "Brasil" : option === "en" ? "United States" : "España"}</p>
                  </button>
                )
              })}
            </div>
            <Button className="mt-6 w-full" onClick={() => onConfirm(selectedLanguage)}>{text.continue}</Button>
          </div>
          <div className="bg-muted/35 border-t px-7 py-3 text-xs sm:px-10">{text.saved}</div>
        </section>
      </div>
    </main>
  )
}
