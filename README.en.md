# memo

**Local-first desktop memos with an optional Notion archive.**

English | [简体中文](README.md)

[![Release](https://img.shields.io/github/v/release/olianate/memo?label=release)](https://github.com/olianate/memo/releases/latest)
[![Release workflow](https://github.com/olianate/memo/actions/workflows/release.yml/badge.svg)](https://github.com/olianate/memo/actions/workflows/release.yml)

![memo main window](docs/screenshots/timeline.png)

memo is a local-first desktop memo app. Notes, search, tags and statistics live
in local SQLite and work offline; Notion is an optional long-term archive.

## Features

- Write immediately: `Enter` saves, `Shift+Enter` adds a paragraph
- Global quick capture with `Ctrl+Shift+Space`, `Esc` hides it
- Markdown blocks, tags, attachments and local search
- Trash, tray-first window and single instance
- Optional Notion archive with schema checks, sync pause and body retention

| Edit a memo | Quick capture |
| --- | --- |
| ![Edit a memo](docs/screenshots/editor.png) | ![Quick capture](docs/screenshots/quick-capture.png) |

![Statistics](docs/screenshots/stats.png)

## Download

Latest: [**Releases**](https://github.com/olianate/memo/releases/latest)

| Platform | Package |
| --- | --- |
| Windows x64 | [memo-windows-amd64.msi](https://github.com/olianate/memo/releases/latest/download/memo-windows-amd64.msi) (recommended) |
| Windows x64 | [memo-windows-amd64-installer.exe](https://github.com/olianate/memo/releases/latest/download/memo-windows-amd64-installer.exe) |
| macOS Intel / Apple Silicon | [memo-macos-universal.dmg](https://github.com/olianate/memo/releases/latest/download/memo-macos-universal.dmg) |

Choose one Windows package. The builds are unsigned and not notarized; use
SmartScreen "More info → Run anyway" or right-click → Open on macOS. Windows
needs WebView2, macOS needs 12+.

Uninstalling keeps `%APPDATA%\memo`; use **Settings → Delete all local data** to
remove it.

## Usage

1. Write in the composer; type `#tag` to attach a tag.
2. Click a card to edit its body, tags and attachments, or move it to the trash.
3. Closing the main window leaves memo in the tray; left-click the tray icon to restore.

Data lives in `%APPDATA%\memo` on Windows and
`~/Library/Application Support/memo` on macOS.

## Notion (optional)

1. Create a Notion integration and copy its token.
2. Share the target page or database with the integration.
3. In **Settings → Connect Notion**, paste the token and pick a database or page.
4. Existing databases are schema-checked: missing properties are added after confirmation; type conflicts block the connection.

The token is stored in the OS keychain, never in SQLite or logs. See
[`docs/development.md`](docs/development.md) for details.

## Build

Requires Go 1.26+, Node.js 24+ and the Wails v3 CLI
(`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0`). Windows MSI needs
WiX 3.14, the NSIS package needs NSIS, and macOS needs Xcode Command Line Tools.

```bash
go test ./internal/...

cd frontend
npm ci
npm test
npx tsc --noEmit
cd ..

wails3 dev
wails3 build

wails3 package INSTALL_SCOPE=user
./build/windows/msi/build.ps1 -Version 1.0.0 -WixDir <WiX 3.14 directory>

wails3 task darwin:package:universal
wails3 task darwin:create:dmg
```

Wails v3.1.0 is pinned under `third_party/wails` with the Windows IME fix.

## Release

```bash
git tag v1.0.0
git push origin v1.0.0
```

`.github/workflows/release.yml` runs the tests, builds the Windows MSI/NSIS and
the macOS universal DMG, writes `SHA256SUMS.txt` and publishes a GitHub Release.

## Docs

- [`docs/development.md`](docs/development.md): implementation, Notion behavior and packaging
- [`GLOSSARY.md`](GLOSSARY.md): domain vocabulary
- [`docs/adr/`](docs/adr/): architecture decisions
- [`SMOKE.md`](SMOKE.md): manual release checklist

## License

[MIT](LICENSE)
