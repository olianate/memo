# The block AST is canonical; Markdown is a serialization

Status: superseded by [0006](0006-markdown-is-canonical.md)
Date: 2026-11-21

The editor must support basic Markdown with two modes: live rendered editing and raw Markdown source. Storing Markdown as the only source would make inline marks, list nesting, attachments, and the Notion block mapping lossy. We decided that the existing block AST (`body_json`) remains the canonical body, while Markdown is a serialization used by raw mode and export. Switching from live to source serializes the AST; switching back parses the supported Markdown subset. Unsupported syntax stays literal text.

## Consequences

- Markdown parser and serializer need round-trip tests for the supported subset.
- Switching modes may normalize whitespace and unsupported syntax; the UI should not promise byte-for-byte Markdown fidelity.
- Notion mapping stays structural: headings, lists, quotes, code, dividers, images, and files map to blocks; inline marks map to rich-text annotations.
- Attachments are blocks with `attachment://id` references in raw mode, not external URLs.
