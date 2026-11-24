import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { SettingsPage } from "./SettingsPage";
import { demoBackend } from "../demoBackend";
import type { UpdateInfo } from "../types";

function updateInfo(overrides: Partial<UpdateInfo> = {}): UpdateInfo {
  return {
    current_version: "0.1.0",
    latest_version: "1.2.0",
    available: true,
    notes: "## 更新内容\n- 新增自动更新",
    published_at: new Date().toISOString(),
    release_url: "https://github.com/olianate/memo/releases/tag/v1.2.0",
    asset_name: "memo-windows-amd64.msi",
    asset_size: 8_000_000,
    can_install: true,
    checked_at: new Date().toISOString(),
    ...overrides,
  };
}

describe("SettingsPage MCP", () => {
  it("enables the MCP server on the configured port", async () => {
    const setMCP = vi.fn(async () => undefined);
    const backend = {
      ...demoBackend,
      settings: async () => ({ ...(await demoBackend.settings()), onboarded: true }),
      setMCP,
    };

    render(<SettingsPage backend={backend} onSettingsChanged={() => {}} onError={() => {}} />);

    fireEvent.click(await screen.findByRole("switch", { name: "启用 MCP 服务" }));
    await waitFor(() => expect(setMCP).toHaveBeenCalledWith(true, 8765));
  });
});

describe("SettingsPage logs", () => {
  it("clears local sync and error logs after confirmation", async () => {
    const clearLogs = vi.fn(async () => undefined);
    const errorLog = {
      id: 1,
      at: new Date().toISOString(),
      level: "error",
      event: "ui:test",
      message: "boom",
    };
    const backend = {
      ...demoBackend,
      settings: async () => ({ ...(await demoBackend.settings()), onboarded: true }),
      syncStatus: async () => ({ ...(await demoBackend.syncStatus()), logs: [errorLog] }),
      errorLogs: async () => [errorLog],
      clearLogs,
    };
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);

    render(<SettingsPage backend={backend} onSettingsChanged={() => {}} onError={() => {}} />);

    const button = await screen.findByRole("button", { name: "清空日志" });
    expect(button).toBeEnabled();
    fireEvent.click(button);
    await waitFor(() => expect(clearLogs).toHaveBeenCalledTimes(1));
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining("清空本地日志"));
    expect(await screen.findByText("日志已清空")).toBeInTheDocument();
    confirm.mockRestore();
  });

  it("shows only the five most recent sync events", async () => {
    const logs = Array.from({ length: 8 }, (_, index) => ({
      id: index + 1,
      at: new Date(Date.now() - index * 1000).toISOString(),
      level: "info",
      event: `event-${index + 1}`,
      message: `message-${index + 1}`,
    }));
    const backend = {
      ...demoBackend,
      settings: async () => ({ ...(await demoBackend.settings()), onboarded: true }),
      syncStatus: async () => ({ ...(await demoBackend.syncStatus()), logs }),
    };

    const { container } = render(<SettingsPage backend={backend} onSettingsChanged={() => {}} onError={() => {}} />);

    await screen.findByText("event-5");
    expect(container.querySelectorAll(".sync-log")).toHaveLength(5);
    expect(screen.queryByText("event-6")).not.toBeInTheDocument();
  });
});

describe("SettingsPage updates", () => {
  it("checks on mount and installs an available update", async () => {
    const checkForUpdates = vi.fn(async () => updateInfo());
    const installUpdate = vi.fn(async () => undefined);
    const backend = {
      ...demoBackend,
      settings: async () => ({ ...(await demoBackend.settings()), onboarded: true }),
      checkForUpdates,
      installUpdate,
    };

    render(<SettingsPage backend={backend} onSettingsChanged={() => {}} onError={() => {}} />);

    expect(await screen.findByText("关于")).toBeInTheDocument();
    const install = await screen.findByRole("button", { name: /安装 v1\.2\.0 并重启/ });
    expect(checkForUpdates).toHaveBeenCalledTimes(1);

    fireEvent.click(install);
    await waitFor(() => expect(installUpdate).toHaveBeenCalledTimes(1));
    expect(await screen.findByText(/安装程序已启动/)).toBeInTheDocument();
  });

  it("reports when the current version is up to date", async () => {
    const checkForUpdates = vi.fn(async () => updateInfo({ available: false, can_install: false, latest_version: "0.1.0" }));
    const backend = {
      ...demoBackend,
      settings: async () => ({ ...(await demoBackend.settings()), onboarded: true }),
      checkForUpdates,
    };

    render(<SettingsPage backend={backend} onSettingsChanged={() => {}} onError={() => {}} />);

    fireEvent.click(await screen.findByRole("button", { name: /检查更新/ }));
    expect(await screen.findByText("当前已是最新版本。")).toBeInTheDocument();
  });
});
