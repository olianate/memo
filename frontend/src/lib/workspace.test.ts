import { afterEach, describe, expect, it, vi } from "vitest";
import type { Memo, SearchResult, Settings, SyncStatus } from "../types";
import { dashboard, memo, searchResult } from "../test/fixtures";
import type { SettingsState } from "./settings";
import type { SettingsPort } from "./workspace";
import { WorkspaceController, type WorkspaceStorage, type WorkspaceTimer } from "./workspace";

const appEvents = vi.hoisted(() => new Map<string, (data: unknown) => void>());

vi.mock("../backend", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../backend")>();
  return {
    ...actual,
    isWailsRuntime: () => false,
    onAppEvent: (event: string, handler: (data: unknown) => void) => {
      appEvents.set(event, handler);
      return () => appEvents.delete(event);
    },
  };
});

class FakeTimer implements WorkspaceTimer {
  private next = 1;
  private tasks = new Map<number, () => void>();

  setTimeout(handler: () => void): number {
    const id = this.next++;
    this.tasks.set(id, handler);
    return id;
  }

  clearTimeout(id: number): void {
    this.tasks.delete(id);
  }

  runAll(): void {
    const pending = [...this.tasks.values()];
    this.tasks.clear();
    pending.forEach((task) => task());
  }
}

function memoryStorage(seed: Record<string, string> = {}): WorkspaceStorage & { values: Record<string, string> } {
  const values = { ...seed };
  return {
    values,
    getItem: (key) => values[key] ?? null,
    setItem: (key, value) => {
      values[key] = value;
    },
  };
}

function settings(overrides: Partial<Settings> = {}): Settings {
  return {
    theme: "system",
    hotkey: "CommandOrControl+Shift+M",
    submit_on_enter: true,
    start_at_login: false,
    retention_days: 30,
    immediate_eviction: false,
    sync_enabled: true,
    sync_paused: false,
    notion_connected: true,
    workspace_name: "工作区",
    workspace_file_limit: 5 * 1024 * 1024,
    data_dir: "C:/memo",
    onboarded: true,
    mcp_enabled: false,
    mcp_port: 8765,
    mcp_running: false,
    version: "1.3.0",
    ...overrides,
  };
}

const syncStatus: SyncStatus = {
  state: "idle",
  enabled: true,
  paused: false,
  configured: true,
  active: true,
  pending: 0,
  retrying: 0,
  logs: [],
};

function fakeBackend() {
  return {
    list: vi.fn(async (): Promise<Memo[]> => [memo({ id: "m1", day: "2026-11-21" })]),
    get: vi.fn(async (id: string): Promise<Memo> => memo({ id, day: "2026-11-21", body: "已恢复正文" })),
    counts: vi.fn(async () => ({ notes: 1, tags: 0, days: 1 })),
    dashboard: vi.fn(async () => dashboard()),
    search: vi.fn(async (_query: string, _tag: string, _from: string, _to: string): Promise<SearchResult[]> => []),
    emptyTrash: vi.fn(async () => 0),
    syncNow: vi.fn(async () => {}),
    setSyncEnabled: vi.fn(async () => {}),
    setSyncPaused: vi.fn(async () => {}),
    takePendingView: vi.fn(async () => ""),
    setWindowActive: vi.fn(async () => {}),
    reportError: vi.fn(async () => {}),
  };
}

function fakeSettings(initial: Settings | null, sync: SyncStatus = syncStatus) {
  let state: SettingsState = {
    settings: initial,
    sync,
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
  const listeners = new Set<(next: SettingsState) => void>();
  const patch = (next: Partial<SettingsState>) => {
    state = { ...state, ...next };
    listeners.forEach((listener) => listener(state));
  };

  const port: SettingsPort = {
    onChanged: () => {},
    onError: () => {},
    getSnapshot: () => state,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    load: vi.fn(async () => {}),
    perform: vi.fn(async () => {}),
    attachEvents: vi.fn(() => () => {}),
    checkUpdates: vi.fn(async () => {}),
    reload: vi.fn(async () => {}),
    setSync: vi.fn((next: SyncStatus) => patch({ sync: next })),
    setTheme: vi.fn(async () => {}),
  };

  return {
    port,
    emit: (next: SettingsState) => {
      state = next;
      listeners.forEach((listener) => listener(next));
    },
    patch,
  };
}

const controllers: WorkspaceController[] = [];

function make(options: { initialSettings?: Settings | null; sync?: SyncStatus; storage?: WorkspaceStorage } = {}) {
  const backend = fakeBackend();
  const page = fakeSettings(
    options.initialSettings === undefined ? settings() : options.initialSettings,
    options.sync,
  );
  const timer = new FakeTimer();
  const storage = options.storage ?? memoryStorage();
  const controller = new WorkspaceController({
    backend,
    settings: page.port,
    timer,
    storage,
    report: vi.fn(),
    installErrorHandlers: () => () => {},
  });
  controllers.push(controller);
  return { controller, backend, settings: page, timer, storage };
}

afterEach(() => {
  controllers.splice(0).forEach((controller) => controller.dispose());
  appEvents.clear();
});

describe("WorkspaceController", () => {
  it("loads chrome immediately and memos only after onboarding", async () => {
    const { controller, backend, settings: page } = make({
      initialSettings: settings({ onboarded: false }),
    });

    controller.start();
    await vi.waitFor(() => expect(backend.dashboard).toHaveBeenCalled());
    expect(backend.counts).toHaveBeenCalledOnce();
    expect(backend.list).not.toHaveBeenCalled();

    page.emit({ ...page.port.getSnapshot(), settings: settings({ onboarded: true }) });
    await vi.waitFor(() => expect(controller.getSnapshot().memos).toHaveLength(1));
    expect(backend.list).toHaveBeenCalledWith({});
  });

  it("resets navigation state and refetches the next view", async () => {
    const { controller, backend } = make();
    controller.start();
    await vi.waitFor(() => expect(controller.getSnapshot().loading).toBe(false));

    controller.setTag("工作");
    await vi.waitFor(() => expect(backend.list).toHaveBeenLastCalledWith({ tag: "工作" }));
    expect(controller.getSnapshot()).toMatchObject({ view: "tag", activeTag: "工作" });

    controller.setQuery("旧查询");
    controller.setView("trash");
    expect(controller.getSnapshot()).toMatchObject({
      view: "trash",
      activeTag: "",
      query: "",
      results: [],
      searching: false,
      showFilters: false,
    });
    await vi.waitFor(() => expect(backend.list).toHaveBeenLastCalledWith({ deleted: "deleted" }));
  });

  it("drops stale search responses", async () => {
    const { controller, backend, timer } = make();
    controller.start();
    await vi.waitFor(() => expect(controller.getSnapshot().loading).toBe(false));

    let resolveOld!: (value: SearchResult[]) => void;
    let resolveNew!: (value: SearchResult[]) => void;
    const old = new Promise<SearchResult[]>((resolve) => (resolveOld = resolve));
    const fresh = new Promise<SearchResult[]>((resolve) => (resolveNew = resolve));
    backend.search.mockImplementation((query: string) => (query === "旧" ? old : fresh));

    controller.setQuery("旧");
    timer.runAll();
    controller.setQuery("新");
    timer.runAll();

    resolveNew([searchResult({ match: "新结果" })]);
    await vi.waitFor(() => expect(controller.getSnapshot().results[0]?.match).toBe("新结果"));

    resolveOld([searchResult({ match: "旧结果" })]);
    await Promise.resolve();
    await Promise.resolve();
    expect(controller.getSnapshot().results[0]?.match).toBe("新结果");
    expect(controller.getSnapshot().searching).toBe(false);
  });

  it("hydrates an evicted memo when it is selected", async () => {
    const { controller, backend } = make();
    controller.start();
    await vi.waitFor(() => expect(controller.getSnapshot().loading).toBe(false));

    const evicted = memo({ id: "e1", day: "2026-11-21", local_state: "evicted", body: "" });
    await controller.openMemo(evicted);

    expect(backend.get).toHaveBeenCalledWith("e1");
    expect(controller.getSnapshot().selected).toMatchObject({ id: "e1", body: "已恢复正文" });
  });

  it("empties the trash and expires its toast", async () => {
    const { controller, backend, timer } = make();
    backend.emptyTrash.mockResolvedValue(2);
    controller.start();
    await vi.waitFor(() => expect(controller.getSnapshot().loading).toBe(false));

    await controller.emptyTrash();
    expect(backend.emptyTrash).toHaveBeenCalledOnce();
    expect(controller.getSnapshot().emptyingTrash).toBe(false);
    expect(controller.getSnapshot().toast).toBe("已永久删除 2 条笔记");
    expect(backend.list).toHaveBeenCalledTimes(2);

    timer.runAll();
    expect(controller.getSnapshot().toast).toBe("");
  });

  it("pauses and resumes sync from the shell", async () => {
    const { controller, backend, settings: page } = make();
    controller.start();
    await vi.waitFor(() => expect(controller.getSnapshot().loading).toBe(false));

    await controller.toggleSync();
    expect(backend.setSyncPaused).toHaveBeenCalledWith(true);
    expect(page.port.reload).toHaveBeenCalled();
    expect(controller.getSnapshot().toast).toBe("同步已暂停");

    page.patch({ sync: { ...syncStatus, paused: true } });
    await controller.toggleSync();
    expect(backend.setSyncPaused).toHaveBeenLastCalledWith(false);
    expect(controller.getSnapshot().toast).toBe("同步已恢复");
  });

  it("sends an unconfigured sync intent to settings", async () => {
    const { controller, backend } = make({
      sync: { ...syncStatus, configured: false, enabled: false },
    });
    controller.start();
    await vi.waitFor(() => expect(controller.getSnapshot().loading).toBe(false));

    await controller.syncNow();
    expect(backend.syncNow).not.toHaveBeenCalled();
    expect(controller.getSnapshot().view).toBe("settings");
    expect(controller.getSnapshot().toast).toBe("请先在设置中连接 Notion 并启用同步");
  });

  it("persists the sidebar preference", () => {
    const storage = memoryStorage({ "memo.sidebar.open": "0" });
    const first = make({ storage });
    expect(first.controller.getSnapshot().sidebarOpen).toBe(false);

    first.controller.setSidebarOpen(true);
    expect(storage.values["memo.sidebar.open"]).toBe("1");
    expect(first.controller.getSnapshot().sidebarOpen).toBe(true);
  });

  it("routes shell events into navigation, refresh, and settings", async () => {
    const { controller, settings: page, timer } = make();
    controller.start();
    await vi.waitFor(() => expect(appEvents.size).toBeGreaterThan(0));

    appEvents.get("sync:status")?.({ ...syncStatus, state: "syncing" });
    expect(page.port.setSync).toHaveBeenCalledWith({ ...syncStatus, state: "syncing" });

    appEvents.get("tags:changed")?.({});
    appEvents.get("ui:navigate")?.("settings");
    expect(controller.getSnapshot().view).toBe("settings");

    timer.runAll();
    expect(page.port.reload).toHaveBeenCalledOnce();
  });

  it("reloads settings during an explicit refresh", async () => {
    const { controller, settings: page } = make();
    controller.start();
    await vi.waitFor(() => expect(controller.getSnapshot().loading).toBe(false));

    controller.refresh();
    expect(page.port.reload).toHaveBeenCalledOnce();
  });

  it("applies a pending view from the OS on start", async () => {
    const { controller, backend } = make();
    backend.takePendingView.mockResolvedValue("settings");

    controller.start();
    await vi.waitFor(() => expect(controller.getSnapshot().view).toBe("settings"));
  });
});
