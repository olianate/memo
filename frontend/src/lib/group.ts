import type { Memo } from "../types";

export interface MemoDay {
  day: string;
  memos: Memo[];
}

/** Groups already-sorted memos by local day, preserving order. */
export function groupByDay(memos: Memo[]): MemoDay[] {
  const groups: MemoDay[] = [];
  let current: MemoDay | null = null;
  for (const memo of memos) {
    const day = memo.day || memo.created_at.slice(0, 10);
    if (!current || current.day !== day) {
      current = { day, memos: [] };
      groups.push(current);
    }
    current.memos.push(memo);
  }
  return groups;
}
