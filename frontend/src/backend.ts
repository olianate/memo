import { Events } from "@wailsio/runtime";
import type {
  ArchiveDatabasePreview,
  ArchiveTarget,
  Attachment,
  AttachmentPayload,
  BackupResult,
  ConnectRequest,
  ConnectResult,
  Counts,
  Dashboard,
  ErrorReport,
  Filter,
  Memo,
  ParentPageList,
  RestoreProgress,
  SearchResult,
  Settings,
  SyncLog,
  SyncStatus,
  Tag,
  UpdateInfo,
  Workspace,
} from "./types";

export type Unsubscribe = () => void;

export interface Backend {
  list(filter?: Filter): Promise<Memo[]>;
  get(id: string): Promise<Memo>;
  counts(): Promise<Counts>;
  create(body: string, tags: string[]): Promise<Memo>;
  createWithAttachments(body: string, tags: string[], files: AttachmentPayload[]): Promise<Memo>;
  update(id: string, body: string, tags: string[], keepLocal: boolean): Promise<Memo>;
  remove(id: string): Promise<void>;
  restore(id: string): Promise<Memo>;
  purgeMemo(id: string): Promise<void>;
  emptyTrash(): Promise<number>;
  setKeepLocal(id: string, keep: boolean): Promise<void>;
  hydrate(id: string): Promise<Memo>;
  addAttachmentBase64(memoId: string, name: string, mime: string, data: string): Promise<Attachment>;
  addAttachments(memoId: string, files: AttachmentPayload[]): Promise<Attachment[]>;
  removeAttachment(id: string): Promise<void>;
  attachmentThumb(id: string): Promise<string>;
  attachmentData(id: string): Promise<string>;
  openAttachment(id: string): Promise<void>;
  openURL(url: string): Promise<void>;

  tags(): Promise<Tag[]>;
  suggestTags(query: string): Promise<Tag[]>;
  renameTag(id: string, name: string): Promise<Tag>;
  mergeTags(sourceId: string, targetId: string): Promise<void>;
  deleteTag(id: string): Promise<void>;

  search(query: string, tag?: string, from?: string, to?: string): Promise<SearchResult[]>;
  dashboard(month: string): Promise<Dashboard>;

  settings(): Promise<Settings>;
  setTheme(theme: string): Promise<void>;
  setHotkey(accelerator: string): Promise<void>;
  setSubmitOnEnter(enabled: boolean): Promise<void>;
  setStartAtLogin(enabled: boolean): Promise<void>;
  setRetention(days: number, immediate: boolean): Promise<void>;
  setMCP(enabled: boolean, port: number): Promise<void>;
  rebuildIndex(): Promise<void>;
  deleteLocalData(): Promise<void>;
  exportBackup(): Promise<BackupResult>;
  exportLogs(): Promise<string>;
  clearLogs(): Promise<void>;
  reportError(report: ErrorReport): Promise<void>;
  errorLogs(): Promise<SyncLog[]>;
  openDataDir(): Promise<void>;
  completeOnboarding(): Promise<void>;

  checkForUpdates(): Promise<UpdateInfo>;
  installUpdate(): Promise<void>;
  openReleasePage(): Promise<void>;

  syncStatus(): Promise<SyncStatus>;
  setSyncEnabled(enabled: boolean): Promise<void>;
  setSyncPaused(paused: boolean): Promise<void>;
  syncNow(): Promise<void>;
  validateToken(token: string): Promise<Workspace>;
  parentPages(token: string, query: string): Promise<ParentPageList>;
  resolveArchiveTarget(token: string, target: string): Promise<ArchiveTarget>;
  previewArchiveDatabase(token: string, target: string): Promise<ArchiveDatabasePreview>;
  connect(request: ConnectRequest): Promise<ConnectResult>;
  disconnect(): Promise<void>;
  restoreAll(): Promise<RestoreProgress>;
  hydrateViaSync(id: string): Promise<void>;
  syncLogs(): Promise<SyncLog[]>;
  setWindowActive(active: boolean): Promise<void>;

  showMain(): Promise<void>;
  takePendingView(): Promise<string>;
  showSticky(): Promise<void>;
  hideSticky(): Promise<void>;
  quit(): Promise<void>;
  saveStickyPosition(x: number, y: number): Promise<void>;
}

// Callers depend on the slice they actually use. Backend is the composition of
// those slices; the two adapters satisfy the whole surface.
export type MarkdownEditorBackend = Pick<
  Backend,
  "attachmentData" | "attachmentThumb" | "openAttachment" | "openURL" | "suggestTags"
>;
export type MemoListBackend = Pick<Backend, "list" | "remove" | "purgeMemo" | "setKeepLocal"> &
  MarkdownEditorBackend;
export type MemoDraftBackend = Pick<
  Backend,
  "create" | "createWithAttachments" | "update" | "addAttachments" | "get" | "removeAttachment"
> &
  MarkdownEditorBackend;
export type MemoSheetBackend = Pick<
  Backend,
  | "get"
  | "hydrate"
  | "update"
  | "remove"
  | "purgeMemo"
  | "restore"
  | "setKeepLocal"
  | "addAttachments"
  | "removeAttachment"
> &
  MarkdownEditorBackend;
export type OnboardingBackend = Pick<
  Backend,
  | "settings"
  | "setHotkey"
  | "setStartAtLogin"
  | "completeOnboarding"
  | "connect"
  | "parentPages"
  | "previewArchiveDatabase"
  | "resolveArchiveTarget"
  | "validateToken"
>;
export type StatsBackend = Pick<Backend, "dashboard">;
export type SettingsControllerBackend = Pick<
  Backend,
  | "settings"
  | "syncStatus"
  | "tags"
  | "errorLogs"
  | "setTheme"
  | "exportBackup"
  | "restoreAll"
  | "exportLogs"
  | "checkForUpdates"
  | "installUpdate"
  | "openReleasePage"
>;
export type SettingsPageBackend = SettingsControllerBackend &
  OnboardingBackend &
  Pick<
    Backend,
    | "clearLogs"
    | "deleteLocalData"
    | "deleteTag"
    | "disconnect"
    | "mergeTags"
    | "openDataDir"
    | "rebuildIndex"
    | "renameTag"
    | "setHotkey"
    | "setMCP"
    | "setRetention"
    | "setStartAtLogin"
    | "setSubmitOnEnter"
    | "setSyncEnabled"
    | "setSyncPaused"
    | "syncNow"
  >;
export type StickyBackend = Pick<
  Backend,
  | "reportError"
  | "tags"
  | "settings"
  | "syncStatus"
  | "create"
  | "createWithAttachments"
  | "update"
  | "get"
  | "addAttachments"
  | "removeAttachment"
  | "attachmentData"
  | "attachmentThumb"
  | "openAttachment"
  | "openURL"
  | "suggestTags"
  | "hideSticky"
>;

export function isWailsRuntime(): boolean {
  if (typeof window === "undefined") return false;
  const wails = (window as unknown as { _wails?: { environment?: unknown } })._wails;
  return Boolean(wails?.environment);
}

export function onAppEvent(name: string, handler: (data: any) => void): Unsubscribe {
  if (!isWailsRuntime()) return () => {};
  return Events.On(name, (event) => handler(event.data));
}
