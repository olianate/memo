# Tags are metadata, not body text

Status: accepted
Date: 2026-11-21

The editor is a single live-preview Markdown surface. Keeping `#tag` in the Markdown body would force body rewrites on rename. We decided that tags are first-class metadata in `memo_tags`; `#tag` (no space) is an editor shortcut that adds a tag chip and removes the token from the body. The body never retains tag tokens and tags appear in a separate chip row above the editor. Notion projects tags to a `multi_select` property. Rename and merge update metadata only.

## Consequences

- Tag rename/merge is cheap and does not rewrite memo bodies or bump revisions.
- Pasting Markdown does not auto-create tags; the user adds them through the chip row or the live shortcut.
- Search indexes `body_text` and `tags_text` separately.
- The quick-entry parser must treat `# Heading` (space after `#`) as a Markdown heading, not a tag.
