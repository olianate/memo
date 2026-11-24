import type { Attachment, Block } from "../types";
import { countWords } from "./format";
import { blockText, blocksFromMarkdown } from "./markdown";

// One projection of a memo body. The demo adapter stores memos, but it does not
// get its own copy of the derivation rules: these functions stay pinned to the
// Go projection through the shared body fixture.

export interface BodyAnalysis {
  words: number;
  characters: number;
  composition: Record<string, number>;
  todoTotal: number;
  todoDone: number;
}

export function markdownLines(body: string): string[] {
  return body.replace(/\r\n/g, "\n").split("\n");
}

export function bodyText(body: string): string {
  return blocksFromMarkdown(body)
    .filter((b) => b.type !== "divider" && b.type !== "image" && b.type !== "file")
    .map(blockText)
    .join("\n");
}

export function snippetOf(body: string): string {
  const text = bodyText(body).replace(/\s+/g, " ").trim();
  const chars = Array.from(text);
  return chars.length <= 200 ? text : `${chars.slice(0, 200).join("")}…`;
}

function countBlock(composition: Record<string, number>, block: Block) {
  switch (block.type) {
    case "heading":
      composition.heading += 1;
      break;
    case "bullet":
    case "ordered":
      composition.list += 1;
      break;
    case "todo":
      composition.todo += 1;
      break;
    case "quote":
      composition.quote += 1;
      break;
    case "code":
      composition.code += 1;
      break;
    case "image":
    case "file":
      composition.attachment += 1;
      break;
    case "divider":
      break;
    default:
      composition.paragraph += 1;
  }
}

export function analyzeBody(body: string): BodyAnalysis {
  const composition: Record<string, number> = {
    paragraph: 0,
    heading: 0,
    list: 0,
    todo: 0,
    quote: 0,
    code: 0,
    attachment: 0,
  };
  let words = 0;
  let characters = 0;
  let todoTotal = 0;
  let todoDone = 0;
  for (const block of blocksFromMarkdown(body)) {
    const text = blockText(block);
    words += countWords(text);
    characters += Array.from(text).length;
    countBlock(composition, block);
    if (block.type === "todo") {
      todoTotal += 1;
      if (block.checked) todoDone += 1;
    }
  }
  return { words, characters, composition, todoTotal, todoDone };
}

export function attachmentLine(kind: Attachment["kind"], id: string, name: string): string {
  return kind === "image" ? `![${name}](attachment://${id})` : `[${name}](attachment://${id})`;
}

export function appendAttachment(body: string, line: string): string {
  const trimmed = body.replace(/\n+$/, "");
  return trimmed ? `${trimmed}\n\n${line}` : line;
}

export function removeAttachmentLine(body: string, id: string): string {
  return markdownLines(body)
    .filter((line) => {
      const match = line.trim().match(/^!?\[[^\]]*\]\(attachment:\/\/([^)]+)\)$/);
      return !match || match[1] !== id;
    })
    .join("\n");
}
