# Pinned Wails v3.1.0 with PR #6596

This directory is a copy of `github.com/wailsapp/wails/v3` v3.1.0 with the
patch from upstream PR #6596 applied:

- Upstream fix: [wailsapp/wails#6596](https://github.com/wailsapp/wails/pull/6596)
- Fixes: [#6595](https://github.com/wailsapp/wails/issues/6595), the Windows
  CJK IME candidate window jumping to a screen corner after Alt+Tab.
- Gate 11 validated this patch on Windows 11 with Microsoft Pinyin.
  macOS IME is untested by decision and remains an accepted risk.
- `go.mod` points at this copy with a `replace` directive.

Remove this copy and the `replace` when the fix ships in an upstream Wails
release. Keep the IME check in the release smoke checklist.
