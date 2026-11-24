# Notion is an optional archive, not the primary store

Status: accepted
Date: 2026-11-20

The app is local-first and must support a sticky capture window, offline capture, Chinese full-text search, and optional multi-device sync through the user's Notion workspace. Notion's API has no body search, a 10,000-row query cap, invisible trashed pages, one-hour file URLs, and no idempotency key or ETag. We decided to keep the local SQLite store as the working copy and source of truth for the UI, with Notion as an optional archive and sync transport. After a memo is confirmed synced, its body and images may be evicted locally after the retention period; metadata and the local FTS index remain. Deletion is represented by an app-managed `Status=deleted` property, not by trashing the page.

## Consequences

- Search is always local; Notion's title-only search is never the search path.
- The app must own a resumable backfill, a trash sweep, and an image cache.
- A privacy-focused user must understand that the FTS index retains text and that Notion cannot revoke uploaded files.
- Notion-side body edits were not pulled in v1; ADR 0006 later added Markdown pull, so the app is no longer the only writer.
