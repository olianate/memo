import { describe, expect, it, vi } from "vitest";
import type { Backend } from "../backend";
import type { Settings, SyncLog, SyncStatus } from "../types";
import { SettingsController } from "./settings";

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

const errorLog: SyncLog = { id: 1, at: "2026-11-21T09:00:00Z", level: "warn", event: "sync", message: "boom" };

function fakeBackend(overrides: Partial<Backend> = {}): Backend {
  const base = {
    settings: vi.fn(async () => settings()),
    syncStatus: vi.fn(async () => syncStatus),
    tags: vi.fn(async () => []),
    errorLogs: vi.fn(async () => [] as SyncLog[]),
    ...overrides,
  };
  return base as unknown as Backend;
}

describe("SettingsController", () => {
  it("loads the snapshot and seeds the editable fields", async () => {
    const backend = fakeBackend({ tags: vi.fn(async () => [{ id: "t1", name: "工作", normalized: "工作", count: 3 }]) });
    const page = new SettingsController(backend);
    await page.load();
    const state = page.getSnapshot();
    expect(state.settings?.workspace_name).toBe("工作区");
    expect(state.sync).toBe(syncStatus);
    expect(state.tags.map((tag) => tag.name)).toEqual(["工作"]);
    expect(state.hotkey).toBe("CommandOrControl+Shift+M");
    expect(state.mcpPort).toBe("8765");
  });

  it("keeps unsaved drafts across a background reload", async () => {
    const page = new SettingsController(fakeBackend());
    await page.load();
    page.setHotkey("CommandOrControl+Alt+M");
    page.setMcpPort("9000");

    await page.reload();

    expect(page.getSnapshot().hotkey).toBe("CommandOrControl+Alt+M");
    expect(page.getSnapshot().mcpPort).toBe("9000");
  });

  it("keeps loading when the error log request fails", async () => {
    const backend = fakeBackend({ errorLogs: vi.fn(async () => Promise.reject(new Error("日志不可用"))) });
    const page = new SettingsController(backend);
    await page.load();
    expect(page.getSnapshot().settings).not.toBeNull();
    expect(page.getSnapshot().errors).toEqual([]);
  });

  it("reports a failed load without throwing", async () => {
    const onError = vi.fn();
    const backend = fakeBackend({ settings: vi.fn(async () => Promise.reject(new Error("读取失败"))) });
    const page = new SettingsController(backend, { onError });
    await page.load();
    expect(page.getSnapshot().error).toBe("读取失败");
    expect(onError).toHaveBeenCalledWith("读取失败");
  });

  it("runs a mutation through busy, message, reload, and change notification", async () => {
    const onChanged = vi.fn();
    const setTheme = vi.fn(async () => {});
    const backend = fakeBackend({ setTheme });
    const page = new SettingsController(backend, { onChanged });
    const seen: boolean[] = [];
    page.subscribe((state) => seen.push(state.busy));

    await page.perform(() => setTheme(), "主题已更新");

    expect(setTheme).toHaveBeenCalledOnce();
    expect(seen).toContain(true);
    expect(page.getSnapshot().busy).toBe(false);
    expect(page.getSnapshot().message).toBe("主题已更新");
    expect(page.getSnapshot().error).toBe("");
    expect(onChanged).toHaveBeenCalledOnce();
  });

  it("surfaces a failed mutation and clears busy", async () => {
    const onError = vi.fn();
    const onChanged = vi.fn();
    const backend = fakeBackend();
    const page = new SettingsController(backend, { onError, onChanged });
    await page.perform(async () => {
      throw new Error("端口被占用");
    }, "不应出现");
    expect(page.getSnapshot().error).toBe("端口被占用");
    expect(page.getSnapshot().message).toBe("");
    expect(page.getSnapshot().busy).toBe(false);
    expect(onError).toHaveBeenCalledWith("端口被占用");
    expect(onChanged).not.toHaveBeenCalled();
  });

  it("applies a theme without resetting another field's draft", async () => {
    const setTheme = vi.fn(async () => {});
    const page = new SettingsController(
      fakeBackend({
        setTheme,
        settings: vi.fn(async () => settings({ theme: "dark" })),
      }),
    );
    await page.load();
    page.setHotkey("CommandOrControl+Alt+M");

    await page.setTheme("dark");

    expect(setTheme).toHaveBeenCalledWith("dark");
    expect(page.getSnapshot().settings?.theme).toBe("dark");
    expect(page.getSnapshot().hotkey).toBe("CommandOrControl+Alt+M");
  });

  it("stores the backup result and the restore progress", async () => {
    const backend = fakeBackend({
      exportBackup: vi.fn(async () => ({ path: "C:/memo/backup.zip", bytes: 42, at: "2026-11-21T09:00:00Z" })),
      restoreAll: vi.fn(async () => ({ total: 3, completed: 3, failed: 0 })),
    });
    const page = new SettingsController(backend);
    await page.exportBackup();
    expect(page.getSnapshot().backup?.path).toBe("C:/memo/backup.zip");
    await page.restoreAll();
    expect(page.getSnapshot().restore).toEqual({ total: 3, completed: 3, failed: 0 });
    expect(page.getSnapshot().message).toBe("恢复完成");
  });

  it("exports logs and records their path", async () => {
    const backend = fakeBackend({ exportLogs: vi.fn(async () => "C:/memo/logs.txt") });
    const page = new SettingsController(backend);
    await page.exportLogs();
    expect(page.getSnapshot().logsPath).toBe("C:/memo/logs.txt");
  });

  it("copies the error log with timestamps", async () => {
    const clipboard = vi.fn(async () => {});
    const backend = fakeBackend({ errorLogs: vi.fn(async () => [errorLog]) });
    const page = new SettingsController(backend, { clipboard });
    await page.load();
    await page.copyErrors();
    expect(clipboard).toHaveBeenCalledWith("[2026-11-21T09:00:00.000Z] warn sync\nboom");
    expect(page.getSnapshot().message).toBe("错误日志已复制");
  });

  it("reports a clipboard failure inline", async () => {
    const onError = vi.fn();
    const backend = fakeBackend({ errorLogs: vi.fn(async () => [errorLog]) });
    const page = new SettingsController(backend, {
      clipboard: async () => {
        throw new Error("剪贴板不可用");
      },
      onError,
    });
    await page.load();
    await page.copyErrors();
    expect(page.getSnapshot().error).toBe("剪贴板不可用");
    expect(onError).toHaveBeenCalledWith("剪贴板不可用");
  });

  it("announces an update only when the check is not silent", async () => {
    const checkForUpdates = vi.fn(async () => ({
      current_version: "1.3.0",
      latest_version: "1.4.0",
      available: true,
      notes: "",
      published_at: "",
      release_url: "",
      asset_name: "",
      asset_size: 0,
      can_install: true,
      checked_at: "2026-11-21T09:00:00Z",
    }));
    const page = new SettingsController(fakeBackend({ checkForUpdates }));
    await page.checkUpdates(true);
    expect(page.getSnapshot().message).toBe("");
    await page.checkUpdates();
    expect(page.getSnapshot().message).toBe("发现新版本 v1.4.0");
    expect(page.getSnapshot().updateBusy).toBe(false);
  });

  it("keeps the update spinner while the installer takes over", async () => {
    const backend = fakeBackend({ installUpdate: vi.fn(async () => {}) });
    const page = new SettingsController(backend);
    await page.installUpdate();
    expect(page.getSnapshot().message).toBe("安装程序已启动，memo 即将重启");
    expect(page.getSnapshot().updateBusy).toBe(true);
    expect(page.getSnapshot().updateProgress).not.toBeNull();
  });

  it("returns to idle when the installer fails", async () => {
    const onError = vi.fn();
    const backend = fakeBackend({
      installUpdate: vi.fn(async () => {
        throw new Error("没有写入权限");
      }),
    });
    const page = new SettingsController(backend, { onError });
    await page.installUpdate();
    expect(page.getSnapshot().updateError).toBe("没有写入权限");
    expect(page.getSnapshot().updateBusy).toBe(false);
    expect(page.getSnapshot().updateProgress).toBeNull();
    expect(onError).toHaveBeenCalledWith("没有写入权限");
  });

  it("routes app events into state and detaches them", async () => {
    const handlers = new Map<string, (data: unknown) => void>();
    const subscribe = (event: string, handler: (data: unknown) => void) => {
      handlers.set(event, handler);
      return () => handlers.delete(event);
    };
    const page = new SettingsController(fakeBackend(), { subscribe });
    const detach = page.attachEvents();
    handlers.get("restore:progress")?.({ total: 2, completed: 1, failed: 0 });
    handlers.get("update:progress")?.({ phase: "downloading", percent: 40, message: "下载中" });
    expect(page.getSnapshot().restore?.completed).toBe(1);
    expect(page.getSnapshot().updateBusy).toBe(true);
    expect(page.getSnapshot().updateProgress?.percent).toBe(40);
    detach();
    expect(handlers.size).toBe(0);
  });
});
