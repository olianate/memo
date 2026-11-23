import type {
  ArchiveDatabasePreview,
  ArchiveTarget,
  Attachment,
  BackupResult,
  Block,
  ConnectRequest,
  ConnectResult,
  Counts,
  Dashboard,
  ErrorReport,
  Filter,
  Memo,
  RestoreProgress,
  SearchResult,
  Settings,
  SyncLog,
  SyncStatus,
  Tag,
  Workspace,
} from "./types";
import type { Backend } from "./backend";

const now = () => new Date();
const iso = (d: Date) => d.toISOString();
const dayKey = (d: Date) => {
  const x = new Date(d);
  return `${x.getFullYear()}-${String(x.getMonth() + 1).padStart(2, "0")}-${String(x.getDate()).padStart(2, "0")}`;
};

function makeBlock(type: Block["type"], text = "", extra: Partial<Block> = {}): Block {
  return {
    id: `b-${Math.random().toString(36).slice(2, 10)}`,
    type,
    spans: text ? [{ text }] : [],
    ...extra,
  };
}

function makeTag(name: string): Tag {
  return { id: `tag-${name}`, name, normalized: name.toLowerCase(), count: 0 };
}

class DemoStore {
  memos: Memo[] = [];
  tagList: Tag[] = [];
  settings: Settings = {
    theme: "light",
    hotkey: "CmdOrCtrl+Shift+Space",
    start_at_login: false,
    retention_days: 30,
    immediate_eviction: false,
    sync_enabled: true,
    sync_paused: false,
    notion_connected: true,
    workspace_name: "演示工作区",
    workspace_file_limit: 5 * 1024 * 1024,
    data_dir: "C:\\Users\\you\\AppData\\Roaming\\memo",
    onboarded: true,
    version: "0.1.0",
  };
  attachments = new Map<string, { data: string; mime: string; thumb?: string }>();
  errors: SyncLog[] = [];
  nextErrorID = 1;
  status: SyncStatus = {
    state: "idle",
    enabled: true,
    paused: false,
    configured: true,
    active: true,
    pending: 0,
    retrying: 0,
    workspace_name: "演示工作区",
    last_success: iso(new Date(Date.now() - 120_000)),
    logs: [],
  };

  constructor() {
    const d = (daysAgo: number, hour: number) => {
      const date = new Date();
      date.setDate(date.getDate() - daysAgo);
      date.setHours(hour, 12, 0, 0);
      return date;
    };
    this.add(
      [
        makeBlock("paragraph", "现在的想法是… 支持 Markdown 的实时渲染与源码模式，标签在正文之外管理。"),
        makeBlock("bullet", "Enter 保存，Shift+Enter 换行"),
        makeBlock("todo", "把 sticky window 做成全局快捷键", { checked: true }),
      ],
      ["产品", "memo"],
      d(0, 9),
    );
    this.add(
      [
        makeBlock("heading", "把 Notion 当归档，而不是实时数据库", { level: 2 }),
        makeBlock("paragraph", "搜索必须留在本地。Notion 只负责长期归档与跨设备同步。"),
        makeBlock("paragraph", "本地保留 30 天后，正文和附件会被清理，但元数据、标签和搜索索引仍在。"),
      ],
      ["产品", "memo", "技术"],
      d(0, 8),
    );
    this.add(
      [
        makeBlock("paragraph", "Wails 的 IME 问题用 PR #6596 修好了。"),
        makeBlock("paragraph", "Windows 11 + 微软拼音通过。macOS 留到发布前再补测。"),
      ],
      ["技术", "todo"],
      d(1, 21),
    );
    this.add(
      [
        makeBlock("paragraph", "给 sticky window 加一个「保留本地」的 pin。"),
        makeBlock("paragraph", "清理正文时不要动它。"),
      ],
      ["产品", "设计"],
      d(1, 16),
    );
    this.add(
      [
        makeBlock("heading", "本周回顾", { level: 1 }),
        makeBlock("paragraph", "写了 4 条笔记，2 个想法进入了实现。"),
        makeBlock("quote", "慢一点，但每天都在写。"),
      ],
      ["生活", "memo"],
      d(3, 22),
    );
    this.add(
      [
        makeBlock("paragraph", "中文搜索要支持 1–2 个字。FTS5 trigram 不够，短查询回退到 LIKE。"),
        makeBlock("code", "SELECT * FROM memos WHERE body_text LIKE '%开会%'", { language: "sql" }),
      ],
      ["技术", "memo"],
      d(5, 10),
    );
    this.recount();
  }

  add(blocks: Block[], tags: string[], created?: Date): Memo {
    const at = created ?? now();
    const tagObjs = tags.map(makeTag);
    const memo: Memo = {
      id: `memo-${Math.random().toString(36).slice(2, 12)}`,
      created_at: iso(at),
      updated_at: iso(at),
      revision: 1,
      snippet: blocks
        .map((b) => (b.spans ?? []).map((s) => s.text).join(""))
        .join(" ")
        .slice(0, 200),
      deleted: false,
      keep_local: false,
      local_state: "present",
      notion_page_id: `page-${Math.random().toString(36).slice(2, 12)}`,
      notion_synced_at: iso(at),
      blocks,
      tags: tagObjs,
      attachments: [],
      day: dayKey(at),
    };
    this.memos.push(memo);
    return memo;
  }

  recount() {
    const counts = new Map<string, number>();
    for (const memo of this.memos.filter((m) => !m.deleted)) {
      for (const tag of memo.tags ?? []) {
        const prev = counts.get(tag.name) ?? 0;
        counts.set(tag.name, prev + 1);
      }
    }
    this.tagList = [...counts.entries()]
      .map(([name, count]) => ({ ...makeTag(name), count }))
      .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name));
  }

  filtered(filter: Filter = {}): Memo[] {
    let list = [...this.memos];
    switch (filter.deleted) {
      case "deleted":
        list = list.filter((m) => m.deleted);
        break;
      case "all":
        break;
      default:
        list = list.filter((m) => !m.deleted);
    }
    if (filter.tag) list = list.filter((m) => (m.tags ?? []).some((t) => t.normalized === filter.tag!.toLowerCase()));
    if (filter.local_only) list = list.filter((m) => !m.notion_page_id);
    if (filter.synced) list = list.filter((m) => Boolean(m.notion_page_id));
    if (filter.with_attachment) list = list.filter((m) => (m.attachments ?? []).length > 0);
    if (filter.untagged) list = list.filter((m) => (m.tags ?? []).length === 0);
    return list.sort((a, b) => b.created_at.localeCompare(a.created_at));
  }
}

const demo = new DemoStore();

function blocksText(blocks: Block[] | null): string {
  return (blocks ?? [])
    .map((b) => (b.spans ?? []).map((s) => s.text).join(""))
    .filter(Boolean)
    .join("\n");
}

function countWords(text: string): number {
  let count = 0;
  let inWord = false;
  for (const ch of text) {
    if (/[\u3040-\u30ff\u3400-\u9fff\uac00-\ud7af]/.test(ch)) {
      count += 1;
      inWord = false;
    } else if (/[\p{L}\p{N}]/u.test(ch)) {
      if (!inWord) count += 1;
      inWord = true;
    } else {
      inWord = false;
    }
  }
  return count;
}

function simpleMarkdown(blocks: Block[]): string {
  return blocks
    .map((b) => {
      const text = (b.spans ?? [])
        .map((s) => {
          let t = s.text;
          if (s.code) t = `\`${t}\``;
          if (s.bold) t = `**${t}**`;
          if (s.italic) t = `*${t}*`;
          if (s.strike) t = `~~${t}~~`;
          if (s.link) t = `[${t}](${s.link})`;
          return t;
        })
        .join("");
      switch (b.type) {
        case "heading":
          return `${"#".repeat(b.level ?? 1)} ${text}`;
        case "bullet":
          return `- ${text}`;
        case "ordered":
          return `1. ${text}`;
        case "todo":
          return `- [${b.checked ? "x" : " "}] ${text}`;
        case "quote":
          return `> ${text}`;
        case "code":
          return "```" + (b.language ?? "") + "\n" + text + "\n```";
        case "divider":
          return "---";
        case "image":
          return `![${b.alt ?? ""}](attachment://${b.attachment_id ?? ""})`;
        case "file":
          return `[${b.alt ?? ""}](attachment://${b.attachment_id ?? ""})`;
        default:
          return text;
      }
    })
    .join("\n\n");
}

function parseMarkdown(source: string): Block[] {
  const out: Block[] = [];
  const lines = source.replace(/\r\n/g, "\n").split("\n");
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    if (!line) continue;
    if (line.startsWith("```")) {
      const language = line.slice(3).trim();
      const code: string[] = [];
      i++;
      for (; i < lines.length && lines[i].trim() !== "```"; i++) code.push(lines[i]);
      out.push(makeBlock("code", code.join("\n"), { language }));
      continue;
    }
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(line)) {
      out.push(makeBlock("divider"));
      continue;
    }
    const heading = line.match(/^(#{1,3})\s+(.*)$/);
    if (heading) {
      out.push(makeBlock("heading", heading[2], { level: heading[1].length }));
      continue;
    }
    const todo = line.match(/^[-*]\s+\[([ xX])\]\s+(.*)$/);
    if (todo) {
      out.push(makeBlock("todo", todo[2], { checked: todo[1].toLowerCase() === "x" }));
      continue;
    }
    const bullet = line.match(/^[-*]\s+(.*)$/);
    if (bullet) {
      out.push(makeBlock("bullet", bullet[1]));
      continue;
    }
    const ordered = line.match(/^\d+[.)]\s+(.*)$/);
    if (ordered) {
      out.push(makeBlock("ordered", ordered[1]));
      continue;
    }
    const quote = line.match(/^>\s?(.*)$/);
    if (quote) {
      out.push(makeBlock("quote", quote[1]));
      continue;
    }
    const image = line.match(/^!\[(.*)\]\(attachment:\/\/(.*)\)$/);
    if (image) {
      out.push(makeBlock("image", "", { alt: image[1], attachment_id: image[2] }));
      continue;
    }
    const file = line.match(/^\[(.*)\]\(attachment:\/\/(.*)\)$/);
    if (file) {
      out.push(makeBlock("file", "", { alt: file[1], attachment_id: file[2] }));
      continue;
    }
    out.push(makeBlock("paragraph", line));
  }
  return out;
}

function dashboard(month: string): Dashboard {
  const [year, mon] = month.split("-").map(Number);
  const start = new Date(year, mon - 1, 1);
  const end = new Date(year, mon, 1);
  const inMonth = demo.memos.filter(
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
  for (const m of demo.memos.filter((x) => !x.deleted)) {
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
    const text = blocksText(m.blocks);
    words += countWords(text);
    hours[new Date(m.created_at).getHours()].count += 1;
    for (const b of m.blocks ?? []) {
      const key =
        b.type === "bullet" || b.type === "ordered"
          ? "list"
          : b.type === "image" || b.type === "file"
            ? "attachment"
            : b.type;
      if (["paragraph", "heading", "list", "todo", "quote", "code", "attachment"].includes(key)) {
        compositionMap[key] = (compositionMap[key] ?? 0) + 1;
      }
      if (b.type === "todo") {
        todoTotal += 1;
        if (b.checked) todoDone += 1;
      }
    }
  }
  const tagCounts = new Map<string, number>();
  for (const m of inMonth) for (const t of m.tags ?? []) tagCounts.set(t.name, (tagCounts.get(t.name) ?? 0) + 1);
  const topTags = [...tagCounts.entries()]
    .map(([name, count]) => ({ ...makeTag(name), count }))
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
    const w = countWords(blocksText(m.blocks));
    if (w <= 50) wordsBuckets[0]++;
    else if (w <= 100) wordsBuckets[1]++;
    else if (w <= 200) wordsBuckets[2]++;
    else if (w <= 400) wordsBuckets[3]++;
    else if (w <= 800) wordsBuckets[4]++;
    else wordsBuckets[5]++;
  }
  const active = demo.memos.filter((m) => !m.deleted);
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
      pending: demo.status.pending,
    },
  };
}

export const demoBackend: Backend = {
  async list(filter = {}) {
    return demo.filtered(filter).map((m) => ({ ...m }));
  },
  async get(id) {
    const memo = demo.memos.find((m) => m.id === id);
    if (!memo) throw new Error("memo not found");
    return { ...memo };
  },
  async counts(): Promise<Counts> {
    const active = demo.memos.filter((m) => !m.deleted);
    return {
      notes: active.length,
      tags: demo.tagList.length,
      days: new Set(active.map((m) => m.day)).size,
    };
  },
  async create(blocks, tags) {
    const memo = demo.add(blocks, tags);
    demo.recount();
    return { ...memo };
  },
  async update(id, blocks, tags, keepLocal) {
    const memo = demo.memos.find((m) => m.id === id);
    if (!memo) throw new Error("memo not found");
    memo.blocks = blocks;
    memo.tags = tags.map(makeTag);
    memo.keep_local = keepLocal;
    memo.revision += 1;
    memo.updated_at = iso(now());
    memo.snippet = blocksText(blocks).slice(0, 200);
    memo.local_state = "present";
    demo.recount();
    return { ...memo };
  },
  async remove(id) {
    const memo = demo.memos.find((m) => m.id === id);
    if (memo) memo.deleted = true;
    demo.recount();
  },
  async restore(id) {
    const memo = demo.memos.find((m) => m.id === id);
    if (!memo) throw new Error("memo not found");
    memo.deleted = false;
    memo.revision += 1;
    demo.recount();
    return { ...memo };
  },
  async purgeMemo(id) {
    demo.memos = demo.memos.filter((m) => m.id !== id);
    demo.recount();
  },
  async emptyTrash() {
    const count = demo.memos.filter((m) => m.deleted).length;
    demo.memos = demo.memos.filter((m) => !m.deleted);
    demo.recount();
    return count;
  },
  async setKeepLocal(id, keep) {
    const memo = demo.memos.find((m) => m.id === id);
    if (memo) memo.keep_local = keep;
  },
  async hydrate(id) {
    const memo = demo.memos.find((m) => m.id === id);
    if (!memo) throw new Error("memo not found");
    memo.local_state = "present";
    return { ...memo };
  },
  async parseMarkdown(source) {
    return parseMarkdown(source);
  },
  async serializeMarkdown(blocks) {
    return simpleMarkdown(blocks);
  },
  async addAttachmentBase64(memoId, name, mime, data) {
    const memo = demo.memos.find((m) => m.id === memoId);
    if (!memo) throw new Error("memo not found");
    const kind: Attachment["kind"] = mime.startsWith("image/") ? "image" : "file";
    const attachment: Attachment = {
      id: `att-${Math.random().toString(36).slice(2, 10)}`,
      memo_id: memoId,
      kind,
      name,
      mime,
      bytes: Math.round((data.length * 3) / 4),
      present: true,
      has_thumb: kind === "image",
    };
    demo.attachments.set(attachment.id, { data: `data:${mime};base64,${data}`, mime });
    memo.attachments = [...(memo.attachments ?? []), attachment];
    memo.blocks = [...(memo.blocks ?? []), makeBlock(kind, "", { attachment_id: attachment.id, alt: name })];
    memo.revision += 1;
    return attachment;
  },
  async removeAttachment(id) {
    for (const memo of demo.memos) {
      memo.attachments = (memo.attachments ?? []).filter((a) => a.id !== id);
      memo.blocks = (memo.blocks ?? []).filter((b) => b.attachment_id !== id);
    }
    demo.attachments.delete(id);
  },
  async attachmentThumb(id) {
    return demo.attachments.get(id)?.data ?? "";
  },
  async attachmentData(id) {
    return demo.attachments.get(id)?.data ?? "";
  },
  async openAttachment() {
    return;
  },

  async tags() {
    return demo.tagList.map((t) => ({ ...t }));
  },
  async suggestTags(query) {
    const q = query.toLowerCase();
    return demo.tagList.filter((t) => t.normalized.includes(q)).slice(0, 8);
  },
  async renameTag(id, name) {
    const tag = demo.tagList.find((t) => t.id === id);
    for (const memo of demo.memos) {
      memo.tags = (memo.tags ?? []).map((t) => (t.id === id ? { ...t, name, normalized: name.toLowerCase() } : t));
    }
    demo.recount();
    return { ...(tag ?? makeTag(name)), name, normalized: name.toLowerCase() };
  },
  async mergeTags(sourceId, targetId) {
    const source = demo.tagList.find((t) => t.id === sourceId);
    const target = demo.tagList.find((t) => t.id === targetId);
    if (!source || !target) return;
    for (const memo of demo.memos) {
      const hasSource = (memo.tags ?? []).some((t) => t.id === sourceId);
      if (!hasSource) continue;
      memo.tags = (memo.tags ?? []).filter((t) => t.id !== sourceId);
      if (!(memo.tags ?? []).some((t) => t.id === targetId)) {
        memo.tags = [...(memo.tags ?? []), target];
      }
    }
    demo.recount();
  },
  async deleteTag(id) {
    for (const memo of demo.memos) memo.tags = (memo.tags ?? []).filter((t) => t.id !== id);
    demo.recount();
  },

  async search(query, tag = "", from = "", to = "") {
    const q = query.toLowerCase();
    const results: SearchResult[] = [];
    for (const memo of demo.filtered({ tag })) {
      const text = blocksText(memo.blocks);
      const idx = text.toLowerCase().indexOf(q);
      const tagHit = (memo.tags ?? []).some((t) => t.normalized.includes(q));
      if (idx < 0 && !tagHit) continue;
      if (from && memo.created_at < from) continue;
      if (to && memo.created_at >= to) continue;
      const before = idx < 0 ? text.slice(0, 120) : text.slice(Math.max(0, idx - 40), idx);
      const match = idx < 0 ? "" : text.slice(idx, idx + query.length);
      const after = idx < 0 ? "" : text.slice(idx + query.length, idx + query.length + 80);
      results.push({ memo: { ...memo }, before, match, after });
    }
    return results;
  },
  async dashboard(month) {
    return dashboard(month);
  },

  async settings() {
    return { ...demo.settings };
  },
  async setTheme(theme) {
    demo.settings.theme = theme as Settings["theme"];
  },
  async setHotkey(accelerator) {
    demo.settings.hotkey = accelerator;
    demo.settings.hotkey_error = undefined;
  },
  async setStartAtLogin(enabled) {
    demo.settings.start_at_login = enabled;
  },
  async setRetention(days, immediate) {
    demo.settings.retention_days = days;
    demo.settings.immediate_eviction = immediate;
  },
  async rebuildIndex() {
    return;
  },
  async deleteLocalData() {
    demo.memos = [];
    demo.tagList = [];
    demo.attachments.clear();
    demo.settings.sync_enabled = false;
    demo.settings.notion_connected = false;
  },
  async exportBackup(): Promise<BackupResult> {
    return { path: "C:\\Users\\you\\AppData\\Roaming\\memo\\backups\\demo", bytes: 1024, at: iso(now()) };
  },
  async exportLogs() {
    return "C:\\Users\\you\\AppData\\Roaming\\memo\\backups\\logs\\sync.log";
  },
  async reportError(report: ErrorReport) {
    const detail = [report.message, report.stack ? `stack:\n${report.stack}` : "", report.context ? `context: ${report.context}` : ""]
      .filter(Boolean)
      .join("\n");
    demo.errors.unshift({
      id: demo.nextErrorID++,
      at: iso(now()),
      level: "error",
      event: `ui:${report.source || "frontend"}`,
      message: detail,
    });
    demo.errors = demo.errors.slice(0, 50);
  },
  async errorLogs() {
    return demo.errors.map((entry) => ({ ...entry }));
  },
  async openDataDir() {
    return;
  },
  async completeOnboarding() {
    demo.settings.onboarded = true;
  },

  async syncStatus() {
    return { ...demo.status, logs: [...(demo.status.logs ?? [])] };
  },
  async setSyncEnabled(enabled) {
    demo.settings.sync_enabled = enabled;
    demo.status.enabled = enabled;
  },
  async setSyncPaused(paused) {
    demo.settings.sync_paused = paused;
    demo.status.paused = paused;
    demo.status.state = paused ? "paused" : "idle";
  },
  async syncNow() {
    demo.status.last_success = iso(now());
    demo.status.state = "idle";
  },
  async validateToken(): Promise<Workspace> {
    return { name: "演示工作区", id: "workspace-demo", file_limit: 5 * 1024 * 1024, bot_type: "bot" };
  },
  async parentPages() {
    return {
      databases: [
        { id: "ds-demo", database_id: "db-demo", title: "memo archive", url: "https://notion.so/db-demo" },
      ],
      pages: [{ id: "parent-demo", title: "memo archive page", url: "https://notion.so/parent-demo" }],
    };
  },
  async resolveArchiveTarget(_token: string, target: string): Promise<ArchiveTarget> {
    const id = target.includes("/") || target.length > 40 ? "ds-demo" : target;
    return { kind: "database", id, title: "memo archive", url: `https://notion.so/${id}` };
  },
  async previewArchiveDatabase(_token: string, target: string): Promise<ArchiveDatabasePreview> {
    return {
      id: target,
      title: "memo archive",
      title_property: "名称",
      missing_properties: [],
      conflicts: [],
      has_existing_rows: false,
    };
  },
  async connect(request: ConnectRequest): Promise<ConnectResult> {
    demo.settings.notion_connected = true;
    demo.settings.workspace_name = "演示工作区";
    const created = request.kind === "page";
    return {
      workspace: { name: "演示工作区", id: "workspace-demo", file_limit: 5 * 1024 * 1024, bot_type: "bot" },
      data_source_id: created ? `ds-${request.target_id}` : request.target_id,
      database_id: created ? `db-${request.target_id}` : "db-demo",
      database_title: created ? request.database_name || "memo" : "memo archive",
      title_property: "名称",
      created_database: created,
    };
  },
  async disconnect() {
    demo.settings.notion_connected = false;
    demo.settings.sync_enabled = false;
    demo.status.enabled = false;
    demo.status.configured = false;
  },
  async restoreAll(): Promise<RestoreProgress> {
    return { total: 0, completed: 0, failed: 0 };
  },
  async hydrateViaSync(id) {
    return demoBackend.hydrate(id).then(() => undefined);
  },
  async syncLogs(): Promise<SyncLog[]> {
    return demo.status.logs ?? [];
  },
  async setWindowActive() {
    return;
  },

  async showMain() {
    return;
  },
  async takePendingView() {
    return "";
  },
  async showSticky() {
    return;
  },
  async hideSticky() {
    return;
  },
  async quit() {
    return;
  },
  async saveStickyPosition() {
    return;
  },
};

export { demo };
