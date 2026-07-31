# agentapi-proxy Native Dashboard

A minimal, production-shaped **macOS menu-bar dashboard** for the
agentapi-proxy **native External Session Manager** (ESM). Built with
Tauri 2 + Vite + React + TypeScript.

The app is a thin UI on top of the existing `agentapi-proxy native` CLI. It
shells out to the proxy binary to read status, sessions, doctor output, and to
restart the daemon. The native daemon is owned by launchd and runs
independently of this dashboard — closing the window simply hides it.

## Layout

```
native/
├── package.json            # dev / build / check / tauri scripts
├── vite.config.ts          # Vite dev server on :1420 (Tauri convention)
├── tsconfig.json
├── index.html
├── src/                    # React + TypeScript frontend
│   ├── main.tsx
│   ├── App.tsx             # dashboard shell, loading/error/empty states
│   ├── api.ts              # invoke() wrappers + refresh-event listener
│   ├── types.ts            # mirrors of the Rust structs
│   ├── styles.css          # tasteful lightweight CSS (light + dark)
│   └── components/
│       ├── StatusCard.tsx
│       ├── SessionsCard.tsx
│       └── DoctorCard.tsx
└── src-tauri/              # Rust + Tauri 2 shell
    ├── Cargo.toml
    ├── tauri.conf.json
    ├── build.rs
    ├── capabilities/default.json
    ├── icons/              # placeholder icons (see below)
    └── src/
        ├── main.rs         # entry point
        ├── lib.rs          # tray menu, window hide-on-close, command registration
        ├── binary.rs       # locate proxy via AGENTAPI_PROXY_NATIVE_BINARY or PATH
        ├── commands.rs     # Tauri commands wrapping `native <sub> --json`
        └── types.rs        # serde structs matching the CLI JSON
```

## What it shows

- **Service** card: service state, health, manager ID, version, upstream /
  public URL, state directory, filesystem-sandbox flag, and labels.
- **Active session count** in the header.
- **Sessions** table: id, status, pid, start time, with an empty state.
- **Doctor** card: result of `native doctor --json` (healthy / issues).
- **Refresh** and **Restart** actions; optional 15s auto-refresh while visible.
- **Loading / error / empty** states for every panel.

## Menu-bar tray

The app installs a system-tray icon with:

- **Show Dashboard** — reveals and focuses the window.
- **Refresh** — emits a `dashboard://refresh` event the frontend listens for.
- **Restart** — runs `native restart` on a background thread, then refreshes.
- **Quit** — exits the dashboard only.

A single left-click on the tray icon also reveals the dashboard.

### Window close = hide

The dashboard window intercepts `CloseRequested`, calls `api.prevent_close()`,
and hides itself. The window is created hidden on launch and revealed from the
tray. The native daemon (launchd `com.agentapi.native`) is independent and
keeps running regardless of the dashboard's lifecycle.

## How it talks to the proxy

The Rust side locates the `agentapi-proxy` binary with this precedence:

1. `AGENTAPI_PROXY_NATIVE_BINARY` — absolute path to the proxy binary.
2. The macOS binary managed by `native install` under
   `~/Library/Application Support/agentapi-native/bin/`.
3. `agentapi-proxy` looked up on `PATH`.

Then it runs (JSON parsed into typed serde structs):

| Command                         | Tauri command    | Rust struct      |
|---------------------------------|------------------|------------------|
| `native status --json`          | `native_status`  | `NativeStatus`   |
| `native sessions --json`        | `native_sessions`| `Vec<NativeSession>` |
| `native doctor --json`          | `native_doctor`  | `DoctorResult`   |
| `native restart`                | `native_restart` | `CommandResult`  |

`sessions` is the alias of `session-list` and emits a JSON array. `doctor`
returns an overall `ok` value plus structured checks so the dashboard can show
partial failures without parsing human-readable output.

The struct fields mirror `nativeStatusOutput` / `nativeSessionListEntry` in
`backend/cmd/native.go` (`#[serde(rename_all = "snake_case")]` + `#[serde(default)]`
on optional fields) so partial CLI output stays forward-compatible.

## Prerequisites

- macOS 13+ (menu-bar tray + `filesystem-sandbox` is macOS-only upstream).
- Node.js 20+ and a package manager (`bun` is used in the scripts; `npm`/`pnpm`
  work too — adjust `beforeDevCommand` / `beforeBuildCommand` in
  `tauri.conf.json` if you switch).
- Rust toolchain (`rustup`) with the `aarch64-apple-darwin` and
  `x86_64-apple-darwin` targets for Tauri 2.
- The `agentapi-proxy` binary installed and on `PATH`, **or**
  `AGENTAPI_PROXY_NATIVE_BINARY` pointing at it. Install the daemon first with
  `agentapi-proxy native install ...` (see `backend/README.md`).

## Scripts

```bash
bun install

bun run dev       # Vite dev server on http://127.0.0.1:1420
bun run check     # tsc --noEmit (typecheck)
bun run build     # tsc --noEmit && vite build  -> dist/
bun run preview   # preview the built dist/
bun run tauri dev # build the Rust shell, launch the menu-bar app
bun run tauri build # produce a signed .app / .dmg bundle
```

> The `tauri` scripts require Rust/cargo. `dev`, `check`, `build`, and
> `preview` are pure-frontend and need only Node + the npm dependencies.

## Icons

`src-tauri/icons/` contains small placeholder PNGs so the project is
self-contained. For a real bundle, drop a 1024×1024 `app-icon.png` into
`src-tauri/icons/` and run:

```bash
bun run tauri icon src-tauri/icons/app-icon.png
```

This regenerates the full set (`icon.icns`, `128x128.png`, etc.) referenced by
`tauri.conf.json`.

## Development notes

- The Vite dev server uses a **fixed port 1420** with `strictPort` so the Rust
  shell's `devUrl` always matches.
- `clearScreen: false` keeps Tauri's build logs readable.
- The frontend never touches the filesystem or the network directly — all
  proxy interaction is funnelled through Tauri commands, which run the CLI in
  the main process. This keeps the bundle sandbox-friendly.
- `Promise.allSettled` in `src/api.ts` means a single failing panel (e.g.
  `doctor` returning non-zero) does not blank the whole dashboard; warnings
  are surfaced in a banner and the remaining panels render normally.
- Capabilities in `capabilities/default.json` are scoped to the `dashboard`
  window and only allow the core, window, event, and opener permissions the
  UI actually needs.

## Caveats

- The dashboard is read-only apart from **Restart**; install / uninstall /
  rotate-token remain CLI-only by design.
