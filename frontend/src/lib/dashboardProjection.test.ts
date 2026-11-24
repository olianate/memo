import { describe, expect, it } from "vitest";
import { buildDashboard } from "./dashboardProjection";
import { memo } from "../test/fixtures";
import type { Tag } from "../types";

const tag = (name: string): Tag => ({ id: `tag-${name}`, name, normalized: name.toLowerCase(), count: 0 });

describe("buildDashboard", () => {
  it("projects month aggregates from memo facts", () => {
    const memos = [
      memo({
        id: "m1",
        day: "2026-11-05",
        body: "- [x] 完成\n- 待办\n\n正文",
        tags: [tag("工作")],
        notion_page_id: "page-1",
        attachments: [
          {
            id: "a1",
            memo_id: "m1",
            name: "图.png",
            mime: "image/png",
            kind: "image",
            bytes: 1024,
            present: true,
            has_thumb: false,
          },
        ],
      }),
      memo({
        id: "m2",
        day: "2026-11-06",
        body: "另一条 #笔记",
        tags: [tag("工作"), tag("笔记")],
      }),
      memo({ id: "m3", day: "2026-11-07", deleted: true, body: "已删除" }),
    ];

    const result = buildDashboard(memos, "2026-11", 2);

    expect(result.month).toBe("2026-11");
    expect(result.daily).toHaveLength(30);
    expect(result.year).toHaveLength(365);
    expect(result.totals).toMatchObject({ notes: 2, attachments: 1, active_days: 2 });
    expect(result.totals.words).toBeGreaterThan(0);
    expect(result.todos).toEqual({ total: 1, done: 1 });
    expect(result.top_tags.map((t) => [t.name, t.count])).toEqual([
      ["工作", 2],
      ["笔记", 1],
    ]);
    expect(result.co_occurrence).toEqual([{ a: "工作", b: "笔记", count: 1 }]);
    expect(result.attachments).toEqual({ images: 1, files: 0, bytes: 1024 });
    expect(result.health).toEqual({ local_only: 1, synced: 1, evicted: 0, pending: 2 });
  });

  it("returns zeroed buckets for an empty month", () => {
    const result = buildDashboard([], "2026-11", 0);
    expect(result.totals).toEqual({ notes: 0, words: 0, tags: 0, attachments: 0, active_days: 0 });
    expect(result.composition.every((row) => row.count === 0)).toBe(true);
    expect(result.word_buckets.every((row) => row.count === 0)).toBe(true);
    expect(result.hours.every((row) => row.count === 0)).toBe(true);
  });
});
