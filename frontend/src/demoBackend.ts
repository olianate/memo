import type {
  ArchiveDatabasePreview,
  ArchiveTarget,
  Attachment,
  AttachmentPayload,
  BackupResult,
  ConnectRequest,
  ConnectResult,
  Counts,
  ErrorReport,
  Filter,
  Memo,
  RestoreProgress,
  SearchResult,
  Settings,
  SyncLog,
  SyncStatus,
  Tag,
  UpdateInfo,
  Workspace,
} from "./types";
import type { Backend } from "./backend";
import {
  appendAttachment,
  attachmentLine,
  bodyText,
  removeAttachmentLine,
  snippetOf,
} from "./lib/bodyProjection";
import { buildDashboard } from "./lib/dashboardProjection";

const now = () => new Date();
const iso = (d: Date) => d.toISOString();
const dayKey = (d: Date) => {
  const x = new Date(d);
  return `${x.getFullYear()}-${String(x.getMonth() + 1).padStart(2, "0")}-${String(x.getDate()).padStart(2, "0")}`;
};

function makeTag(name: string): Tag {
  return { id: `tag-${name}`, name, normalized: name.toLowerCase(), count: 0 };
}

class DemoStore {
  memos: Memo[] = [];
  tagList: Tag[] = [];
  settings: Settings = {
    theme: "light",
    hotkey: "CmdOrCtrl+Shift+Space",
    submit_on_enter: true,
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
    mcp_enabled: false,
    mcp_port: 8765,
    mcp_running: false,
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
      "现在的想法是… 支持 Markdown 的实时渲染，标签在正文之外管理。\n\n- Enter 保存，Shift+Enter 换行\n- [x] 把 sticky window 做成全局快捷键",
      ["产品", "memo"],
      d(0, 9),
    );
    this.add(
      "## 把 Notion 当归档，而不是实时数据库\n\n搜索必须留在本地。Notion 只负责长期归档与跨设备同步。\n\n本地保留 30 天后，正文和附件会被清理，但元数据、标签和搜索索引仍在。",
      ["产品", "memo", "技术"],
      d(0, 8),
    );
    this.add(
      "Wails 的 IME 问题用 PR #6596 修好了。\n\nWindows 11 + 微软拼音通过。macOS 留到发布前再补测。",
      ["技术", "todo"],
      d(1, 21),
    );
    this.add(
      "给 sticky window 加一个「保留本地」的 pin。\n\n清理正文时不要动它。",
      ["产品", "设计"],
      d(1, 16),
    );
    this.add(
      "# 本周回顾\n\n写了 4 条笔记，2 个想法进入了实现。\n\n> 慢一点，但每天都在写。",
      ["生活", "memo"],
      d(3, 22),
    );
    this.add(
      "中文搜索要支持 1–2 个字。FTS5 trigram 不够，短查询回退到 LIKE。\n\n```sql\nSELECT * FROM memos WHERE body_text LIKE '%开会%'\n```",
      ["技术", "memo"],
      d(5, 10),
    );
    this.recount();
  }

  add(body: string, tags: string[], created?: Date): Memo {
    const at = created ?? now();
    const tagObjs = tags.map(makeTag);
    const memo: Memo = {
      id: `memo-${Math.random().toString(36).slice(2, 12)}`,
      created_at: iso(at),
      updated_at: iso(at),
      revision: 1,
      snippet: snippetOf(body),
      deleted: false,
      keep_local: false,
      local_state: "present",
      notion_page_id: `page-${Math.random().toString(36).slice(2, 12)}`,
      notion_synced_at: iso(at),
      body,
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

function attachPayloads(memo: Memo, files: AttachmentPayload[]): Attachment[] {
  const added: Attachment[] = [];
  for (const file of files) {
    const kind: Attachment["kind"] = file.mime.startsWith("image/") ? "image" : "file";
    const attachment: Attachment = {
      id: `att-${Math.random().toString(36).slice(2, 10)}`,
      memo_id: memo.id,
      kind,
      name: file.name,
      mime: file.mime,
      bytes: Math.round((file.data.length * 3) / 4),
      present: true,
      has_thumb: kind === "image",
    };
    demo.attachments.set(attachment.id, { data: `data:${file.mime};base64,${file.data}`, mime: file.mime });
    memo.attachments = [...(memo.attachments ?? []), attachment];
    memo.body = appendAttachment(memo.body, attachmentLine(kind, attachment.id, file.name));
    added.push(attachment);
  }
  if (added.length) {
    memo.snippet = snippetOf(memo.body);
    memo.updated_at = iso(now());
  }
  return added;
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
  async create(body, tags) {
    const memo = demo.add(body, tags);
    demo.recount();
    return { ...memo };
  },
  async createWithAttachments(body, tags, files) {
    const memo = demo.add(body, tags);
    attachPayloads(memo, files);
    demo.recount();
    return { ...memo, attachments: [...(memo.attachments ?? [])] };
  },
  async update(id, body, tags, keepLocal) {
    const memo = demo.memos.find((m) => m.id === id);
    if (!memo) throw new Error("memo not found");
    memo.body = body;
    memo.tags = tags.map(makeTag);
    memo.keep_local = keepLocal;
    memo.revision += 1;
    memo.updated_at = iso(now());
    memo.snippet = snippetOf(body);
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
  async addAttachmentBase64(memoId, name, mime, data) {
    const added = await demoBackend.addAttachments(memoId, [{ name, mime, data }]);
    if (!added.length) throw new Error("attachment was not stored");
    return added[0];
  },
  async addAttachments(memoId, files) {
    const memo = demo.memos.find((m) => m.id === memoId);
    if (!memo) throw new Error("memo not found");
    const added = attachPayloads(memo, files);
    if (added.length) memo.revision += 1;
    return added;
  },
  async removeAttachment(id) {
    for (const memo of demo.memos) {
      memo.attachments = (memo.attachments ?? []).filter((a) => a.id !== id);
      memo.body = removeAttachmentLine(memo.body, id);
      memo.snippet = snippetOf(memo.body);
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
  async openURL(url) {
    if (typeof window !== "undefined") window.open(url, "_blank", "noopener,noreferrer");
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
      const text = bodyText(memo.body);
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
    return buildDashboard(demo.memos, month, demo.status.pending);
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
  async setSubmitOnEnter(enabled) {
    demo.settings.submit_on_enter = enabled;
  },
  async setStartAtLogin(enabled) {
    demo.settings.start_at_login = enabled;
  },
  async setRetention(days, immediate) {
    demo.settings.retention_days = days;
    demo.settings.immediate_eviction = immediate;
  },
  async setMCP(enabled, port) {
    demo.settings.mcp_enabled = enabled;
    demo.settings.mcp_port = port;
    demo.settings.mcp_running = enabled;
    demo.settings.mcp_endpoint = enabled ? `http://127.0.0.1:${port}/mcp` : undefined;
    demo.settings.mcp_error = undefined;
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
  async clearLogs() {
    demo.errors = [];
    demo.status.logs = [];
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

  async checkForUpdates(): Promise<UpdateInfo> {
    return {
      current_version: demo.settings.version,
      latest_version: demo.settings.version,
      available: false,
      notes: "",
      published_at: "",
      release_url: "https://github.com/olianate/memo/releases",
      asset_name: "",
      asset_size: 0,
      can_install: false,
      checked_at: iso(now()),
    };
  },
  async installUpdate() {
    throw new Error("演示模式不支持自动安装更新");
  },
  async openReleasePage() {
    return;
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
