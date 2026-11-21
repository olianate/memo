const TAG_RUNE = /[\p{L}\p{N}_·-]/u;

function prevIndex(text: string, index: number): number {
  if (index <= 0) return 0;
  const code = text.charCodeAt(index - 1);
  if (code >= 0xdc00 && code <= 0xdfff && index >= 2) return index - 2;
  return index - 1;
}

export interface TagToken {
  token: string;
  start: number;
  end: number;
}

/** Mirrors domain.ExtractTagToken: `#tag` immediately before the caret. */
export function extractTagToken(text: string, caret: number): TagToken | null {
  if (caret < 0 || caret > text.length) return null;
  let start = caret;
  while (start > 0) {
    const prev = prevIndex(text, start);
    const ch = text.slice(prev, start);
    if (ch === "#" || /\s/.test(ch)) break;
    if (!TAG_RUNE.test(ch)) return null;
    start = prev;
  }
  if (start === 0) return null;
  const hashIndex = prevIndex(text, start);
  if (text.slice(hashIndex, start) !== "#") return null;
  start = hashIndex;
  if (start > 0) {
    const before = text.slice(prevIndex(text, start), start);
    if (!/\s|\(|\[|（|【/.test(before)) return null;
  }
  // A token inside an unmatched backtick is inline code, not a tag.
  const backticks = (text.slice(0, start).match(/`/g) ?? []).length;
  if (backticks % 2 === 1) return null;
  const token = text.slice(start + 1, caret);
  if (!token || token.trim() !== token) return null;
  return { token, start, end: caret };
}

/** Removes a committed tag token and tidies the whitespace around it. */
export function removeTagToken(text: string, start: number, end: number): string {
  if (start < 0 || end > text.length || start >= end) return text;
  const before = text.slice(0, start);
  let after = text.slice(end);
  if (before && after && /\s/.test(before[before.length - 1]) && /^[^\S\n]/.test(after)) {
    after = after.slice(1);
  }
  return before + after;
}

export interface InputRule {
  type: "heading" | "bullet" | "ordered" | "todo" | "quote" | "code";
  level?: number;
}

/** Block Markdown shortcuts applied when the marker is followed by a space. */
export function matchInputRule(text: string): InputRule | null {
  switch (text) {
    case "#":
      return { type: "heading", level: 1 };
    case "##":
      return { type: "heading", level: 2 };
    case "###":
      return { type: "heading", level: 3 };
    case "-":
    case "*":
      return { type: "bullet" };
    case "1.":
    case "1)":
      return { type: "ordered" };
    case "[]":
    case "[ ]":
      return { type: "todo" };
    case ">":
      return { type: "quote" };
    case "```":
      return { type: "code" };
    default:
      return null;
  }
}

/** True when the heading marker is the only thing before the caret. */
export function isHeadingMarker(text: string, caret: number): boolean {
  return caret === text.length && /^#{1,3}$/.test(text);
}
