# memo — smoke checklist

Run before calling a build releasable. Use a disposable profile:
`$env:MEMO_DATA_DIR="$env:TEMP\memo-smoke"`.

## Launch and capture

- [ ] `.\bin\memo.exe` launches; the window title is `memo`.
- [ ] First run shows onboarding; **暂时不连接** reaches the notes screen.
- [ ] The composer saves with Enter; Shift+Enter adds a paragraph; the memo
      appears in the timeline immediately.
- [ ] Restart the app: the memo and its tags are still there.
- [ ] `Ctrl+Shift+Space` shows the sticky window; Esc hides it; the draft is
      preserved while the window is hidden.
- [ ] Sticky position survives a restart and is clamped to the monitor work
      area.
- [ ] Type Chinese with Microsoft Pinyin in the composer and the sticky window.
      Alt+Tab away/back and confirm the candidate window stays anchored
      (gate 11; requires the pinned Wails patch).

## Editing, tags, attachments

- [ ] Edit a memo from the sheet; `updated_at` changes and the timeline
      refreshes.
- [ ] Delete a memo: it leaves the timeline, appears in 回收站, restores.
- [ ] Type `#工作` + space in live mode: a chip appears and the body no longer
      contains `#工作`. `# 标题` stays a heading. `C#` stays text.
- [ ] Rename and merge tags in the sidebar; memo bodies and revisions are
      unchanged.
- [ ] Paste/drag/pick a PNG: thumbnail appears, the memo survives restart, and
      clicking opens the image.
- [ ] Add a PDF: it renders as a file chip and opens with the OS default app.
- [ ] Switch live/source modes and back; headings, lists, todo, quote, code,
      divider and inline marks round-trip. Unsupported Markdown stays literal.

## Search and stats

- [ ] Search an English phrase; results show a highlighted snippet.
- [ ] Search with one and two Chinese characters; both return results (LIKE
      fallback).
- [ ] Combine a tag filter and a date range with a query.
- [ ] `设置 → 重建搜索索引` completes and search still works.
- [ ] Statistics: month selector, totals, daily bars, yearly heatmap,
      composition, word buckets, top tags, co-occurrence, hours, todo
      completion, attachments and sync/storage health all render.
- [ ] A month with no notes shows the zero state.
- [ ] After a body is evicted, search still finds the memo and stats still count
      it.

## Notion connection and sync

- [ ] Invalid token: actionable error, nothing stored.
- [ ] Valid token + parent page: the data source is created, the token is in the
      OS keychain, and onboarding completes.
- [ ] Capture a memo: it appears in Notion with properties and body blocks.
- [ ] Edit it: revision and body update; no duplicate page.
- [ ] Delete it: `Status=deleted`, page stays active, not trashed.
- [ ] Second profile/device with the same data source pulls create, edit and
      delete within a minute while active.
- [ ] Manual Notion trash is caught by the daily best-effort sweep (or is
      documented as lagging).
- [ ] Disconnect: token cleared, local notes remain.

## Retention and recovery

- [ ] Set retention to immediate, sync a memo, wait for the retention tick:
      body_json is empty, body_text/tags/FTS remain, attachment files are gone.
- [ ] Open an evicted memo: it hydrates from Notion and the retention clock
      resets.
- [ ] Pin a memo (`保留本地`): it is never evicted.
- [ ] Local-only memos are never evicted.
- [ ] Open an evicted memo offline: placeholder + retry, no data loss.
- [ ] `从 Notion 恢复全部` reports progress and restores every evicted memo.

## Settings and packaging

- [ ] Theme light/dark/system applies immediately and survives restart.
- [ ] Hotkey change applies immediately; an invalid/conflicting hotkey surfaces
      a message.
- [ ] Start at login toggles the HKCU Run entry and survives restart.
- [ ] Export backup writes `backups/backup-<timestamp>/memo.db` + attachments.
- [ ] Export logs writes a redacted sync log.
- [ ] `删除全部本地数据` asks for confirmation, wipes local memos, and turns sync
      off (Notion archive untouched).
- [ ] `bin/memo-amd64-installer.exe` installs per-user without UAC, launches,
      and `Ctrl+Shift+Space` works after install.
- [ ] Uninstall removes the program but keeps `%APPDATA%\memo`; the delete-data
      action is documented.
- [ ] macOS `.app`/`.dmg`: build on a Mac, right-click → Open for the unsigned
      app, and record the IME result (accepted risk until tested).
