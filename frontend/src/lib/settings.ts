import type { Backend } from "../backend";
import { onAppEvent } from "../backend";
import type {
  BackupResult,
  RestoreProgress,
  Settings,
  SyncLog,
  SyncStatus,
  Tag,
  UpdateInfo,
  UpdateProgress,
} from "../types";

/** Everything the settings page renders, owned by one module. */
export interface SettingsState {
  settings: Settings | null;
  sync: SyncStatus | null;
  tags: Tag[];
  errors: SyncLog[];
  /** Unsaved text in the hotkey field. */
  hotkey: string;
  /** Unsaved text in the MCP port field. */
  mcpPort: string;
  backup: BackupResult | null;
  restore: RestoreProgress | null;
  logsPath: string;
  update: UpdateInfo | null;
  updateProgress: UpdateProgress | null;
  /** A settings mutation is in flight. */
  busy: boolean;
  updateBusy: boolean;
  message: string;
  error: string;
  updateError: string;
}

type Listener = (state: SettingsState) => void;
type EventHandler = (data: unknown) => void;

export interface SettingsOptions {
  onChanged?: () => void;
  onError?: (message: string) => void;
  clipboard?: (text: string) => Promise<void>;
  subscribe?: (event: string, handler: EventHandler) => () => void;
}

const EMPTY: SettingsState = {
  settings: null,
  sync: null,
  tags: [],
  errors: [],
  hotkey: "",
  mcpPort: "8765",
  backup: null,
  restore: null,
  logsPath: "",
  update: null,
  updateProgress: null,
  busy: false,
  updateBusy: false,
  message: "",
  error: "",
  updateError: "",
};

const defaultClipboard = async (text: string) => {
  await navigator.clipboard.writeText(text);
};

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Owns the settings page's async work: loading the snapshot, the busy/error/
 * message lifecycle around a mutation, backup and restore results, the update
 * flow, and the app events that stream into it. The page only renders state
 * and forwards intent.
 */
export class SettingsController {
  onChanged: () => void;
  onError: (message: string) => void;

  private readonly backend: Backend;
  private readonly clipboard: (text: string) => Promise<void>;
  private readonly subscribeTo: (event: string, handler: EventHandler) => () => void;
  private state: SettingsState = { ...EMPTY };
  private readonly listeners = new Set<Listener>();

  constructor(backend: Backend, options: SettingsOptions = {}) {
    this.backend = backend;
    this.onChanged = options.onChanged ?? (() => {});
    this.onError = options.onError ?? (() => {});
    this.clipboard = options.clipboard ?? defaultClipboard;
    this.subscribeTo = options.subscribe ?? onAppEvent;
  }

  subscribe = (listener: Listener) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = (): SettingsState => this.state;

  setHotkey = (value: string) => this.patch({ hotkey: value });

  setMcpPort = (value: string) => this.patch({ mcpPort: value });

  /** Reloads settings, sync status, tags, and error logs in parallel. */
  async load(): Promise<void> {
    try {
      const [settings, sync, tags, errors] = await Promise.all([
        this.backend.settings(),
        this.backend.syncStatus(),
        this.backend.tags(),
        this.backend.errorLogs().catch(() => [] as SyncLog[]),
      ]);
      this.patch({
        settings,
        sync,
        tags,
        errors,
        hotkey: settings.hotkey,
        mcpPort: String(settings.mcp_port),
      });
    } catch (error) {
      this.patch({ error: this.report(error) });
    }
  }

  /** Runs one mutation, then reloads the snapshot and tells the app. */
  async perform(action: () => Promise<void>, success?: string): Promise<void> {
    this.patch({ busy: true, error: "", message: "" });
    try {
      await action();
      if (success) this.patch({ message: success });
      await this.load();
      this.onChanged();
    } catch (error) {
      this.patch({ error: this.report(error) });
    } finally {
      this.patch({ busy: false });
    }
  }

  async exportBackup(): Promise<void> {
    await this.perform(async () => {
      this.patch({ backup: await this.backend.exportBackup() });
    }, "备份完成");
  }

  async restoreAll(): Promise<void> {
    await this.perform(async () => {
      this.patch({ restore: null });
      this.patch({ restore: await this.backend.restoreAll() });
    }, "恢复完成");
  }

  async exportLogs(): Promise<void> {
    await this.perform(async () => {
      this.patch({ logsPath: await this.backend.exportLogs() });
    }, "错误日志已导出");
  }

  async copyErrors(): Promise<void> {
    const text = this.state.errors
      .map((log) => `[${new Date(log.at).toISOString()}] ${log.level} ${log.event}\n${log.message}`)
      .join("\n\n");
    try {
      await this.clipboard(text);
      this.patch({ message: "错误日志已复制" });
    } catch (error) {
      this.patch({ error: this.report(error) });
    }
  }

  async checkUpdates(silent = false): Promise<void> {
    this.patch(silent ? { updateBusy: true } : { updateBusy: true, updateError: "", message: "" });
    try {
      const update = await this.backend.checkForUpdates();
      this.patch({ update, updateError: "" });
      if (!silent) {
        this.patch({ message: update.available ? `发现新版本 v${update.latest_version}` : "当前已是最新版本" });
      }
    } catch (error) {
      if (!silent) this.patch({ updateError: describe(error) });
    } finally {
      this.patch({ updateBusy: false });
    }
  }

  async installUpdate(): Promise<void> {
    this.patch({
      updateBusy: true,
      updateError: "",
      message: "",
      updateProgress: { phase: "preparing", percent: 0, message: "正在准备下载更新…" },
    });
    let started = false;
    try {
      await this.backend.installUpdate();
      started = true;
      this.patch({ message: "安装程序已启动，memo 即将重启" });
    } catch (error) {
      const text = this.report(error);
      this.patch({ updateProgress: null, updateError: text });
    } finally {
      if (!started) this.patch({ updateBusy: false });
    }
  }

  async openReleasePage(): Promise<void> {
    try {
      await this.backend.openReleasePage();
    } catch (error) {
      this.patch({ updateError: this.report(error) });
    }
  }

  /** Subscribes to the events that stream into this page. Returns a detach. */
  attachEvents(): () => void {
    const offRestore = this.subscribeTo("restore:progress", (progress) => {
      this.patch({ restore: progress as RestoreProgress });
    });
    const offAvailable = this.subscribeTo("update:available", (info) => {
      if (info && typeof info === "object") this.patch({ update: info as UpdateInfo });
    });
    const offProgress = this.subscribeTo("update:progress", (progress) => {
      this.patch({ updateProgress: progress as UpdateProgress, updateBusy: true });
    });
    return () => {
      offRestore();
      offAvailable();
      offProgress();
    };
  }

  private report(error: unknown): string {
    const text = describe(error);
    this.onError(text);
    return text;
  }

  private patch(next: Partial<SettingsState>) {
    this.state = { ...this.state, ...next };
    for (const listener of this.listeners) listener(this.state);
  }
}
