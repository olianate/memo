import { describe, expect, it } from "vitest";
import type { Block, Span } from "../types";
import { blockText, blocksFromMarkdown } from "./markdown";
import suite from "../test/markdown-suite.json";

interface SuiteBlock {
  type: string;
  text?: string;
  level?: number;
  checked?: boolean;
  language?: string;
  alt?: string;
  attachment_id?: string;
  spans?: Span[];
}

function shape(block: Block) {
  return {
    type: block.type,
    text: blockText(block),
    level: block.level ?? 0,
    checked: block.checked ?? false,
    language: block.language ?? "",
    alt: block.alt ?? "",
    attachment_id: block.attachment_id ?? "",
  };
}

function expected(block: SuiteBlock) {
  return {
    type: block.type,
    text: block.text ?? "",
    level: block.level ?? 0,
    checked: block.checked ?? false,
    language: block.language ?? "",
    alt: block.alt ?? "",
    attachment_id: block.attachment_id ?? "",
  };
}

// The same JSON suite runs against internal/domain/markdown in Go. If a case
// changes here, the sync projection changes with it.
describe("shared markdown block suite", () => {
  const cases = suite.cases as Array<{ name: string; body: string; blocks: SuiteBlock[] }>;
  for (const testCase of cases) {
    it(testCase.name, () => {
      const blocks = blocksFromMarkdown(testCase.body);
      expect(blocks.map(shape)).toEqual(testCase.blocks.map(expected));
      testCase.blocks.forEach((want, index) => {
        if (want.spans) expect(blocks[index].spans).toEqual(want.spans);
      });
    });
  }
});
