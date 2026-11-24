# memo

A local-first desktop memo app that can archive synced notes in Notion.

## Language

**Memo**:
A single timestamped Markdown note; the unit of capture, search, sync, and local eviction.
_Avoid_: Note, card, entry

**Attachment**:
An image or file stored with a memo. Metadata survives local eviction; bytes are evicted and hydrated on demand.
_Avoid_: Media, upload

**Memo body**:
The Markdown text the user typed, stored verbatim and canonical; it is the only body source of truth. Derived text, statistics, and Notion blocks are projections of it.
_Avoid_: Body JSON, block AST, rich text, Markdown source

**Live preview editor**:
The single Markdown editing surface where the line holding the caret is raw source and committed lines render; there is no separate source mode.
_Avoid_: Block editor, WYSIWYG, Notion editor

**Tag**:
A flat label stored as memo metadata and projected to a Notion multi-select property. `#tag` is an editor quick-entry shortcut; the token is consumed and never kept in the raw Markdown body.
_Avoid_: Label, category, folder

**Sticky capture window**:
The frameless, always-on-top window summoned by a global hotkey for quick capture.
_Avoid_: Quick note, popup, widget

**Local retention**:
The policy that evicts a synced memo's body and attachments from local disk after a retention period while keeping its metadata and search index.
_Avoid_: Cleanup, cache expiry, deletion

**Evicted memo**:
A memo whose body and attachments are no longer on local disk but whose metadata, tags, and search index remain.
_Avoid_: Deleted memo, archived memo

**Notion archive**:
The Notion data source that stores synced memos; it is an archive, not a real-time database or a privacy eraser.
_Avoid_: Notion database, backup

**Sync outbox**:
The durable local queue of pending create, update, and delete operations for Notion.
_Avoid_: Queue, pending changes

**Tombstone**:
A memo marked deleted via the `Status` property; the Notion page stays active so every device can see the deletion.
_Avoid_: Deleted page, trash

**Statistics**:
Local aggregates computed from the SQLite store: monthly totals, daily bar chart, yearly heatmap, content composition, word buckets, top tags/co-occurrence, hour-of-day, todo completion, attachment counts, and sync/storage health.
_Avoid_: Analytics, insights

**Gate**:
A spike or probe that must pass before implementation proceeds.
_Avoid_: Milestone, blocker
