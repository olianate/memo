import type { Dashboard, Memo, SearchResult } from "../types";

export function memo(overrides: Partial<Memo> & { id: string; day: string }): Memo {
  return {
    created_at: overrides.created_at ?? `${overrides.day}T09:00:00Z`,
    updated_at: overrides.updated_at ?? `${overrides.day}T09:00:00Z`,
    revision: 1,
    snippet: "",
    deleted: false,
    keep_local: false,
    local_state: "present",
    blocks: [{ id: "b1", type: "paragraph", spans: [{ text: "示例正文" }] }],
    tags: [],
    attachments: [],
    ...overrides,
  };
}

export function dashboard(overrides: Partial<Dashboard> = {}): Dashboard {
  const month = "2026-11";
  return {
    month,
    totals: { notes: 3, words: 120, tags: 2, attachments: 1, active_days: 2 },
    daily: [
      { date: `${month}-01`, count: 1 },
      { date: `${month}-02`, count: 2 },
    ],
    year: Array.from({ length: 365 }, (_, i) => ({ date: `2026-01-${String((i % 28) + 1).padStart(2, "0")}`, count: i % 3 })),
    composition: [
      { type: "paragraph", count: 4 },
      { type: "heading", count: 1 },
      { type: "list", count: 2 },
      { type: "todo", count: 1 },
      { type: "quote", count: 0 },
      { type: "code", count: 0 },
      { type: "attachment", count: 1 },
    ],
    word_buckets: [
      { label: "0–50", count: 2 },
      { label: "51–100", count: 1 },
      { label: "101–200", count: 0 },
      { label: "201–400", count: 0 },
      { label: "401–800", count: 0 },
      { label: "800+", count: 0 },
    ],
    top_tags: [{ id: "t1", name: "工作", normalized: "工作", count: 2 }],
    co_occurrence: [{ a: "工作", b: "memo", count: 1 }],
    hours: Array.from({ length: 24 }, (_, hour) => ({ hour, count: hour === 9 ? 2 : 0 })),
    todos: { total: 2, done: 1 },
    attachments: { images: 1, files: 0, bytes: 1024 },
    health: { local_only: 1, synced: 2, evicted: 0, pending: 0 },
    ...overrides,
  };
}

export function searchResult(overrides: Partial<SearchResult> = {}): SearchResult {
  return {
    memo: memo({ id: "m1", day: "2026-11-21" }),
    before: "这是 ",
    match: "产品",
    after: " 相关的笔记",
    ...overrides,
  };
}
