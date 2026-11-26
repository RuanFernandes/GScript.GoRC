import {useEffect, useRef} from "react"

import {useLanguage} from "@/hooks/useLanguage"
import {highlightParts, type CompletionItem} from "@/lib/chatCompletion"

export const completionOptionId = (listId: string, index: number) => `${listId}-${index}`

function Highlighted({text, matches}: {text: string; matches: number[]}) {
  return (
    <>
      {highlightParts(text, matches).map((part, index) => part.match
        ? <span key={index} className="font-bold text-sky-600 dark:text-sky-400">{part.text}</span>
        : <span key={index}>{part.text}</span>)}
    </>
  )
}

// ChatCompletionList is the suggestion list above the chat input. Focus stays
// in the input; the keyboard is handled there.
export function ChatCompletionList({id, items, selected, onAccept}: {
  id: string
  items: CompletionItem[]
  selected: number
  onAccept: (item: CompletionItem) => void
}) {
  const {t} = useLanguage()
  const selectedRef = useRef<HTMLButtonElement>(null)
  const playerPhase = items[0]?.kind === "player"

  useEffect(() => {
    selectedRef.current?.scrollIntoView({block: "nearest"})
  }, [selected, items])

  return (
    <div
      // Keeps the focus in the input while clicking or dragging the scrollbar.
      onMouseDown={(e) => e.preventDefault()}
      className="bg-popover text-popover-foreground absolute right-0 bottom-full left-0 z-30 mb-2 rounded-md border p-1 shadow-lg"
    >
      <div className="text-muted-foreground flex items-center justify-between gap-2 px-2 py-1 text-[11px]">
        <span>{playerPhase ? t("rc.players") : t("rc.commands")}</span>
        <span>{t("rc.completionHint")}</span>
      </div>
      <div
        id={id}
        role="listbox"
        aria-label={playerPhase ? t("rc.playerSuggestions") : t("rc.commandSuggestions")}
        className="max-h-56 overflow-y-auto"
      >
        {items.map((item, index) => (
          <button
            key={item.value}
            id={completionOptionId(id, index)}
            ref={index === selected ? selectedRef : undefined}
            type="button"
            role="option"
            aria-selected={index === selected}
            tabIndex={-1}
            onClick={() => onAccept(item)}
            className={`flex w-full items-center justify-between gap-3 rounded-sm px-2 py-1.5 text-left font-mono text-xs ${index === selected ? "bg-accent text-accent-foreground" : "hover:bg-accent/50"}`}
          >
            <span className="shrink-0">
              <Highlighted text={item.label} matches={item.labelMatches} />
            </span>
            <span className="text-muted-foreground min-w-0 truncate font-sans text-[10px]">
              {item.kind === "command"
                ? t(item.takesAccount ? "rc.playerType" : "rc.commandType")
                : item.detail && <Highlighted text={item.detail} matches={item.detailMatches} />}
            </span>
          </button>
        ))}
      </div>
    </div>
  )
}
