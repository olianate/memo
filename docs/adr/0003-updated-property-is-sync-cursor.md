# Use a client-managed Updated property as the sync cursor

Status: accepted
Date: 2026-11-21

Notion's page `last_edited_time` does not change when a child block is edited (confirmed by the 2026-11-21 probe), and the block-children API has no `updated_since` query. We decided that every local edit writes a client timestamp to the `Updated` date property, and that pull queries filter on `Updated` with a 10-minute overlap. `last_edited_time` remains a secondary cursor only. This keeps body edits visible to other devices without full scans.

## Consequences

- The app must write `Updated` on every edit, including tag-only renames and image changes.
- Clock skew between devices is possible; conflict resolution still uses `Revision` first, then `Updated`, then `Device`.
- A Notion-side body edit that bypasses the app is not detected; the app is the writer (ADR-0001).
- If `Updated` filtering proves too coarse under minute rounding, the fallback is a periodic full metadata reconcile; do not switch back to `last_edited_time` alone.
