import type { Attachment, Block, Memo } from "../types";
import { blocksFromMarkdown } from "./markdown";

/** Everything the timeline needs to render one memo's body. */
export interface MemoPreview {
  blocks: Block[];
  images: Attachment[];
  files: Attachment[];
  long: boolean;
}

/** The expander appears once a body has more non-empty lines than this. */
export const PREVIEW_LINE_LIMIT = 6;
const MAX_CACHED_BODIES = 200;

// Parsing is keyed by body text, so a re-render reuses the same block list and
// a 500-row timeline parses each distinct body once.
const parsedBodies = new Map<string, Block[]>();

function blocksFor(body: string): Block[] {
  if (!body) return [];
  const cached = parsedBodies.get(body);
  if (cached) {
    parsedBodies.delete(body);
    parsedBodies.set(body, cached);
    return cached;
  }
  const blocks = blocksFromMarkdown(body);
  parsedBodies.set(body, blocks);
  while (parsedBodies.size > MAX_CACHED_BODIES) {
    const oldest = parsedBodies.keys().next();
    if (oldest.done) break;
    parsedBodies.delete(oldest.value);
  }
  return blocks;
}

function presentAttachments(memo: Memo, kind: Attachment["kind"]): Attachment[] {
  return (memo.attachments ?? []).filter((a) => a.kind === kind && a.present);
}

function isLong(body: string): boolean {
  return body.split("\n").filter((line) => line.trim()).length > PREVIEW_LINE_LIMIT;
}

export function memoPreview(memo: Memo): MemoPreview {
  return {
    blocks: blocksFor(memo.body),
    // Evicted attachments keep metadata but no bytes; hide them instead of
    // rendering broken media. The eviction banner explains the state.
    images: presentAttachments(memo, "image"),
    files: presentAttachments(memo, "file"),
    long: isLong(memo.body),
  };
}
