import {BookOpen, FolderOpen, Puzzle, Wrench} from "lucide-react"

import {Button} from "@/components/ui/button"
import {PluginDocumentation} from "@/screens/SettingsWindowScreen"
import {useLanguage} from "@/hooks/useLanguage"
import {rcService} from "@/services/rcService"

export function PluginDocumentationWindowScreen() {
  const {language, t} = useLanguage()

  return (
    <div className="bg-background flex h-full min-h-0 flex-col">
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b px-5 py-4 sm:px-7">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-primary/10 text-primary">
            <BookOpen className="size-4" />
          </div>
          <div className="min-w-0">
            <h1 className="truncate text-lg font-semibold">{t("settings.pluginDocumentation")}</h1>
            <p className="text-muted-foreground truncate text-xs">{t("settings.pluginDocsSubtitle")}</p>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => void rcService.openPluginManager()}>
            <Wrench />{t("settings.openPluginManager")}
          </Button>
          <Button variant="outline" size="sm" onClick={() => void rcService.openPluginsFolder()}>
            <FolderOpen />{t("settings.openPluginsFolder")}
          </Button>
        </div>
      </header>
      <main data-plugin-doc-scroll className="min-h-0 flex-1 overflow-y-auto px-5 py-6 sm:px-8 sm:py-8">
        <div className="mx-auto max-w-5xl">
          <div className="mb-6 flex items-start gap-3 border-b pb-5">
            <Puzzle className="mt-0.5 size-5 shrink-0 text-primary" />
            <div>
              <h2 className="text-base font-semibold">{t("settings.pluginDocsTitle")}</h2>
              <p className="text-muted-foreground mt-1 max-w-3xl text-sm leading-6">{t("settings.pluginDocsDescription")}</p>
            </div>
          </div>
          <PluginDocumentation language={language} />
        </div>
      </main>
    </div>
  )
}
