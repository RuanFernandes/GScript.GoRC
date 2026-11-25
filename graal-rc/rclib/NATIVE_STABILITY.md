# Native call diagnostics

All native calls are serialized by `dllMu`, taken either by `proc.Call`
(handle-free exports) or by `withConnection` (see `handle.go`). One hold can cover
several exports, such as a query plus `rc_last_error`; it is reported under the
first export's name. Instrumentation records that name, time spent waiting for that mutex, call duration, queue
depth, and cumulative timing counters. It does not record native arguments,
handles, account names, credentials, file contents, or server addresses.

Start `MonitorNativeCalls(stop)` in one application-owned background goroutine.
It exits when `stop` closes. There is at most one monitor per process and no
goroutine, timer, or unbounded history allocated per native call.
`ReadNativeCallStats` returns an independent snapshot for local diagnostics.

The monitor samples once per second. Completed calls or mutex waits of at least
250 ms contribute to slow-operation counters, summarized at most every 30 seconds
while those counters change. Calls still running after five seconds produce a
stall report; the same call is reported again at most every 30 seconds until it
exits. A stall is an observation of elapsed time, not proof of a deadlock: login,
large transfers, and a slow network can legitimately take longer.

The diagnostics mutex is separate from `dllMu` and is never held while entering
native code or waiting for native serialization. As a result, the watchdog can
report a stuck call even when every other native operation is blocked. Stopping
the watchdog also does not need the native mutex.

## Limits and next steps

A Go deadline cannot stop arbitrary DLL code. The watchdog does not release
`dllMu`, call disconnect, retry writes, or start a replacement connection while a
native call is still active. Doing so could corrupt shared native state or
duplicate a server mutation.

To recover automatically from a permanently blocked or crashing DLL, move the
native session into a supervised helper process with bounded IPC requests and
responses. The desktop process could then terminate the helper, preserve editor
drafts, and offer a new session. That change requires an explicit session and
request ownership design; it is not provided by this diagnostics layer.

## Connection ownership

`Handle` is a never-reused id, not the native pointer. Exports that take the
connection are `connectionProc` values, which can only be invoked inside
`withConnection`: liveness is checked under the same `dllMu` hold as the call,
and `Disconnect` retires the id in the hold that runs `rc_disconnect`.

`rc_get_*` free their previous result on the next call, so results are copied
into Go memory inside the same hold.

`dllMu` cannot serialize grclib's own receive threads. In the grclib source,
`rc_last_error` returns `last_error.c_str()` without taking `error_mutex`, and
the receive thread rebuilds `server_cache` (the array `rc_get_servers` returns)
when a server list reply arrives. Closing those two races needs a grclib change
that returns caller-owned copies.
