# memo

**Local-first desktop memos with an optional Notion archive.**

English | [简体中文](README.md)

[![Release](https://img.shields.io/github/v/release/olianate/memo?label=release)](https://github.com/olianate/memo/releases/latest) [![Release workflow](https://github.com/olianate/memo/actions/workflows/release.yml/badge.svg)](https://github.com/olianate/memo/actions/workflows/release.yml)

![memo main window](docs/screenshots/timeline.png)

## Features

- Write immediately: `Enter` saves, `Shift+Enter` adds a paragraph; Markdown live preview
- Global quick capture with `Ctrl+Shift+Space`, `Esc` hides it
- Tags, attachments, local search, statistics, pinning, trash (auto-purged after 30 days)
- Tray-first, single instance; data in local SQLite, works offline
- Optional Notion archive with schema checks, sync pause and body retention

| Edit a memo | Quick capture |
| --- | --- |
| ![Edit a memo](docs/screenshots/editor.png) | ![Quick capture](docs/screenshots/quick-capture.png) |

![Statistics](docs/screenshots/stats.png)

## Download

[Latest release](https://github.com/olianate/memo/releases/latest)

- Windows x64: [MSI](https://github.com/olianate/memo/releases/latest/download/memo-windows-amd64.msi) (recommended) / [NSIS installer](https://github.com/olianate/memo/releases/latest/download/memo-windows-amd64-installer.exe)
- macOS Intel / Apple Silicon: [Universal DMG](https://github.com/olianate/memo/releases/latest/download/memo-macos-universal.dmg)

Builds are unsigned: on first launch use SmartScreen "More info → Run anyway" on Windows, or right-click → Open on macOS. Windows needs WebView2; macOS needs 12+.

## Usage

Write in the composer and type `#tag` to add a tag; click a card to edit its body, tags and attachments, or move it to the trash. Closing the main window leaves memo in the tray; left-click the tray icon to restore.

Data lives in `%APPDATA%\memo` on Windows and `~/Library/Application Support/memo` on macOS. Uninstalling keeps it; use **Settings → Delete all local data** to remove it. If startup fails, memo shows a native dialog and writes `logs/memo.log`.

## Notion (optional)

Create an integration and share the target page or database with it, then paste the token in **Settings → Connect Notion**. The token is stored in the OS keychain, never in SQLite or logs; see [`docs/development.md`](docs/development.md) for details.

## Build

Requires Go 1.26+, Node.js 24+ and the Wails v3 CLI (`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0`); packaging MSI / NSIS / DMG additionally needs WiX 3.14 / NSIS / Xcode Command Line Tools.

```bash
go test ./internal/... ./tools/...
( cd frontend && npm ci && npm test && npx tsc --noEmit )

wails3 dev     # hot reload
wails3 build   # production build
```

## Docs

Development and packaging: [`docs/development.md`](docs/development.md); releasing: [`docs/releasing.md`](docs/releasing.md). Also see the [glossary](GLOSSARY.md), [architecture decisions](docs/adr/), [release checklist](SMOKE.md) and [third-party notices](NOTICE).

## License

[MIT](LICENSE)
