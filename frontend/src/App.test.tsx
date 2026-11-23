import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import App from "./App";
import { demoBackend } from "./demoBackend";
import { memo } from "./test/fixtures";
import type { Backend } from "./backend";
import type { SyncStatus } from "./types";

describe("App", () => {
  it("reloads the timeline after a save", async () => {
    let rows = [] as ReturnType<typeof memo>[];
    const list = vi.fn(async () => rows);
    const create = vi.fn(async (blocks: any, tags: any) => {
      const saved = memo({ id: "m-new", day: "2026-11-21", blocks, tags: tags.map((name: string) => ({ id: name, name, normalized: name, count: 0 })) });
      rows = [saved];
      return saved;
    });
    const backend: Backend = {
      ...demoBackend,
      list,
      create,
      settings: async () => ({ ...(await demoBackend.settings()), onboarded: true }),
    };
    render(<App backend={backend} />);
    await screen.findByText("这里还很安静");
    const editor = document.querySelector<HTMLElement>(".ed-input");
    expect(editor).not.toBeNull();
    if (editor) {
      editor.textContent = "新笔记";
      fireEvent.input(editor);
    }
    fireEvent.click(screen.getByRole("button", { name: "保存笔记" }));
    await waitFor(() => expect(create).toHaveBeenCalled());
    expect(await screen.findByText("新笔记")).toBeInTheDocument();
    expect(list.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("empties the trash from the trash view", async () => {
    const trashed = await demoBackend.create(
      [{ id: "b-trash", type: "paragraph", spans: [{ text: "trash me" }] }],
      [],
    );
    await demoBackend.remove(trashed.id);
    const emptyTrash = vi.fn(async () => {
      const count = await demoBackend.emptyTrash();
      return count;
    });
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<App backend={{ ...demoBackend, emptyTrash }} />);

    fireEvent.click(await screen.findByRole("button", { name: /回收站/ }));
    fireEvent.click(await screen.findByRole("button", { name: "清空回收站" }));
    await waitFor(() => expect(emptyTrash).toHaveBeenCalled());
    expect(await screen.findByText("回收站是空的")).toBeInTheDocument();
    expect(screen.queryByText(/在上面的输入框记下第一个想法/)).not.toBeInTheDocument();
    confirm.mockRestore();
  });

  it("pauses sync from the topbar status button", async () => {
    let paused = false;
    const now = new Date().toISOString();
    const syncStatus = vi.fn(async (): Promise<SyncStatus> => ({
      state: paused ? "paused" : "idle",
      enabled: true,
      paused,
      configured: true,
      active: true,
      pending: 0,
      retrying: 0,
      last_success: now,
      workspace_name: "演示工作区",
      logs: [],
    }));
    const setSyncPaused = vi.fn(async (value: boolean) => {
      paused = value;
    });
    render(
      <App
        backend={{
          ...demoBackend,
          syncStatus,
          setSyncPaused,
        }}
      />,
    );

    fireEvent.click(await screen.findByRole("button", { name: "点击暂停同步" }));
    await waitFor(() => expect(setSyncPaused).toHaveBeenCalledWith(true));
    expect(await screen.findByRole("button", { name: "点击恢复同步" })).toBeInTheDocument();
  });

  it("hides the pending banner while sync is paused", async () => {
    const now = new Date().toISOString();
    render(
      <App
        backend={{
          ...demoBackend,
          syncStatus: async () => ({
            state: "paused",
            enabled: true,
            paused: true,
            configured: true,
            active: true,
            pending: 1,
            retrying: 0,
            last_success: now,
            workspace_name: "演示工作区",
            logs: [],
          }),
        }}
      />,
    );
    await screen.findByRole("button", { name: "点击恢复同步" });
    expect(screen.queryByText(/等待同步/)).not.toBeInTheDocument();
  });

  it("collapses and restores the sidebar", async () => {
    window.localStorage.setItem("memo.sidebar.open", "1");
    render(<App backend={demoBackend} />);

    expect(document.querySelector(".sidebar")).not.toBeNull();
    fireEvent.click(await screen.findByRole("button", { name: "收起侧边栏" }));
    expect(document.querySelector(".sidebar")).toBeNull();
    expect(await screen.findByRole("button", { name: "展开侧边栏" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "展开侧边栏" }));
    expect(document.querySelector(".sidebar")).not.toBeNull();
    expect(await screen.findByRole("button", { name: "收起侧边栏" })).toBeInTheDocument();
  });
});
