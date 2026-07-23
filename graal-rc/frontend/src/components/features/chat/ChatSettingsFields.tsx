// ChatSettingsFields: the editable chat-color + logging controls, shared by the
// ChatSettingsDialog (RcScreen header) and the Settings window's Chat section.
// Driven by useChatSettings (persisted to localStorage). No action buttons — the
// caller renders Reset/Done as fits its container.
import {Input} from "@/components/ui/input"
import {Button} from "@/components/ui/button"
import type {ChatSettings} from "@/types"

interface ChatSettingsFieldsProps {
  settings: ChatSettings
  onChange: (patch: Partial<ChatSettings>) => void
  onBrowse: () => void | Promise<void>
  onBrowsePm?: () => void | Promise<void>
}

const FIELDS: {key: keyof ChatSettings; label: string}[] = [
  {key: "timestamp", label: "Timestamp [HH:MM]"},
  {key: "rcPrefix", label: "[RC] prefix"},
  {key: "ncPrefix", label: "[NC] prefix"},
  {key: "ircPrefix", label: "[IRC] prefix"},
  {key: "speaker", label: "Speaker (before :)"},
  {key: "content", label: "Message content"},
]

export function ChatSettingsFields({settings, onChange, onBrowse, onBrowsePm}: ChatSettingsFieldsProps) {
  return (
    <div className="grid gap-4">
      <div className="grid gap-3">
        {FIELDS.map((f) => (
          <div key={f.key} className="flex items-center justify-between gap-3">
            <label htmlFor={`color-${f.key}`} className="text-sm">
              {f.label}
            </label>
            <input
              id={`color-${f.key}`}
              type="color"
              value={settings[f.key] as string}
              onChange={(e) => onChange({[f.key]: e.target.value})}
              className="h-8 w-12 cursor-pointer rounded border bg-transparent"
            />
          </div>
        ))}
      </div>

      <div className="border-t pt-3">
        <p className="text-muted-foreground mb-2 text-xs">
          Logging writes every chat line (append) to <code>rclog_MM_DD_YYYY.txt</code> in the chosen folder.
        </p>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={settings.logChat}
            onChange={(e) => onChange({logChat: e.target.checked})}
          />
          Log Chat
        </label>
        <div className="mt-2 flex items-center gap-2">
          <Input
            value={settings.logDir}
            onChange={(e) => onChange({logDir: e.target.value})}
            placeholder="Select output folder…"
            readOnly
          />
          <Button variant="outline" onClick={() => onBrowse()}>
            Browse
          </Button>
        </div>
      </div>

      <div className="border-t pt-3">
        <p className="text-muted-foreground mb-2 text-xs">
          PM logging writes each PM (in/out) to <code>{"{folder}/{server}/PM_{account}_Log.txt"}</code>.
        </p>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={settings.pmLog}
            onChange={(e) => onChange({pmLog: e.target.checked})}
          />
          Log PMs
        </label>
        <div className="mt-2 flex items-center gap-2">
          <Input
            value={settings.pmLogDir}
            onChange={(e) => onChange({pmLogDir: e.target.value})}
            placeholder="Select PM log folder…"
            readOnly
          />
          <Button variant="outline" onClick={() => onBrowsePm?.()}>
            Browse
          </Button>
        </div>
      </div>
    </div>
  )
}
