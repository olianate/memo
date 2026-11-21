# Tags are metadata, not body text

Status: accepted
Date: 2026-11-21

The editor supports both raw Markdown source and live rendered mode. Keeping `#tag` in the Markdown body would leak tags into source mode and force body rewrites on rename. We decided that tags are first-class metadata in `memo_tags`; `#tag` (no space) is a live-mode shortcut that adds a tag chip and removes the token from the body. Raw source never retains tag tokens and shows tags in a separate chip row. Notion projects tags to a `multi_select` property. Rename and merge update metadata only.

## Consequences

- Tag rename/merge is cheap and does not rewrite memo bodies or bump revisions.
- Pasting Markdown does not auto-create tags; the user adds them through the chip row or the live shortcut.
- Search indexes `body_text` and `tags_text` separately.
- The quick-entry parser must treat `# Heading` (space after `#`) as a Markdown heading, not a tag.
