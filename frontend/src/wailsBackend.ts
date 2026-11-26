import type {
  ArchiveDatabasePreview,
  ArchiveTarget,
  Attachment,
  BackupResult,
  ConnectRequest,
  ConnectResult,
  Dashboard,
  ErrorReport,
  Memo,
  ParentPageList,
  RestoreProgress,
  Settings,
  SyncLog,
  SyncStatus,
  Tag,
  UpdateInfo,
  Workspace,
} from "./types";
import type { Backend } from "./backend";

// The generated bindings are JSDoc-typed JavaScript modules.
import * as MemoSvc from "../bindings/github.com/olianate/memo/internal/services/memoservice.js";
import * as TagSvc from "../bindings/github.com/olianate/memo/internal/services/tagservice.js";
import * as SearchSvc from "../bindings/github.com/olianate/memo/internal/services/searchservice.js";
import * as StatsSvc from "../bindings/github.com/olianate/memo/internal/services/statsservice.js";
import * as SettingsSvc from "../bindings/github.com/olianate/memo/internal/services/settingsservice.js";
import * as SyncSvc from "../bindings/github.com/olianate/memo/internal/services/syncservice.js";
import * as PlatformSvc from "../bindings/github.com/olianate/memo/internal/services/platformservice.js";
import * as UpdateSvc from "../bindings/github.com/olianate/memo/internal/services/updateservice.js";

const asMemo = (m: any): Memo => ({
  ...m,
  body: m.body ?? "",
  tags: m.tags ?? [],
  attachments: m.attachments ?? [],
});

const asTag = (t: any): Tag => ({
  id: t.id,
  name: t.name,
  normalized: t.normalized,
  count: t.count ?? 0,
});

export const wailsBackend: Backend = {
  async list(filter = {}) {
    const rows = await MemoSvc.List({
      deleted: filter.deleted ?? "active",
      tag: filter.tag ?? "",
      local_only: Boolean(filter.local_only),
      synced: Boolean(filter.synced),
      with_attachment: Boolean(filter.with_attachment),
      untagged: Boolean(filter.untagged),
      from: filter.from ?? "",
      to: filter.to ?? "",
      limit: filter.limit ?? 500,
    });
    return (rows ?? []).map(asMemo);
  },
  async get(id) {
    return asMemo(await MemoSvc.Get(id));
  },
  async counts() {
    return await MemoSvc.Counts();
  },
  async create(body, tags) {
    return asMemo(await MemoSvc.Create(body, tags));
  },
  async createWithAttachments(body, tags, files) {
    return asMemo(await MemoSvc.CreateWithAttachments(body, tags, files as any));
  },
  async update(id, body, tags, keepLocal) {
    return asMemo(await MemoSvc.Update(id, body, tags, keepLocal));
  },
  async remove(id) {
    await MemoSvc.Delete(id);
  },
  async restore(id) {
    return asMemo(await MemoSvc.Restore(id));
  },
  async purgeMemo(id) {
    await MemoSvc.Purge(id);
  },
  async emptyTrash(): Promise<number> {
    return (await MemoSvc.EmptyTrash()) ?? 0;
  },
  async setKeepLocal(id, keep) {
    await MemoSvc.SetKeepLocal(id, keep);
  },
  async setPinned(id, pinned) {
    await MemoSvc.SetPinned(id, pinned);
  },
  async hydrate(id) {
    return asMemo(await MemoSvc.Hydrate(id));
  },
  async addAttachmentBase64(memoId, name, mime, data): Promise<Attachment> {
    return (await MemoSvc.AddAttachmentBase64(memoId, name, mime, data)) as unknown as Attachment;
  },
  async addAttachments(memoId, files): Promise<Attachment[]> {
    return ((await MemoSvc.AddAttachments(memoId, files as any)) ?? []) as unknown as Attachment[];
  },
  async removeAttachment(id) {
    await MemoSvc.RemoveAttachment(id);
  },
  async attachmentThumb(id) {
    return await MemoSvc.AttachmentThumb(id);
  },
  async attachmentData(id) {
    return await MemoSvc.AttachmentData(id);
  },
  async openAttachment(id) {
    await MemoSvc.OpenAttachment(id);
  },
  async openURL(url) {
    await PlatformSvc.OpenURL(url);
  },

  async tags() {
    return ((await TagSvc.List()) ?? []).map(asTag);
  },
  async suggestTags(query) {
    return ((await TagSvc.Suggest(query)) ?? []).map(asTag);
  },
  async renameTag(id, name) {
    return asTag(await TagSvc.Rename(id, name));
  },
  async mergeTags(sourceId, targetId) {
    await TagSvc.Merge(sourceId, targetId);
  },
  async deleteTag(id) {
    await TagSvc.Delete(id);
  },

  async search(query, tag = "", from = "", to = "") {
    return ((await SearchSvc.Query(query, tag, from, to)) ?? []).map((r: any) => ({
      ...r,
      memo: asMemo(r.memo),
    }));
  },
  async dashboard(month): Promise<Dashboard> {
    const raw = (await StatsSvc.Dashboard(month)) as any;
    return {
      ...raw,
      top_tags: (raw?.top_tags ?? []).map((t: any) => ({
        id: t.id,
        name: t.name,
        normalized: t.normalized_name ?? t.normalized ?? String(t.name).toLowerCase(),
        count: t.count ?? 0,
      })),
    } as Dashboard;
  },

  async settings(): Promise<Settings> {
    return (await SettingsSvc.Get()) as Settings;
  },
  async setTheme(theme) {
    await SettingsSvc.SetTheme(theme);
  },
  async setHotkey(accelerator) {
    await SettingsSvc.SetHotkey(accelerator);
  },
  async setSubmitOnEnter(enabled) {
    await SettingsSvc.SetSubmitOnEnter(enabled);
  },
  async setStartAtLogin(enabled) {
    await SettingsSvc.SetStartAtLogin(enabled);
  },
  async setRetention(days, immediate) {
    await SettingsSvc.SetRetention(days, immediate);
  },
  async setMCP(enabled, port) {
    await SettingsSvc.SetMCP(enabled, port);
  },
  async rebuildIndex() {
    await SettingsSvc.RebuildIndex();
  },
  async deleteLocalData() {
    await SettingsSvc.DeleteLocalData();
  },
  async exportBackup(): Promise<BackupResult> {
    return await SettingsSvc.ExportBackup();
  },
  async exportLogs() {
    return await SettingsSvc.ExportLogs();
  },
  async clearLogs() {
    await SettingsSvc.ClearLogs();
  },
  async reportError(report: ErrorReport) {
    await SettingsSvc.ReportError(report as any);
  },
  async errorLogs(): Promise<SyncLog[]> {
    return ((await SettingsSvc.RecentErrors()) ?? []) as unknown as SyncLog[];
  },
  async openDataDir() {
    await SettingsSvc.OpenDataDir();
  },
  async completeOnboarding() {
    await SettingsSvc.CompleteOnboarding();
  },

  async checkForUpdates(): Promise<UpdateInfo> {
    return await UpdateSvc.Check();
  },
  async installUpdate() {
    await UpdateSvc.Install();
  },
  async openReleasePage() {
    await UpdateSvc.OpenReleasePage();
  },

  async syncStatus(): Promise<SyncStatus> {
    const s = (await SyncSvc.Status()) as unknown as SyncStatus;
    return { ...s, logs: s.logs ?? [] };
  },
  async setSyncEnabled(enabled) {
    await SyncSvc.SetEnabled(enabled);
  },
  async setSyncPaused(paused) {
    await SyncSvc.SetPaused(paused);
  },
  async syncNow() {
    await SyncSvc.SyncNow();
  },
  async validateToken(token): Promise<Workspace> {
    return (await SyncSvc.ValidateToken(token)) as unknown as Workspace;
  },
  async parentPages(token, query): Promise<ParentPageList> {
    const result = (await SyncSvc.ParentPages(token, query)) as unknown as ParentPageList;
    return {
      databases: result?.databases ?? [],
      pages: result?.pages ?? [],
    };
  },
  async resolveArchiveTarget(token, target): Promise<ArchiveTarget> {
    return (await SyncSvc.ResolveArchiveTarget(token, target)) as unknown as ArchiveTarget;
  },
  async previewArchiveDatabase(token, target): Promise<ArchiveDatabasePreview> {
    const result = (await SyncSvc.PreviewArchiveDatabase(token, target)) as unknown as ArchiveDatabasePreview;
    return {
      ...result,
      missing_properties: result?.missing_properties ?? [],
      conflicts: result?.conflicts ?? [],
    };
  },
  async connect(request: ConnectRequest): Promise<ConnectResult> {
    return (await SyncSvc.Connect(request as any)) as unknown as ConnectResult;
  },
  async disconnect() {
    await SyncSvc.Disconnect();
  },
  async restoreAll(): Promise<RestoreProgress> {
    return (await SyncSvc.RestoreAll()) as unknown as RestoreProgress;
  },
  async hydrateViaSync(id) {
    await SyncSvc.Hydrate(id);
  },
  async syncLogs(): Promise<SyncLog[]> {
    return ((await SyncSvc.Logs()) ?? []) as unknown as SyncLog[];
  },
  async setWindowActive(active) {
    await SyncSvc.SetActive(active);
  },

  async showMain() {
    await PlatformSvc.ShowMain();
  },
  async takePendingView() {
    return (await PlatformSvc.TakePendingView()) ?? "";
  },
  async showSticky() {
    await PlatformSvc.ShowSticky();
  },
  async hideSticky() {
    await PlatformSvc.HideSticky();
  },
  async quit() {
    await PlatformSvc.Quit();
  },
  async saveStickyPosition(x, y) {
    await PlatformSvc.SaveStickyPosition(x, y);
  },
};
