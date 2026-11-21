# Pin Wails v3.1.0 with the PR #6596 IME patch

Status: accepted
Date: 2026-11-21

Wails v3.1.0 reproduces #6595 on Windows 11 + Microsoft Pinyin: after Alt+Tab, the Pinyin candidate window jumps to a screen corner. PR #6596 defers `MoveFocus` until the window activation sequence completes, and the IME spike passed with it. We decided to pin Wails v3.1.0 plus that patch via a local module replace instead of switching to Tauri 2 or Electron, because the rest of the stack and the spike pass. macOS IME behavior is untested by decision and accepted as a risk.

## Consequences

- The patched Wails module is vendored at `third_party/wails` and referenced by a `replace` directive; builds are not upstream-clean. The patch originates from [wailsapp/wails#6596](https://github.com/wailsapp/wails/pull/6596); remove the vendored copy and replace directive when the fix ships upstream.
- Remove the patch when upstream ships the fix; add a regression check to the release checklist.
- macOS IME remains a manual test before a macOS release.
