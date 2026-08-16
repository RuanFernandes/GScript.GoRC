# GoRC — Remote Control

A modern, cross-platform **Remote Control (RC)** client for GServer-style multiplayer game servers.

GoRC is a native desktop application that gives server staff a single place to do
their day-to-day work: manage player accounts, moderate live chat, edit server-side
scripts, and browse, edit, and re-upload the remote file system — without ever opening
a raw packet or a terminal.

It is a from-scratch reimplementation of the classic staff Remote Control tooling,
built on a current stack ([Go](https://go.dev/) + [Wails v3](https://wails.io),
[React](https://react.dev) + [shadcn/ui](https://ui.shadcn.com)) and driven by the
protocol library in [`rclib/`](./rclib), which owns every detail of the wire format.
GoRC never hand-builds or hand-parses a packet; it talks to the server exclusively
through typed library calls.

> **Status:** actively developed. Windows x64 and Linux x64 builds are published
> through CI; see [Releases](#releases).

---

## Table of contents

- [What it does](#what-it-does)
- [Highlights](#highlights)
- [Screenshots](#screenshots)
- [How it works](#how-it-works)
- [Project layout](#project-layout)
- [Requirements](#requirements)
- [Building from source](#building-from-source)
- [Running in development](#running-in-development)
- [Releases](#releases)
- [Configuration & data](#configuration--data)
- [Contributing](#contributing)
- [License](#license)

---

## What it does

GoRC connects to a compatible game server with staff credentials and exposes the
operations a staff member needs:

- **Server list** — discover the servers your account has staff rights on and connect.
- **Account vault** — keep multiple accounts locally, passwords encrypted at rest.
- **Live chat** — read and send server chat, plus IRC channels, in tabbed views.
- **Player list** — a dedicated window showing who is online, with live updates.
- **Script management** — browse and edit **weapons**, **classes**, and **NPCs**;
  add, update, delete, warp, and open per-script editors.
- **File browser** — walk the remote file tree, download, upload (drag-in or picker),
  rename, move, and delete files. Open files by type: media in the OS handler,
  text in a built-in editor, SQLite databases in a full explorer.
- **SQLite explorer** — inspect tables, edit cells inline, run SQL with autocomplete,
  and visualize the schema as an ER diagram, then commit changes back atomically.
- **Scripting editor** — a Monaco-based editor with syntax highlighting for the
  server scripting language, a remote theme gallery, and configurable fonts.

Everything runs in independent native windows that share one live session.

## Highlights

- **Native and fast.** Compiled Go backend, native OS webview (WebView2 on Windows,
  WebKitGTK on Linux). No Electron, no bundled Chromium.
- **No CGO required for the app.** The protocol library is loaded dynamically via
  syscalls — no C toolchain, no gcc, no cross-compilation pain for the client itself.
  (Linux GTK builds still need system headers; see [Requirements](#requirements).)
- **Credentials never leave the backend.** Passwords are stored in an encrypted vault
  (DPAPI on Windows) and are never exposed to the UI layer.
- **Parity with the reference tooling**, plus modern UX: inline editing, ER diagrams,
  SQL autocomplete, drag-and-drop uploads, and a themeable editor.
- **Cross-platform releases** via GitHub Actions.

## Screenshots

_(Add screenshots here once available — drop them under `docs/img/` and link them.)_

## How it works

```
┌─────────────────────────────────────────────────────────────┐
│                      Native OS webview                       │
│   React + shadcn/ui (screens, hooks, services)               │
│        ▲ typed TS bindings (generated)        │
└────────┼─────────────────────────────────────────────────────┘
         │ Wails v3 IPC
┌────────┴────────────────────────────────────────────────────┐
│   Go application service (graal-rc)                          │
│   ├── internal/connection  — session, handle, event pump     │
│   ├── internal/credentials — encrypted multi-account vault   │
│   └── internal/sqlite      — local DB explorer engine        │
│        ▼ syscall (no CGO)                                   │
├─────────────────────────────────────────────────────────────┤
│   rclib (grclib) — the protocol library (LGPL-2.0)           │
│   packets · strings · file compression · editors · IRC       │
└─────────────────────────────────────────────────────────────┘
```

Layering is strict and one-directional:

1. **`rclib`** — thin Go bindings over the C-ABI protocol library. Resolves
   function pointers, marshals Go types to/from the library, and wires callbacks.
2. **`internal/connection`** — owns the live session and handle, correlates
   asynchronous requests with replies, and emits Wails events to the UI.
3. **App service (`app.go`)** — the Wails v3 service exposing methods to the
   frontend, plus window lifecycle and persistent settings.
4. **Frontend** — `services → hooks → screens`. The frontend only ever calls
   generated bindings; it never knows about the wire protocol.

## Project layout

```
.
├── graal-rc/                 # the application (Go + React)
│   ├── app.go                # Wails v3 service: bindings, windows, settings
│   ├── rclib/rclib.go        # Go bindings to the protocol library
│   ├── internal/connection/  # session, handle, async request correlation
│   ├── internal/credentials/ # encrypted account vault
│   ├── internal/sqlite/      # local SQLite explorer engine
│   ├── build/                # platform Taskfiles, NSIS, icons, manifests
│   └── frontend/             # React + shadcn/ui + Tailwind v4
├── rclib/                    # the protocol library (C ABI), LGPL-2.0
│   ├── README.md             # library API and build instructions
│   └── example/              # reference C++ client
└── .github/workflows/        # CI: build + publish releases
```

## Requirements

**To run a release:** just grab the installer, AppImage, or macOS `.app`
tarball from [Releases](#releases). No extra runtime is needed on Windows or
macOS; Linux needs the GTK/WebKit runtime libraries (typically preinstalled on
mainstream desktop distros).

**To build from source:**

- [Go](https://go.dev/dl/) **1.25+**
- [Node.js](https://nodejs.org/) **22** and npm
- The Wails v3 CLI: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117`
- **Linux only:** `libgtk-4-dev libwebkitgtk-6.0-dev libadwaita-1-dev libfuse2t64`
  (and `pkg-config`, `file`, `zip`).
- **Windows only (packaging):** [NSIS](https://nsis.sourceforge.io/) for the installer.
- The prebuilt protocol library (`grclib64.dll`, `grclib.so`, or `grclib.dylib`)
  under `rclib/`.
  See [`rclib/README.md`](./rclib/README.md) to build it yourself.

## Building from source

All commands run from the `graal-rc/` directory unless noted.

```sh
# 1. Frontend dependencies
cd graal-rc/frontend
npm install

# 2. Backend + frontend production build
cd ..
wails3 task build          # native binary in graal-rc/bin/

# 3. Package for the host OS
wails3 task windows:package   # Windows: NSIS installer (.exe)
wails3 task linux:package     # Linux: AppImage
wails3 task darwin:package    # macOS: graal-rc.app bundle
```

> **Note:** build the Go packages explicitly (`go build . ./rclib/ ./internal/connection/`)
> rather than `go build ./...` — the `build/{ios,android,…}` directories contain
> platform-tagged stubs that are not meant for a plain `./...` compile.

To regenerate the TypeScript bindings consumed by the frontend (after changing any
bound Go method):

```sh
# from graal-rc/
wails3 generate bindings -ts -d frontend/bindings
```

## Running in development

```sh
cd graal-rc
wails3 task dev
```

This starts Vite and launches the app with hot reload. Each extra window (player list,
script manager, file browser, editors) is opened on demand by the running session.

## Releases

Releases are produced by the [`release`](./.github/workflows/release.yml) workflow,
triggered manually from the **Actions** tab. It builds and publishes:

- **Windows x64** — NSIS installer
- **Linux x64** — AppImage
- **macOS x64** — `.app` bundle in a tarball (native Intel and Rosetta 2)

Each release bundles the matching native protocol library for its platform and bitness.

## Configuration & data

GoRC stores its data under the user configuration directory
(`<UserConfigDir>/graal-rc/` on the host), including:

- `accounts.dat` — the encrypted account vault (Windows: DPAPI-protected, tied to the
  current Windows user and machine).
- `coding.json` — editor theme, font, and size settings.
- `filebrowser.json` — file browser preferences (e.g. downloads folder).
- `remote-theme.json` — cached editor theme (works offline after first load).
- `filecache/` — transient cache for opened remote files.

Passwords are never written to the frontend or logged.

## Contributing

Contributions are welcome. Please open an issue first for non-trivial changes so the
approach can be discussed. Keep the [layering](#how-it-works) intact: the frontend
must not know about the wire protocol, and `rclib` must remain a thin binding layer.

When adding or changing a bound method, regenerate the bindings and exercise the
feature against a live server before opening a pull request.

## License

GoRC is licensed under the **GNU General Public License v3**. See
[`LICENSE`](./LICENSE).

The protocol library in [`rclib/`](./rclib) is a separate work licensed under
**LGPL-2.0-only**; see [`rclib/README.md`](./rclib/README.md) and `rclib/LICENSE`.
The GPL does not override the permissions the LGPL grants for that library.
