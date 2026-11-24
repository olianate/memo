import React from "react";
import type { Block, Span } from "../types";

export function escapeHtml(text: string): string {
  return text
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

export function spanToHtml(span: Span): string {
  let inner = escapeHtml(span.text).replaceAll("\n", "<br>");
  if (span.code) inner = `<code>${inner}</code>`;
  if (span.bold) inner = `<strong>${inner}</strong>`;
  if (span.italic) inner = `<em>${inner}</em>`;
  if (span.strike) inner = `<s>${inner}</s>`;
  if (span.link) inner = `<a href="${escapeHtml(span.link)}" rel="noreferrer">${inner}</a>`;
  return inner;
}

export function spansToHtml(spans: Span[] | null | undefined): string {
  return (spans ?? []).map(spanToHtml).join("");
}

function pushSpan(out: Span[], text: string, marks: Omit<Span, "text">) {
  if (!text) return;
  const last = out[out.length - 1];
  if (
    last &&
    Boolean(last.bold) === Boolean(marks.bold) &&
    Boolean(last.italic) === Boolean(marks.italic) &&
    Boolean(last.strike) === Boolean(marks.strike) &&
    Boolean(last.code) === Boolean(marks.code) &&
    (last.link ?? "") === (marks.link ?? "")
  ) {
    last.text += text;
    return;
  }
  out.push({ text, ...marks });
}

const BLOCK_TAGS = new Set(["DIV", "P", "LI", "H1", "H2", "H3", "BLOCKQUOTE"]);

/** Converts edited contentEditable DOM into flat inline spans. */
export function spansFromDOM(root: HTMLElement): Span[] {
  const out: Span[] = [];
  const walk = (node: Node, marks: Omit<Span, "text">) => {
    if (node.nodeType === Node.TEXT_NODE) {
      pushSpan(out, node.textContent ?? "", marks);
      return;
    }
    if (!(node instanceof HTMLElement)) return;
    const tag = node.tagName;
    if (tag === "BR") {
      pushSpan(out, "\n", marks);
      return;
    }
    const next: Omit<Span, "text"> = { ...marks };
    if (tag === "B" || tag === "STRONG" || node.style.fontWeight === "bold" || node.style.fontWeight === "700") next.bold = true;
    if (tag === "I" || tag === "EM" || node.style.fontStyle === "italic") next.italic = true;
    if (tag === "S" || tag === "DEL" || tag === "STRIKE" || node.style.textDecoration.includes("line-through")) next.strike = true;
    if (tag === "CODE") next.code = true;
    if (tag === "A") next.link = node.getAttribute("href") ?? undefined;
    if (BLOCK_TAGS.has(tag) && out.length > 0 && !out[out.length - 1].text.endsWith("\n")) {
      pushSpan(out, "\n", marks);
    }
    node.childNodes.forEach((child) => walk(child, next));
  };
  root.childNodes.forEach((child) => walk(child, {}));
  // Trim a trailing newline introduced by a wrapped block.
  while (out.length > 0) {
    const last = out[out.length - 1];
    last.text = last.text.replace(/\n$/, "");
    if (last.text) break;
    out.pop();
  }
  return out;
}

interface InlineStyle {
  bold?: boolean;
  italic?: boolean;
  strike?: boolean;
  code?: boolean;
  link?: string;
}

/** Parses the supported inline Markdown subset, used when a block blurs. */
export function inlineMarkdownToSpans(source: string): Span[] {
  const out: Span[] = [];
  const parse = (text: string, style: InlineStyle) => {
    let buffer = "";
    const flush = () => {
      if (buffer) {
        pushSpan(out, buffer, style);
        buffer = "";
      }
    };
    for (let i = 0; i < text.length; ) {
      const rest = text.slice(i);
      if (rest[0] === "\\" && rest.length > 1) {
        buffer += rest[1];
        i += 2;
        continue;
      }
      if (rest[0] === "`") {
        const end = rest.indexOf("`", 1);
        if (end > 0) {
          flush();
          parse(rest.slice(1, end), { ...style, code: true });
          i += end + 1;
          continue;
        }
      }
      if (rest[0] === "[" ) {
        const close = rest.indexOf("](");
        if (close > 0) {
          const urlEnd = rest.indexOf(")", close + 2);
          if (urlEnd > 0) {
            flush();
            parse(rest.slice(1, close), { ...style, link: rest.slice(close + 2, urlEnd) });
            i += urlEnd + 1;
            continue;
          }
        }
      }
      if (rest.startsWith("**") || rest.startsWith("__")) {
        const marker = rest.slice(0, 2);
        const end = rest.indexOf(marker, 2);
        if (end > 0) {
          flush();
          parse(rest.slice(2, end), { ...style, bold: true });
          i += end + 2;
          continue;
        }
      }
      if (rest.startsWith("~~")) {
        const end = rest.indexOf("~~", 2);
        if (end > 0) {
          flush();
          parse(rest.slice(2, end), { ...style, strike: true });
          i += end + 2;
          continue;
        }
      }
      if (rest[0] === "*" || rest[0] === "_") {
        const marker = rest[0];
        const end = rest.indexOf(marker, 1);
        const before = i > 0 ? text[i - 1] : "";
        const after = end >= 0 && end + 1 < text.length ? text[end + 1] : "";
        const wordBoundary = marker === "_" && /[\p{L}\p{N}]/u.test(before + after);
        if (end > 0 && !wordBoundary) {
          flush();
          parse(rest.slice(1, end), { ...style, italic: true });
          i += end + 1;
          continue;
        }
      }
      buffer += rest[0];
      i += 1;
    }
    flush();
  };
  parse(source, {});
  return out;
}

/** Removes a plain-text range while preserving the inline marks around it. */
export function removeSpanRange(spans: Span[], start: number, end: number): Span[] {
  const full = spans.map((span) => span.text).join("");
  // Collapse the double space left when the token sat mid-line.
  if (start > 0 && end < full.length && full[start - 1] === " " && full[end] === " ") {
    end += 1;
  }
  const cut: Span[] = [];
  let offset = 0;
  for (const span of spans) {
    const length = span.text.length;
    const spanStart = offset;
    offset += length;
    if (spanStart + length <= start || spanStart >= end) {
      cut.push({ ...span });
      continue;
    }
    const localStart = Math.max(0, start - spanStart);
    const localEnd = Math.min(length, end - spanStart);
    const before = span.text.slice(0, localStart);
    const after = span.text.slice(localEnd);
    if (before) cut.push({ ...span, text: before });
    if (after) cut.push({ ...span, text: after });
  }
  const merged: Span[] = [];
  for (const span of cut) {
    if (!span.text) continue;
    const last = merged[merged.length - 1];
    if (
      last &&
      Boolean(last.bold) === Boolean(span.bold) &&
      Boolean(last.italic) === Boolean(span.italic) &&
      Boolean(last.strike) === Boolean(span.strike) &&
      Boolean(last.code) === Boolean(span.code) &&
      (last.link ?? "") === (span.link ?? "")
    ) {
      last.text += span.text;
      continue;
    }
    merged.push(span);
  }
  return merged;
}

/** Accepts http(s), mailto, and bare domains; rejects other schemes. */
export function safeLinkUrl(raw: string): string | null {
  const value = raw.trim();
  if (!value) return null;
  if (/^(https?:|mailto:)/i.test(value)) return value;
  if (/^[a-z][a-z0-9+.-]*:/i.test(value)) return null;
  if (/^[\w-]+(\.[\w-]+)+([/?#].*)?$/i.test(value)) return `https://${value}`;
  return null;
}

export function InlineSpans({
  spans,
  onOpenLink,
}: {
  spans?: Span[] | null;
  onOpenLink?: (url: string) => void;
}) {
  return (
    <>
      {(spans ?? []).map((span, index) => {
        let node: React.ReactNode = span.text;
        if (span.code) node = <code>{node}</code>;
        if (span.bold) node = <strong>{node}</strong>;
        if (span.italic) node = <em>{node}</em>;
        if (span.strike) node = <s>{node}</s>;
        if (span.link) {
          node = (
            <a
              href={span.link}
              title={span.link}
              onClick={(event) => {
                event.preventDefault();
                event.stopPropagation();
                if (onOpenLink) onOpenLink(span.link as string);
              }}
              rel="noreferrer"
            >
              {node}
            </a>
          );
        }
        return <React.Fragment key={index}>{node}</React.Fragment>;
      })}
    </>
  );
}

export function blockText(block: Block): string {
  return (block.spans ?? []).map((s) => s.text).join("");
}

/** Renders a memo body's text blocks for a timeline card or sheet preview. */
export function BlockPreview({
  blocks,
  limit = 6,
  onOpenLink,
}: {
  blocks: Block[] | null;
  limit?: number;
  onOpenLink?: (url: string) => void;
}) {
  const visible = (blocks ?? []).slice(0, limit);
  return (
    <>
      {visible.map((block) => {
        if (block.type === "divider") return <hr key={block.id} className="md-divider" />;
        if (block.type === "image" || block.type === "file") return null;
        const content = <InlineSpans spans={block.spans} onOpenLink={onOpenLink} />;
        switch (block.type) {
          case "heading":
            return (
              <div key={block.id} className={`md-heading md-h${block.level ?? 1}`}>
                {content}
              </div>
            );
          case "bullet":
            return (
              <div key={block.id} className="md-list">
                <span className="md-bullet">•</span>
                <span>{content}</span>
              </div>
            );
          case "ordered":
            return (
              <div key={block.id} className="md-list">
                <span className="md-bullet">1.</span>
                <span>{content}</span>
              </div>
            );
          case "todo":
            return (
              <div key={block.id} className={`md-list ${block.checked ? "is-done" : ""}`}>
                <span className="md-bullet">{block.checked ? "☑" : "☐"}</span>
                <span>{content}</span>
              </div>
            );
          case "quote":
            return (
              <blockquote key={block.id} className="md-quote">
                {content}
              </blockquote>
            );
          case "code":
            return (
              <pre key={block.id} className="md-code">
                <code>{blockText(block)}</code>
              </pre>
            );
          default:
            return (
              <p key={block.id} className="md-paragraph">
                {content}
              </p>
            );
        }
      })}
    </>
  );
}
