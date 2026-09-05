# Editor recovery

Unsaved script, NPC flag, server configuration, and text-file edits are stored
under the user's configuration directory at `graal-rc/editor-drafts`. Reopening
the same resource restores the text locally and shows a notice. When the remote
baseline changed, the editor opens its existing diff with the current server
version on the left and the recovered text on the right. Recovery never uploads.

Each filename is a SHA-256 hash of the listserver endpoint, server endpoint,
login account, server name, resource kind, and resource key. The session epoch
is kept only in the native window lease so reconnecting can recover the same
draft while stale window callbacks cannot operate on the replacement session.
Read, upload, conflict resolution, dirty state and close bindings validate that
lease. A delayed local draft write remains valid after disconnect, until a newer
editor claims the same resource.

The frontend writes an immediate browser journal and debounces native writes
by 250 ms. It flushes on page hide/backgrounding. The native store uses a synced
temporary file and atomic replacement (`MoveFileEx` with replacement and write
through on Windows). Save/discard cleanup compares revisions; newer edits made
during upload are preserved. Small cleared-revision tombstones prevent a crash
between native cleanup and browser cleanup from restoring already discarded
text. A browser owner marker fences callbacks from a superseded window.

Text is limited to 4 MiB per original/current value and the native store to
128 MiB. Browser storage has its own quota. Errors remain visible in the editor;
the last valid native draft is preserved when a write fails. A corrupted native
draft explicitly fails editor opening instead of overwriting the damaged file.
There is no automatic expiry of unsaved drafts. Files contain local source code
and use user-only permissions where the operating system supports them.

Automated tests cover restart recovery, identity isolation, stale leases,
out-of-order writes, concurrent revision cleanup, payload limits, browser quota
failures, newer edits during save/discard, and server-change comparison. Manual
QA should include typing and forcing a connection drop, then reopening the same
resource; restarting the application; changing the remote baseline before
reopening; and checking that save/discard does not restore the cleared revision.
