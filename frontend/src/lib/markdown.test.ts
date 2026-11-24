import { describe, expect, it } from "vitest";
import { blocksFromMarkdown, inlineMarkdownToSpans, removeSpanRange, spansToHtml } from "./markdown";

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

  it("parses angle-bracket autolinks and strips br tags", () => {
    const spans = inlineMarkdownToSpans("before <https://example.com/a\\_b> now<br>next");
    expect(spans).toContainEqual({ text: "https://example.com/a_b", link: "https://example.com/a_b" });
    const text = spans.map((s) => s.text).join("");
    expect(text).not.toContain("<");
    expect(text).toBe("before https://example.com/a_b now next");
  });

  it("parses a GFM table into rows", () => {
    const blocks = blocksFromMarkdown("| a | **b** |\n| --- | --- |\n| 1 | 2 |");
    expect(blocks).toHaveLength(1);
    expect(blocks[0].type).toBe("table");
    expect(blocks[0].rows).toEqual([
      ["a", "**b**"],
      ["1", "2"],
    ]);
  });

  it("renders a standalone pipe row as a one-row table", () => {
    const blocks = blocksFromMarkdown("|← margin →|← border →|\n\nnext");
    expect(blocks[0].type).toBe("table");
    expect(blocks[0].rows).toEqual([["← margin →", "← border →"]]);
    expect(blocks[1].type).toBe("paragraph");
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
