# Memo bodies are Markdown; Notion blocks are an adapter

Status: accepted
Date: 2026-11-24
Supersedes: [0004](0004-block-ast-is-canonical.md)

A memo body is the Markdown the user typed. It is stored verbatim and is the only
body source of truth. The app does not persist a block AST and the editor does
not present a Notion-style block model. Notion is an optional archive, so its
block model belongs at the sync boundary and nowhere else.

The editor is a single Markdown surface with Obsidian-style live preview: the
line holding the caret is raw Markdown, committed lines render, and clicking a
rendered line returns it to source. There is no separate whole-document source
mode. Supported syntax is the basic subset:
paragraphs, ATX headings, bullet/ordered/todo lists, quotes, fenced code,
dividers, inline bold/italic/strikethrough/code/links, and attachment links
(`![alt](attachment://id)`, `[name](attachment://id)`). Everything else is kept
and rendered as literal text.

Derived state is computed from the Markdown on write: `body_text` for search and
snippets, `block_stats` for statistics, the FTS row, and the content hash. The
`Block` type survives only as the internal projection that the Notion adapter
produces and consumes: push parses Markdown into Notion blocks; pull serializes
every Notion block back into Markdown. A supported block becomes its Markdown
form; an unsupported block that carries text becomes a plain-text paragraph; a
block with no Markdown representation is preserved verbatim as a ` ```notion `
fenced block containing the original block JSON, so the raw source stays
visible and nothing is dropped. Attachments upload to Notion and download from
Notion only at that boundary.

## Consequences

- Saving never rewrites the body: no normalization, no re-serialization, no
  whitespace changes. Raw mode is byte-preserving.
- Notion round-trips are lossy for syntax outside the supported subset. Notion is
  an archive, not a second editor; the UI and docs must not promise fidelity
  there.
- Pull never drops a block. Unrepresentable blocks survive as readable raw
  source, and the frontend shows that source instead of hiding it.
- Push recognizes a ` ```notion ` fence and restores the original block when the
  API can create that type; otherwise the raw source stays as a code block.
- The frontend receives `body` Markdown and parses it locally for display. There
  is no backend parse/serialize call in the editor loop.
- Tags stay first-class metadata (ADR 0005); the `#tag` quick-entry shortcut
  removes the token from the Markdown it consumes.
- Statistics parse the same supported subset; unparseable input degrades to
  plain text instead of failing a save.
- Storage schema replaces `body_json` with `body_md`. There is no migration or
  compatibility shim for the block-AST schema, and no dual-write: a profile from
  the block-AST version must be reset.
- Markdown tables, HTML, math, footnotes, embeds, and nested-list nesting are not
  rendered; they remain literal text and are flattened when pushed to Notion.
