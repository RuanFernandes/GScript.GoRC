// PlayerTable renders the active server's players (rc_get_players). The level
// column is run through parsePlayerTag so status-prefixed values like
// "H Testbed3d" render as a flag badge + clean value instead of the raw token.
import {Badge} from "@/components/ui/badge"
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table"
import {parsePlayerTag} from "@/lib/playerTag"
import type {Player} from "@/types"

interface PlayerTableProps {
  players: Player[]
}

export function PlayerTable({players}: PlayerTableProps) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Nick</TableHead>
          <TableHead>Account</TableHead>
          <TableHead>Level</TableHead>
          <TableHead className="text-right">ID</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {players.length === 0 ? (
          <TableRow>
            <TableCell className="text-muted-foreground" colSpan={4}>
              No players.
            </TableCell>
          </TableRow>
        ) : (
          players.map((p) => {
            const tag = parsePlayerTag(p.level)
            return (
              <TableRow key={`${p.account}-${p.id}`}>
                <TableCell className="font-medium">{p.nick || p.account}</TableCell>
                <TableCell className="text-muted-foreground">{p.account}</TableCell>
                <TableCell>
                  {tag.flag ? (
                    <span className="inline-flex items-center gap-1.5">
                      <Badge variant="secondary" className="font-mono">
                        {tag.flag}
                      </Badge>
                      {tag.value}
                    </span>
                  ) : (
                    <span className="text-muted-foreground">{tag.value}</span>
                  )}
                </TableCell>
                <TableCell className="text-right text-muted-foreground tabular-nums">{p.id}</TableCell>
              </TableRow>
            )
          })
        )}
      </TableBody>
    </Table>
  )
}
