import { describe, expect, it } from "vitest";
import { memoPreview, PREVIEW_LINE_LIMIT } from "./preview";
import { memo } from "../test/fixtures";
import type { Attachment } from "../types";

function attachment(overrides: Partial<Attachment> & { id: string; kind: "image" | "file" }): Attachment {
  return {
    memo_id: "m1",
    name: overrides.id,
    mime: "text/plain",
    bytes: 10,
    present: true,
    has_thumb: false,
    ...overrides,
  };
}

describe("memoPreview", () => {
  it("parses a body once and reuses the block list", () => {
    const entry = memo({ id: "m1", day: "2026-11-21", body: "# 标题\n\n一段正文" });
    const first = memoPreview(entry);
    const second = memoPreview(entry);
    expect(first.blocks).toBe(second.blocks);
    expect(first.blocks.map((b) => b.type)).toEqual(["heading", "paragraph"]);
  });

  it("keeps distinct block lists for distinct bodies", () => {
    const a = memoPreview(memo({ id: "a", day: "2026-11-21", body: "第一段" }));
    const b = memoPreview(memo({ id: "b", day: "2026-11-21", body: "第二段" }));
    expect(a.blocks).not.toBe(b.blocks);
    expect(a.blocks[0].spans?.[0]?.text).toBe("第一段");
  });

  it("returns no blocks for an evicted body", () => {
    const preview = memoPreview(memo({ id: "m1", day: "2026-11-21", body: "", local_state: "evicted" }));
    expect(preview.blocks).toEqual([]);
    expect(preview.long).toBe(false);
  });

  it("hides evicted attachments and splits images from files", () => {
    const preview = memoPreview(
      memo({
        id: "m1",
        day: "2026-11-21",
        attachments: [
          attachment({ id: "img-1", kind: "image" }),
          attachment({ id: "img-2", kind: "image", present: false }),
          attachment({ id: "file-1", kind: "file" }),
          attachment({ id: "file-2", kind: "file", present: false }),
        ],
      }),
    );
    expect(preview.images.map((a) => a.id)).toEqual(["img-1"]);
    expect(preview.files.map((a) => a.id)).toEqual(["file-1"]);
  });

  it("marks a body long only past the line limit", () => {
    const lines = (count: number) => Array.from({ length: count }, (_, i) => `第 ${i} 行`).join("\n\n");
    expect(memoPreview(memo({ id: "m1", day: "2026-11-21", body: lines(PREVIEW_LINE_LIMIT) })).long).toBe(false);
    expect(memoPreview(memo({ id: "m1", day: "2026-11-21", body: lines(PREVIEW_LINE_LIMIT + 1) })).long).toBe(true);
  });
});
