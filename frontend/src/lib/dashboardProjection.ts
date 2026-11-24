import type { Dashboard, Memo, Tag } from "../types";
import { analyzeBody } from "./bodyProjection";

const dayKey = (d: Date) => {
  const x = new Date(d);
  return `${x.getFullYear()}-${String(x.getMonth() + 1).padStart(2, "0")}-${String(x.getDate()).padStart(2, "0")}`;
};

const tagFor = (name: string, count: number): Tag => ({
  id: `tag-${name}`,
  name,
  normalized: name.toLowerCase(),
  count,
});

/** Project dashboard aggregates from the memo facts the demo adapter owns. */
export function buildDashboard(memos: Memo[], month: string, pending: number): Dashboard {
  const [year, mon] = month.split("-").map(Number);
  const start = new Date(year, mon - 1, 1);
  const end = new Date(year, mon, 1);
  const inMonth = memos.filter(
    (m) => !m.deleted && new Date(m.created_at) >= start && new Date(m.created_at) < end,
  );
  const daysInMonth = new Date(year, mon, 0).getDate();
  const daily = Array.from({ length: daysInMonth }, (_, i) => {
    const date = `${month}-${String(i + 1).padStart(2, "0")}`;
    return { date, count: inMonth.filter((m) => m.day === date).length };
  });
  const yearMap = new Map<string, number>();
  const yearStart = new Date();
  yearStart.setDate(yearStart.getDate() - 364);
  yearStart.setHours(0, 0, 0, 0);
  for (const m of memos.filter((x) => !x.deleted)) {
    const d = new Date(m.created_at);
    if (d >= yearStart) yearMap.set(m.day, (yearMap.get(m.day) ?? 0) + 1);
  }
  const yearDays: Dashboard["year"] = [];
  for (let i = 0; i < 365; i++) {
    const d = new Date(yearStart);
    d.setDate(d.getDate() + i);
    const key = dayKey(d);
    yearDays.push({ date: key, count: yearMap.get(key) ?? 0 });
  }
  const compositionMap: Record<string, number> = {};
  const hours = Array.from({ length: 24 }, (_, hour) => ({ hour, count: 0 }));
  let words = 0;
  let todoTotal = 0;
  let todoDone = 0;
  for (const m of inMonth) {
    const stats = analyzeBody(m.body);
    words += stats.words;
    hours[new Date(m.created_at).getHours()].count += 1;
    for (const [key, count] of Object.entries(stats.composition)) {
      compositionMap[key] = (compositionMap[key] ?? 0) + count;
    }
    todoTotal += stats.todoTotal;
    todoDone += stats.todoDone;
  }
  const tagCounts = new Map<string, number>();
  for (const m of inMonth) for (const t of m.tags ?? []) tagCounts.set(t.name, (tagCounts.get(t.name) ?? 0) + 1);
  const topTags = [...tagCounts.entries()]
    .map(([name, count]) => tagFor(name, count))
    .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name));
  const pairs = new Map<string, number>();
  for (const m of inMonth) {
    const tags = [...new Set((m.tags ?? []).map((t) => t.normalized))].sort();
    for (let i = 0; i < tags.length; i++) {
      for (let j = i + 1; j < tags.length; j++) {
        const key = `${tags[i]}|${tags[j]}`;
        pairs.set(key, (pairs.get(key) ?? 0) + 1);
      }
    }
  }
  const attachments = inMonth.flatMap((m) => m.attachments ?? []);
  const activeDays = new Set(inMonth.map((m) => m.day)).size;
  const wordsBuckets = [0, 0, 0, 0, 0, 0];
  for (const m of inMonth) {
    const w = analyzeBody(m.body).words;
    if (w <= 50) wordsBuckets[0]++;
    else if (w <= 100) wordsBuckets[1]++;
    else if (w <= 200) wordsBuckets[2]++;
    else if (w <= 400) wordsBuckets[3]++;
    else if (w <= 800) wordsBuckets[4]++;
    else wordsBuckets[5]++;
  }
  const active = memos.filter((m) => !m.deleted);
  return {
    month,
    totals: {
      notes: inMonth.length,
      words,
      tags: topTags.length,
      attachments: attachments.length,
      active_days: activeDays,
    },
    daily,
    year: yearDays,
    composition: ["paragraph", "heading", "list", "todo", "quote", "code", "attachment"].map((type) => ({
      type,
      count: compositionMap[type] ?? 0,
    })),
    word_buckets: ["0–50", "51–100", "101–200", "201–400", "401–800", "800+"].map((label, i) => ({
      label,
      count: wordsBuckets[i],
    })),
    top_tags: topTags,
    co_occurrence: [...pairs.entries()]
      .map(([key, count]) => {
        const [a, b] = key.split("|");
        return { a, b, count };
      })
      .sort((a, b) => b.count - a.count),
    hours,
    todos: { total: todoTotal, done: todoDone },
    attachments: {
      images: attachments.filter((a) => a.kind === "image").length,
      files: attachments.filter((a) => a.kind === "file").length,
      bytes: attachments.reduce((n, a) => n + a.bytes, 0),
    },
    health: {
      local_only: active.filter((m) => !m.notion_page_id).length,
      synced: active.filter((m) => Boolean(m.notion_page_id)).length,
      evicted: active.filter((m) => m.local_state === "evicted").length,
      pending,
    },
  };
}
