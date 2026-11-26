import { describe, expect, it } from "vitest";
import type { Memo } from "../types";
import { groupByDay } from "./group";

const memo = (id: string, day: string): Memo => ({
  id,
  created_at: `${day}T09:00:00Z`,
  updated_at: `${day}T09:00:00Z`,
  revision: 1,
  snippet: "",
  deleted: false,
  keep_local: false,
  pinned: false,
  local_state: "present",
  body: "",
  tags: [],
  attachments: [],
  day,
});

describe("groupByDay", () => {
  it("groups consecutive memos by local day and preserves order", () => {
    const groups = groupByDay([memo("a", "2026-11-21"), memo("b", "2026-11-21"), memo("c", "2026-11-20")]);
    expect(groups.map((g) => g.day)).toEqual(["2026-11-21", "2026-11-20"]);
    expect(groups[0].memos.map((m) => m.id)).toEqual(["a", "b"]);
    expect(groups[1].memos.map((m) => m.id)).toEqual(["c"]);
  });

  it("returns no groups for an empty timeline", () => {
    expect(groupByDay([])).toEqual([]);
  });
});
