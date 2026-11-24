# memo — development notes

Local-first desktop memos with optional Notion archiving, built with Go, Wails
v3, React/TypeScript and SQLite. The local store is authoritative; Notion is an
archive and the sync transport.

For downloads, screenshots and the project overview, see the
[README](../README.md). This document covers the implementation, Notion behavior
and packaging.

## Stack and layout

```
main.go                       Wails application, windows, tray, hotkey
internal/
  domain/                     memo, block AST, tags, Markdown parser/serializer
  store/                      SQLite schema, transactions, outbox, settings
  search/                     FTS5 trigram + LIKE fallback
  stats/                      metadata-driven dashboard queries
  notion/                     REST gateway, block/property mapping, HTTP client
  notion/fake/                in-memory gateway for sync tests
  sync/                       push, pull, conflict, retention, hydration
  attachments/                image compression, thumbnails, file storage
  platform/                   data dir, keychain, open-with-OS
  services/                   typed Wails boundary (the only frontend API)
frontend/                     React UI (main window + sticky capture)
tools/seed/                   dev profile seed for screenshots/smoke tests
third_party/wails/            pinned Wails v3.1.0 + PR #6596 (IME fix)
```

The UI never calls Notion or the filesystem. Every call goes through a method in
`internal/services`; the TypeScript `Backend` interface (`frontend/src/backend.ts`)
is the seam, with a demo implementation used outside the Wails runtime.

## Run, test, build

Run commands from the repository root unless a `cd` is shown. Requirements:
Go 1.26+, Node 24+, Wails v3 CLI and WebView2 on Windows. The Wails module is
pinned locally, so no network fetch of a forked Wails is needed.

```powershell
# backend tests (temporary SQLite databases + fake Notion gateway)
go test ./internal/...

# frontend unit/component tests and the embedded production assets
cd frontend
npm ci
npm test
npm run build
cd ..

# full build (production tag embeds frontend/dist)
wails3 build
.\bin\memo.exe

# development with hot reload
wails3 dev
```

The app uses `%APPDATA%\memo` for `memo.db` and `attachments/`. Set
`MEMO_DATA_DIR` to use another profile (the seed tool does this). For a
screenshot/smoke run, `MEMO_SHOW_STICKY=1` opens quick capture at startup
without relying on the global hotkey:

```powershell
$env:MEMO_DATA_DIR="$env:TEMP\memo-demo"
go run ./tools/seed
go build -tags production -o bin/memo-demo.exe .
.\bin\memo-demo.exe
```

### Generated bindings

`frontend/bindings/` is committed and mirrors the Go import path, so the module
path in `go.mod` decides its layout
(`frontend/bindings/github.com/olianate/memo/internal/...`). After changing a
bound service or DTO, regenerate before committing:

```powershell
wails3 task common:generate:bindings
```

The task runs `wails3 generate bindings -clean`, which deletes and rewrites the
whole tree. Never hand-edit those files.

## Notion setup

1. Create a Notion integration and copy its personal access token.
2. Share the target with the integration:
   - Preferred: share an existing database. memo connects to it directly.
   - Alternative: share a page. memo creates its own database under that page
     and asks for the database name.
3. In memo, choose **连接 Notion**, paste the token, validate, then pick a
   database or page. If search lags after sharing, paste the link directly.
4. For an existing database, memo checks the schema before connecting. It only
   adds missing properties and asks for confirmation first. A same-named
   property with an incompatible type blocks the connection.
5. The token is stored in the OS keychain. It is never written to SQLite or
   logs.

The app pins `Notion-Version: 2026-03-11` and uses `data_sources/{id}/query`.
See `adr/0001-notion-as-optional-archive.md`,
`adr/0003-updated-property-is-sync-cursor.md` and
`adr/0005-tags-are-metadata.md` for the constraints behind the design.

## Packaging

The app version shown in Settings lives in `internal/services/version.go`
(currently `1.1.0`). Keep `build/config.yml`, `build/windows/info.json` and
`build/darwin/Info*.plist` in sync when bumping it.

### Windows NSIS installer

Per-user install, no admin prompt:

```powershell
# NSIS must be on PATH. A portable copy works:
#   https://nsis.sourceforge.io/Download
wails3 package INSTALL_SCOPE=user
# -> bin/memo-amd64-installer.exe
```

The installer installs under `%LOCALAPPDATA%\Programs\memo`, creates Start Menu
and desktop shortcuts, and installs the WebView2 bootstrapper. Uninstall keeps
`%APPDATA%\memo` (notes, attachments, settings). To remove local data first, use
**设置 → 删除全部本地数据**; Notion itself is never modified.

### Windows MSI

The MSI is authored with WiX Toolset 3.14 and installs per-user into
`%LOCALAPPDATA%\Programs\memo` with Start Menu and desktop shortcuts. It needs
the production binary from `wails3 build`, then:

```powershell
# Download and extract wix314-binaries.zip first:
#   https://github.com/wixtoolset/wix3/releases/tag/wix3141rtm
.\build\windows\msi\build.ps1 -Version 1.1.0 -WixDir C:\path\to\wix314
# -> bin/memo-windows-amd64.msi
```

The WiX source is `build/windows/msi/memo.wxs`; the uninstaller keeps the data
directory, matching the NSIS behavior.

### macOS

macOS must be packaged on a Mac:

```bash
wails3 task darwin:package:universal   # Intel + Apple Silicon .app
wails3 task darwin:create:dmg          # bin/memo.dmg
# or: wails3 package                   # host-architecture .app
```

Unsigned builds need right-click → Open the first time. macOS IME behavior is an
accepted, untested risk (gate 11 was waived for macOS); test on a Mac before any
release.

### Updates

Updates are checked from GitHub Releases at startup and every 12 hours, and on
demand under Settings → 软件更新. The updater downloads the matching installer,
verifies `SHA256SUMS.txt` when present, runs it silently, and relaunches memo.
The installers remain unsigned, so Windows SmartScreen or macOS Gatekeeper may
still ask for confirmation. On Windows the updater reuses the MSI it was
installed from (detected via the MSI registry marker) and otherwise uses the
NSIS installer. On macOS the new `.app` is copied over the existing bundle;
keep a copy of the old build when testing the update path.

#### macOS icon (Icon Composer)

`build/appicon.icon` is an Icon Composer document for the macOS 26 icon style: a
green gradient fill, the `memo_glyph.png` layer, a neutral shadow and the
specular highlight. On macOS with Xcode 26+, `generate:icons` compiles it with
`actool` into `build/darwin/Assets.car` and rewrites `build/darwin/icons.icns`
from the same document; `create:app:bundle` copies both into the bundle, and
`CFBundleIconName=appicon` (`build/darwin/Info*.plist`, mirrored by
`info.cfBundleIconName` in `build/config.yml`) selects the asset-catalog icon.

```bash
# what generate:icons runs on macOS
cd build
wails3 generate icons -input appicon.png -macfilename darwin/icons.icns \
  -windowsfilename windows/icon.ico \
  -iconcomposerinput appicon.icon -macassetdir darwin
```

Without Xcode 26 the compilation is skipped and the `.app` keeps the PNG-derived
`icons.icns`: the app still runs, but macOS 26 shows the flat icon. The release
workflow prints a warning when `Assets.car` is missing. Both outputs are
regenerated per build, so `Assets.car` is gitignored while `icons.icns` stays
committed for hosts that cannot run `actool`.

### GitHub Actions

`.github/workflows/ci.yml` runs on every push to `main` and on every pull
request: `gofmt`, `go build ./...`, `go vet ./...`, `go test ./internal/...`,
`npx tsc --noEmit` and `npm test`. It is also the single definition of the test
steps, so it can be called as a reusable workflow.

`.github/workflows/release.yml` runs on `v*` tags and reuses `ci.yml` for its
`verify` job, then checks the tag against `build/config.yml`, builds the Windows
NSIS + MSI packages, builds the macOS universal DMG, writes `SHA256SUMS.txt` and
publishes a GitHub Release with the stable asset names used by the README
download links. Release notes are read from `docs/releases/<tag>.md` (for
example `docs/releases/v1.1.0.md`); commit that file before pushing the tag.
If it is absent, the workflow falls back to a generic summary. The workflow can
also be triggered manually to exercise the build jobs without publishing.

## Verification

- `go test ./...` covers the store, search, stats, attachments, Notion gateway
  contract, sync push/pull/conflict, two-device convergence, retention and
  hydration.
- `npm test` covers tag quick entry, Markdown rendering, day grouping, the
  composer, search snippets, the timeline and the statistics zero state.
- `../SMOKE.md` is the manual checklist for launch, capture, IME, search, Notion
  connection, retention and restart.

## Known limits (v1)

- The local SQLite database is not encrypted; use OS full-disk encryption.
- The FTS index retains tokenized text after body eviction. This is deliberate
  so search still works.
- Notion cannot permanently delete pages or revoke uploaded file objects.
- Notion-side body edits are not pulled; the app is the writer.
- Markdown tables, HTML, math, footnotes and embeds stay literal text.
- macOS IME is untested by decision.
