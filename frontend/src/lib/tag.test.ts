import { describe, expect, it } from "vitest";
import { extractTagToken, matchInputRule, removeTagToken } from "../lib/tag";

describe("tag quick entry", () => {
  it("extracts a tag immediately before the caret", () => {
    expect(extractTagToken("hello #work", 11)).toMatchObject({ token: "work", start: 6, end: 11 });
    expect(extractTagToken("中文 #工作", 6)).toMatchObject({ token: "工作" });
    expect(extractTagToken("#work", 5)).toMatchObject({ token: "work" });
  });

  it("does not treat headings, C#, or mid-word hashes as tags", () => {
    expect(extractTagToken("# Heading", 1)).toBeNull();
    expect(extractTagToken("C#", 2)).toBeNull();
    expect(extractTagToken("hello#tag", 9)).toBeNull();
    expect(extractTagToken("acme #tag-one ", 14)).toBeNull();
    expect(extractTagToken("`#code", 6)).toBeNull();
  });

  it("removes the committed token and tidies the space", () => {
    const text = "hello #work world";
    expect(removeTagToken(text, 6, 11)).toBe("hello world");
    expect(removeTagToken("#work", 0, 5)).toBe("");
  });
});

describe("markdown input rules", () => {
  it.each([
    ["#", { type: "heading", level: 1 }],
    ["##", { type: "heading", level: 2 }],
    ["###", { type: "heading", level: 3 }],
    ["-", { type: "bullet" }],
    ["1.", { type: "ordered" }],
    ["[ ]", { type: "todo" }],
    [">", { type: "quote" }],
    ["```", { type: "code" }],
  ])("maps %s to a block type", (marker, expected) => {
    expect(matchInputRule(marker)).toEqual(expected);
  });

  it("leaves ordinary text alone", () => {
    expect(matchInputRule("hello")).toBeNull();
    expect(matchInputRule("# heading text")).toBeNull();
  });
});
