import { describe, expect, it } from "vitest";
import { inlineMarkdownToSpans, removeSpanRange, spansToHtml } from "./markdown";

describe("inline markdown", () => {
  it("parses bold, italic, strike, code and links", () => {
    const spans = inlineMarkdownToSpans("**bold** *italic* ~~gone~~ `code` [link](https://example.com)");
    expect(spans).toEqual([
      { text: "bold", bold: true },
      { text: " " },
      { text: "italic", italic: true },
      { text: " " },
      { text: "gone", strike: true },
      { text: " " },
      { text: "code", code: true },
      { text: " " },
      { text: "link", link: "https://example.com" },
    ]);
  });

  it("keeps snake_case and C# literal", () => {
    const spans = inlineMarkdownToSpans("snake_case and C# remain");
    expect(spans.map((s) => s.text).join("")).toBe("snake_case and C# remain");
    expect(spans.some((s) => s.italic)).toBe(false);
  });

  it("escapes HTML when rendering into the editor", () => {
    expect(spansToHtml([{ text: "<script>" }])).toBe("&lt;script&gt;");
  });

  it("removes a tag range without losing inline marks", () => {
    const spans = [
      { text: "hello ", bold: true },
      { text: "#work", italic: true },
      { text: " world", bold: true },
    ];
    expect(removeSpanRange(spans, 6, 11)).toEqual([{ text: "hello world", bold: true }]);
  });
});
