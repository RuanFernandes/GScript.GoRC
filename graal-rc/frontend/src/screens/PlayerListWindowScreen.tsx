// PlayerListWindowScreen is the content rendered in the external "Players"
// window (opened via App.OpenPlayerList, URL "/#players"). It is a standalone
// full-window view that polls the shared backend service for the live player
// cache — same data the main window sees — and renders the player table.
import {Loader2} from "lucide-react"

import {PlayerTable} from "@/components/features/playerlist/PlayerTable"
import {ScrollArea} from "@/components/ui/scroll-area"
import {usePlayers} from "@/hooks/usePlayers"
import {rcService} from "@/services/rcService"

export function PlayerListWindowScreen() {
  const {players, loading} = usePlayers(rcService, true)

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2.5">
        <h1 className="text-base font-semibold">Players</h1>
        <span className="text-muted-foreground text-sm">({players.length})</span>
        {loading && <Loader2 className="text-muted-foreground size-4 animate-spin" />}
      </header>
      <ScrollArea className="min-h-0 flex-1 p-2">
        <PlayerTable players={players} />
      </ScrollArea>
    </div>
  )
}
