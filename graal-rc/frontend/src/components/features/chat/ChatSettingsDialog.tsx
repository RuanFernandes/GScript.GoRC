// ChatSettingsDialog: color pickers for the chat line segments. Driven by
// useChatSettings (persisted to localStorage). Reuses the AlertDialog primitive
// with fully-controlled open state.
import {useEffect} from "react"

import {AlertDialogContent, AlertDialogDescription, AlertDialogHeader, AlertDialogTitle, AlertDialog} from "@/components/ui/alert-dialog"
import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {rcService} from "@/services/rcService"
import type {ChatSettings} from "@/types"

interface ChatSettingsDialogProps {
  open: boolean
  settings: ChatSettings
  onChange: (patch: Partial<ChatSettings>) => void
  onReset: () => void
  onClose: () => void
}

const FIELDS: {key: keyof ChatSettings; label: string}[] = [
  {key: "timestamp", label: "Timestamp [HH:MM]"},
  {key: "rcPrefix", label: "[RC] prefix"},
  {key: "ncPrefix", label: "[NC] prefix"},
  {key: "ircPrefix", label: "[IRC] prefix"},
  {key: "speaker", label: "Speaker (before :)"},
  {key: "content", label: "Message content"},
]

export function ChatSettingsDialog({
  open,
  settings,
  onChange,
  onReset,
  onClose,
}: ChatSettingsDialogProps) {
  // Color inputs should be keyboard-accessible; keep them simple native pickers.
  useEffect(() => {
    // no-op; placeholder for future autofocus logic
  }, [open])

  return (
    <AlertDialog open={open} onOpenChange={(next) => !next && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Chat colors</AlertDialogTitle>
          <AlertDialogDescription>
            Customize the colors used for each part of a chat line.
          </AlertDialogDescription>
        </AlertDialogHeader>
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

        <div className="mt-2 border-t pt-3">
          <p className="text-muted-foreground mb-2 text-xs">
            Logging writes every chat line (append) to{" "}
            <code>rclog_MM_DD_YYYY.txt</code> in the chosen folder.
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
            <Button
              variant="outline"
              onClick={async () => {
                const dir = await rcService.chooseDirectory()
                if (dir) onChange({logDir: dir})
              }}
            >
              Browse
            </Button>
          </div>
        </div>

        <div className="flex justify-between gap-2">
          <Button variant="ghost" onClick={onReset}>
            Reset defaults
          </Button>
          <Button onClick={onClose}>Done</Button>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  )
}
