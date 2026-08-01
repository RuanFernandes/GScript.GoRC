// ChatSettingsFields: the editable chat-color + logging controls, shared by the
// ChatSettingsDialog (RcScreen header) and the Settings window's Chat section.
// Driven by useChatSettings (persisted to localStorage). No action buttons — the
// caller renders Reset/Done as fits its container.
import {Input} from "@/components/ui/input"
import {Button} from "@/components/ui/button"
import type {ChatSettings} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

interface ChatSettingsFieldsProps {
  settings: ChatSettings
  onChange: (patch: Partial<ChatSettings>) => void
  onBrowse: () => void | Promise<void>
  onBrowsePm?: () => void | Promise<void>
}

const FIELDS: {key: keyof ChatSettings; labelKey: string}[] = [
  {key: "timestamp", labelKey: "settings.timestamp"},
  {key: "rcPrefix", labelKey: "settings.rcPrefix"},
  {key: "ncPrefix", labelKey: "settings.ncPrefix"},
  {key: "ircPrefix", labelKey: "settings.ircPrefix"},
  {key: "speaker", labelKey: "settings.speaker"},
  {key: "content", labelKey: "settings.message"},
]

export function ChatSettingsFields({settings, onChange, onBrowse, onBrowsePm}: ChatSettingsFieldsProps) {
  const {t} = useLanguage()

  return (
    <div className="grid gap-4">
      <div className="grid gap-3">
        {FIELDS.map((f) => (
          <div key={f.key} className="flex items-center justify-between gap-4">
            <label htmlFor={`color-${f.key}`} className="text-sm">{t(f.labelKey)}</label>
            <ColorControl id={`color-${f.key}`} value={settings[f.key] as string} onChange={(value) => onChange({[f.key]: value})} />
          </div>
        ))}
      </div>

      <div className="border-t pt-4">
        <p className="text-sm font-medium">{t("settings.logging")}</p>
        <p className="text-muted-foreground mb-3 mt-1 text-xs">{t("settings.logChat")}</p>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={settings.logChat}
            onChange={(e) => onChange({logChat: e.target.checked})}
          />
          {t("settings.logChat")}
        </label>
        <div className="mt-2 flex items-center gap-2">
          <Input
            value={settings.logDir}
            onChange={(e) => onChange({logDir: e.target.value})}
            placeholder={t("settings.notSet")}
            readOnly
          />
            <Button variant="outline" onClick={() => onBrowse()}>
            {t("settings.browse")}
          </Button>
        </div>
      </div>

      <div className="border-t pt-3">
        <p className="text-sm font-medium">{t("settings.logPms")}</p>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={settings.pmLog}
            onChange={(e) => onChange({pmLog: e.target.checked})}
          />
          {t("settings.logPms")}
        </label>
        <div className="mt-2 flex items-center gap-2">
          <Input
            value={settings.pmLogDir}
            onChange={(e) => onChange({pmLogDir: e.target.value})}
            placeholder={t("settings.notSet")}
            readOnly
          />
            <Button variant="outline" onClick={() => onBrowsePm?.()}>
            {t("settings.browse")}
          </Button>
        </div>
      </div>
    </div>
  )
}

function ColorControl({id, value, onChange}: {id: string; value: string; onChange: (value: string) => void}) {
  const normalized = /^#[0-9a-f]{6}$/i.test(value) ? value : "#ffffff"
  return (
    <div className="flex items-center gap-2 rounded-md border border-input bg-input/20 p-1">
      <input id={id} type="color" value={normalized} onChange={(event) => onChange(event.target.value)} className="size-8 cursor-pointer rounded border-0 bg-transparent p-0" aria-label="Choose color" />
      <input type="text" value={value} onChange={(event) => { if (/^#[0-9a-f]{0,6}$/i.test(event.target.value)) onChange(event.target.value) }} className="h-8 w-20 bg-transparent px-1 font-mono text-xs uppercase outline-none" aria-label={`${id} hex value`} />
    </div>
  )
}
