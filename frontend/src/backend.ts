import { Events } from "@wailsio/runtime";
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
  ParentPageList,
  RestoreProgress,
  SearchResult,
  Settings,
  SyncLog,
  SyncStatus,
  Tag,
  Workspace,
} from "./types";

export type Unsubscribe = () => void;

export interface Backend {
  list(filter?: Filter): Promise<Memo[]>;
  get(id: string): Promise<Memo>;
  counts(): Promise<Counts>;
  create(blocks: Block[], tags: string[]): Promise<Memo>;
  update(id: string, blocks: Block[], tags: string[], keepLocal: boolean): Promise<Memo>;
  remove(id: string): Promise<void>;
  restore(id: string): Promise<Memo>;
  purgeMemo(id: string): Promise<void>;
  emptyTrash(): Promise<number>;
  setKeepLocal(id: string, keep: boolean): Promise<void>;
  hydrate(id: string): Promise<Memo>;
  parseMarkdown(source: string): Promise<Block[]>;
  serializeMarkdown(blocks: Block[]): Promise<string>;
  addAttachmentBase64(memoId: string, name: string, mime: string, data: string): Promise<Attachment>;
  removeAttachment(id: string): Promise<void>;
  attachmentThumb(id: string): Promise<string>;
  attachmentData(id: string): Promise<string>;
  openAttachment(id: string): Promise<void>;

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
  setStartAtLogin(enabled: boolean): Promise<void>;
  setRetention(days: number, immediate: boolean): Promise<void>;
  rebuildIndex(): Promise<void>;
  deleteLocalData(): Promise<void>;
  exportBackup(): Promise<BackupResult>;
  exportLogs(): Promise<string>;
  reportError(report: ErrorReport): Promise<void>;
  errorLogs(): Promise<SyncLog[]>;
  openDataDir(): Promise<void>;
  completeOnboarding(): Promise<void>;

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

export function isWailsRuntime(): boolean {
  if (typeof window === "undefined") return false;
  const wails = (window as unknown as { _wails?: { environment?: unknown } })._wails;
  return Boolean(wails?.environment);
}

export function onAppEvent(name: string, handler: (data: any) => void): Unsubscribe {
  if (!isWailsRuntime()) return () => {};
  return Events.On(name, (event) => handler(event.data));
}
