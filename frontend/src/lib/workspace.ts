import { useCallback, useRef, useSyncExternalStore } from "react";
import type { Backend } from "../backend";
import { isWailsRuntime, onAppEvent } from "../backend";
import type {
  Counts,
  Dashboard,
  Filter,
  Memo,
  SearchResult,
  Settings,
  SyncStatus,
  UpdateInfo,
  ViewId,
} from "../types";
import { currentMonth, endOfDayIso, startOfDayIso } from "./format";
import { installGlobalErrorHandlers, reportError, setErrorSink } from "./errorLog";
import type { SettingsController } from "./settings";

/** The backend calls the workspace makes; settings remain SettingsController's. */
export type WorkspaceBackend = Pick<
  Backend,
  | "list"
  | "get"
  | "counts"
  | "dashboard"
  | "search"
  | "emptyTrash"
  | "syncNow"
  | "setSyncEnabled"
  | "setSyncPaused"
  | "takePendingView"
  | "setWindowActive"
  | "reportError"
>;

/** The part of SettingsController the workspace composes. */
export type SettingsPort = Pick<
  SettingsController,
  | "getSnapshot"
  | "subscribe"
  | "load"
  | "perform"
  | "attachEvents"
  | "checkUpdates"
  | "reload"
  | "setSync"
  | "setTheme"
  | "onChanged"
  | "onError"
>;

export interface WorkspaceTimer {
  setTimeout(handler: () => void, ms: number): number;
  clearTimeout(id: number): void;
}

export interface WorkspaceStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

export interface WorkspaceOptions {
  backend: WorkspaceBackend;
  settings: SettingsPort;
  timer?: WorkspaceTimer;
  storage?: WorkspaceStorage | null;
  report?: (error: unknown, context?: unknown, source?: string) => void;
  installErrorHandlers?: () => () => void;
}

export interface WorkspaceState {
  view: ViewId;
  activeTag: string;
  from: string;
  to: string;
  memos: Memo[];
  counts: Counts;
  dashboard: Dashboard | null;
  loading: boolean;
  selected: Memo | null;
  query: string;
  searching: boolean;
  results: SearchResult[];
  error: string;
  toast: string;
  showFilters: boolean;
  emptyingTrash: boolean;
  online: boolean;
  sidebarOpen: boolean;
}

const sidebarPrefKey = "memo.sidebar.open";

const emptyCounts: Counts = { notes: 0, tags: 0, days: 0 };

const browserTimer: WorkspaceTimer = {
  setTimeout: (handler, ms) => window.setTimeout(handler, ms),
  clearTimeout: (id) => window.clearTimeout(id),
};

function browserStorage(): WorkspaceStorage | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function viewFilter(view: ViewId, tag: string, from: string, to: string): Filter {
  const filter: Filter = {};
  if (view === "local") filter.local_only = true;
  if (view === "synced") filter.synced = true;
  if (view === "attachments") filter.with_attachment = true;
  if (view === "untagged") filter.untagged = true;
  if (view === "trash") filter.deleted = "deleted";
  if (view === "tag" && tag) filter.tag = tag;
  if (from) filter.from = startOfDayIso(from);
  if (to) filter.to = endOfDayIso(to);
  return filter;
}

/**
 * Owns the shell's navigation, memo stream, search, selection, refresh
 * invalidation, and transient UI state. SettingsController stays the single
 * owner of settings, sync, tags, and theme; this module composes that snapshot
 * and applies the theme without copying it.
 */
export class WorkspaceController {
  private readonly backend: WorkspaceBackend;
  private readonly settings: SettingsPort;
  private readonly timer: WorkspaceTimer;
  private readonly storage: WorkspaceStorage | null;
  private readonly report: (error: unknown, context?: unknown, source?: string) => void;
  private readonly installHandlers: () => () => void;

  private state: WorkspaceState;
  private readonly listeners = new Set<(state: WorkspaceState) => void>();
  private readonly disposers: Array<() => void> = [];

  private started = false;
  private onboarded = false;
  private themedSettings: Settings | null = null;
  private chromeGeneration = 0;
  private memoGeneration = 0;
  private searchGeneration = 0;
  private refreshTimer: number | undefined;
  private searchTimer: number | undefined;
  private toastTimer: number | undefined;
  private themeCleanup: (() => void) | null = null;

  constructor(options: WorkspaceOptions) {
    this.backend = options.backend;
    this.settings = options.settings;
    this.timer = options.timer ?? browserTimer;
    this.storage = options.storage === undefined ? browserStorage() : options.storage;
    this.report = options.report ?? reportError;
    this.installHandlers = options.installErrorHandlers ?? installGlobalErrorHandlers;

    let sidebarOpen = true;
    try {
      sidebarOpen = this.storage?.getItem(sidebarPrefKey) !== "0";
    } catch {
      sidebarOpen = true;
    }

    this.state = {
      view: "all",
      activeTag: "",
      from: "",
      to: "",
      memos: [],
      counts: emptyCounts,
      dashboard: null,
      loading: true,
      selected: null,
      query: "",
      searching: false,
      results: [],
      error: "",
      toast: "",
      showFilters: false,
      emptyingTrash: false,
      online: typeof navigator === "undefined" ? true : navigator.onLine,
      sidebarOpen,
    };

    this.settings.onChanged = () => this.refresh();
    this.settings.onError = (message) => this.fail(message);
  }

  subscribe = (listener: (state: WorkspaceState) => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getSnapshot = (): WorkspaceState => this.state;

  start = () => {
    if (this.started) return;
    this.started = true;

    setErrorSink((report) => this.backend.reportError(report));
    const removeHandlers = this.installHandlers();
    this.disposers.push(() => setErrorSink(null), removeHandlers);

    this.disposers.push(this.settings.subscribe(this.handleSettings));
    this.handleSettings(this.settings.getSnapshot());
    this.disposers.push(this.settings.attachEvents());

    this.attachWindow();
    this.attachEvents();

    void this.settings.load();
    void this.settings.checkUpdates(true);
    void this.loadChrome();

    document.querySelector<HTMLElement>(".content")?.scrollTo?.({ top: 0 });
  };

  dispose = () => {
    if (!this.started) return;
    this.started = false;
    for (const off of this.disposers.splice(0)) off();
    this.clearThemeListener();
    this.clearTimer("refresh");
    this.clearTimer("search");
    this.clearTimer("toast");
    this.chromeGeneration += 1;
    this.memoGeneration += 1;
    this.searchGeneration += 1;
    this.onboarded = false;
    this.themedSettings = null;
  };

  setView = (view: ViewId) => {
    this.cancelSearch();
    this.patch({
      view,
      activeTag: "",
      query: "",
      showFilters: false,
      results: [],
      searching: false,
    });
    void this.loadMemos();
  };

  setTag = (name: string) => {
    this.cancelSearch();
    this.patch({ activeTag: name, view: "tag", query: "", results: [], searching: false });
    void this.loadMemos();
  };

  setQuery = (value: string) => {
    this.patch({ query: value });
    this.scheduleSearch();
  };

  setFrom = (value: string) => {
    this.patch({ from: value });
    this.scheduleSearch();
    void this.loadMemos();
  };

  setTo = (value: string) => {
    this.patch({ to: value });
    this.scheduleSearch();
    void this.loadMemos();
  };

  clearDates = () => {
    this.patch({ from: "", to: "" });
    this.scheduleSearch();
    void this.loadMemos();
  };

  toggleFilters = () => this.patch({ showFilters: !this.state.showFilters });

  toggleSidebar = () => this.setSidebarOpen(!this.state.sidebarOpen);

  setSidebarOpen = (open: boolean) => {
    try {
      this.storage?.setItem(sidebarPrefKey, open ? "1" : "0");
    } catch {
      // The layout preference is optional; ignore storage failures.
    }
    this.patch({ sidebarOpen: open });
  };

  refresh = () => {
    void this.settings.reload();
    void this.loadChrome();
    void this.loadMemos();
  };

  openMemo = async (memo: Memo) => {
    this.patch({ selected: memo });
    if (memo.local_state !== "evicted") return;
    // Selecting an evicted memo hydrates it from Notion. If that fails (for
    // example offline), the sheet still opens with a retry placeholder.
    try {
      const fresh = await this.backend.get(memo.id);
      if (this.state.selected?.id === memo.id) this.patch({ selected: fresh });
      this.scheduleRefresh();
    } catch {
      /* keep the placeholder */
    }
  };

  closeMemo = () => this.patch({ selected: null });

  memoChanged = (memo: Memo | null) => {
    this.patch({ selected: memo });
    this.refresh();
  };

  emptyTrash = async () => {
    this.patch({ emptyingTrash: true });
    try {
      const count = await this.backend.emptyTrash();
      this.notify(count > 0 ? `已永久删除 ${count} 条笔记` : "回收站已经是空的");
    } catch (error) {
      this.fail(describe(error));
    } finally {
      this.patch({ emptyingTrash: false });
      this.refresh();
    }
  };

  syncNow = async () => {
    const sync = this.settings.getSnapshot().sync;
    if (!sync?.configured || !sync.enabled) {
      this.setView("settings");
      this.notify("请先在设置中连接 Notion 并启用同步");
      return;
    }
    try {
      await this.backend.syncNow();
      this.notify("同步完成");
      this.refresh();
    } catch (error) {
      this.fail(describe(error));
    }
  };

  toggleSync = async () => {
    const sync = this.settings.getSnapshot().sync;
    if (!sync?.configured) {
      this.setView("settings");
      this.notify("请先在设置中连接 Notion");
      return;
    }
    try {
      if (!sync.enabled) {
        await this.backend.setSyncEnabled(true);
        if (sync.paused) await this.backend.setSyncPaused(false);
        this.notify("同步已开启");
      } else if (sync.paused) {
        await this.backend.setSyncPaused(false);
        this.notify("同步已恢复");
      } else {
        await this.backend.setSyncPaused(true);
        this.notify("同步已暂停");
      }
      await this.settings.reload();
      void this.loadChrome();
      void this.loadMemos();
    } catch (error) {
      this.fail(describe(error));
    }
  };

  toggleTheme = async () => {
    const settings = this.settings.getSnapshot().settings;
    if (!settings) return;
    const media =
      typeof window !== "undefined" && typeof window.matchMedia === "function"
        ? window.matchMedia("(prefers-color-scheme: dark)")
        : null;
    const resolved =
      settings.theme === "system" ? (media?.matches ? "dark" : "light") : settings.theme;
    await this.settings.setTheme(resolved === "dark" ? "light" : "dark");
  };

  fail = (message: string) => {
    this.report(message, { kind: "ui.error_banner" }, "app");
    this.patch({ error: message });
  };

  clearError = () => this.patch({ error: "" });

  notify = (message: string) => {
    this.patch({ toast: message });
    this.clearTimer("toast");
    this.toastTimer = this.timer.setTimeout(() => {
      this.toastTimer = undefined;
      this.patch({ toast: "" });
    }, 3200);
  };

  private handleSettings = (state: { settings: Settings | null }) => {
    if (state.settings !== this.themedSettings) {
      this.themedSettings = state.settings;
      this.applyTheme(state.settings);
    }
    if (state.settings?.onboarded) {
      if (!this.onboarded) {
        this.onboarded = true;
        void this.loadMemos();
      }
    } else {
      this.onboarded = false;
    }
  };

  private applyTheme(settings: Settings | null) {
    this.clearThemeListener();
    if (!settings || typeof document === "undefined") return;
    const media =
      typeof window !== "undefined" && typeof window.matchMedia === "function"
        ? window.matchMedia("(prefers-color-scheme: dark)")
        : null;
    const apply = () => {
      const resolved =
        settings.theme === "system" ? (media?.matches ? "dark" : "light") : settings.theme;
      document.documentElement.dataset.theme = resolved;
    };
    apply();
    if (settings.theme === "system" && media) {
      media.addEventListener("change", apply);
      this.themeCleanup = () => media.removeEventListener("change", apply);
    }
  }

  private clearThemeListener() {
    this.themeCleanup?.();
    this.themeCleanup = null;
  }

  private attachWindow() {
    const updateOnline = () => {
      this.patch({ online: typeof navigator === "undefined" ? true : navigator.onLine });
    };
    const onKey = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        const input = document.querySelector<HTMLInputElement>(".search-box input");
        input?.focus();
        input?.select();
      }
    };
    const applyPendingView = () => {
      void this.backend
        .takePendingView()
        .then((target) => {
          if (target === "settings") this.setView("settings");
        })
        .catch(() => {});
    };

    window.addEventListener("online", updateOnline);
    window.addEventListener("offline", updateOnline);
    window.addEventListener("keydown", onKey);
    window.addEventListener("focus", applyPendingView);
    this.disposers.push(() => {
      window.removeEventListener("online", updateOnline);
      window.removeEventListener("offline", updateOnline);
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("focus", applyPendingView);
    });

    if (isWailsRuntime()) {
      void this.backend.setWindowActive(document.hasFocus()).catch(() => {});
      const focus = () => void this.backend.setWindowActive(true).catch(() => {});
      const blur = () => void this.backend.setWindowActive(false).catch(() => {});
      window.addEventListener("focus", focus);
      window.addEventListener("blur", blur);
      this.disposers.push(() => {
        window.removeEventListener("focus", focus);
        window.removeEventListener("blur", blur);
      });
    }

    applyPendingView();
  }

  private attachEvents() {
    const offs = [
      onAppEvent("memos:changed", () => this.scheduleRefresh()),
      onAppEvent("tags:changed", () => this.scheduleRefresh()),
      onAppEvent("sync:status", (data) => {
        if (data && typeof data === "object") this.settings.setSync(data as SyncStatus);
        this.scheduleRefresh();
      }),
      onAppEvent("sync:error", (message) => this.fail(String(message))),
      onAppEvent("retention:evicted", () => this.scheduleRefresh()),
      onAppEvent("restore:done", () => this.scheduleRefresh()),
      onAppEvent("ui:navigate", (target) => {
        if (target === "settings") this.setView("settings");
      }),
      onAppEvent("update:available", (data) => {
        if (data && typeof data === "object") {
          const info = data as UpdateInfo;
          this.notify(`发现新版本 v${info.latest_version}，可在设置中安装`);
        }
      }),
    ];
    this.disposers.push(() => offs.forEach((off) => off()));
  }

  private scheduleRefresh() {
    this.clearTimer("refresh");
    this.refreshTimer = this.timer.setTimeout(() => {
      this.refreshTimer = undefined;
      this.refresh();
    }, 150);
  }

  private async loadChrome() {
    const generation = ++this.chromeGeneration;
    try {
      const [counts, dashboard] = await Promise.all([
        this.backend.counts(),
        this.backend.dashboard(currentMonth()),
      ]);
      if (generation !== this.chromeGeneration) return;
      this.patch({ counts, dashboard });
    } catch (error) {
      if (generation === this.chromeGeneration) this.fail(describe(error));
    }
  }

  private async loadMemos() {
    const view = this.state.view;
    if (view === "stats" || view === "settings") {
      this.memoGeneration += 1;
      this.patch({ loading: false });
      return;
    }
    const generation = ++this.memoGeneration;
    this.patch({ loading: true });
    try {
      const rows = await this.backend.list(viewFilter(view, this.state.activeTag, this.state.from, this.state.to));
      if (generation === this.memoGeneration) this.patch({ memos: rows });
    } catch (error) {
      if (generation === this.memoGeneration) this.fail(describe(error));
    } finally {
      if (generation === this.memoGeneration) this.patch({ loading: false });
    }
  }

  private scheduleSearch() {
    this.clearTimer("search");
    const text = this.state.query.trim();
    if (!text) {
      this.searchGeneration += 1;
      this.patch({ results: [], searching: false });
      return;
    }
    const generation = ++this.searchGeneration;
    this.patch({ searching: true });
    this.searchTimer = this.timer.setTimeout(() => {
      this.searchTimer = undefined;
      void this.runSearch(generation, text);
    }, 220);
  }

  private async runSearch(generation: number, text: string) {
    try {
      const rows = await this.backend.search(
        text,
        this.state.activeTag,
        this.state.from ? startOfDayIso(this.state.from) : "",
        this.state.to ? endOfDayIso(this.state.to) : "",
      );
      if (generation === this.searchGeneration) this.patch({ results: rows });
    } catch (error) {
      if (generation === this.searchGeneration) this.fail(describe(error));
    } finally {
      if (generation === this.searchGeneration) this.patch({ searching: false });
    }
  }

  private cancelSearch() {
    this.clearTimer("search");
    this.searchGeneration += 1;
  }

  private clearTimer(kind: "refresh" | "search" | "toast") {
    if (kind === "refresh" && this.refreshTimer !== undefined) {
      this.timer.clearTimeout(this.refreshTimer);
      this.refreshTimer = undefined;
    }
    if (kind === "search" && this.searchTimer !== undefined) {
      this.timer.clearTimeout(this.searchTimer);
      this.searchTimer = undefined;
    }
    if (kind === "toast" && this.toastTimer !== undefined) {
      this.timer.clearTimeout(this.toastTimer);
      this.toastTimer = undefined;
    }
  }

  private patch(next: Partial<WorkspaceState>) {
    this.state = { ...this.state, ...next };
    for (const listener of this.listeners) listener(this.state);
  }
}

export function useWorkspace(controller: WorkspaceController): WorkspaceState {
  return useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getSnapshot);
}

export function useWorkspaceSelector<T>(
  controller: WorkspaceController,
  selector: (state: WorkspaceState) => T,
  isEqual: (a: T, b: T) => boolean = Object.is,
): T {
  const selectorRef = useRef(selector);
  selectorRef.current = selector;
  const isEqualRef = useRef(isEqual);
  isEqualRef.current = isEqual;
  const cache = useRef<{
    state: WorkspaceState;
    selector: (state: WorkspaceState) => T;
    value: T;
  } | null>(null);

  const getSnapshot = useCallback(() => {
    const state = controller.getSnapshot();
    const activeSelector = selectorRef.current;
    const cached = cache.current;
    if (cached && cached.state === state && cached.selector === activeSelector) return cached.value;
    const value = activeSelector(state);
    if (cached && isEqualRef.current(cached.value, value)) {
      cache.current = { state, selector: activeSelector, value: cached.value };
      return cached.value;
    }
    cache.current = { state, selector: activeSelector, value };
    return value;
  }, [controller]);

  return useSyncExternalStore(controller.subscribe, getSnapshot, getSnapshot);
}
