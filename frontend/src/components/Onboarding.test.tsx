import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Onboarding } from "./Onboarding";
import { demoBackend } from "../demoBackend";

const workspace = { name: "工作区", id: "w", file_limit: 1024, bot_type: "bot" };

async function openParentStep() {
  fireEvent.click(screen.getByRole("button", { name: "连接我的 Notion" }));
  fireEvent.change(screen.getByLabelText("Notion 集成令牌"), { target: { value: "secret" } });
  fireEvent.click(screen.getByRole("button", { name: "验证令牌" }));
}

describe("Onboarding", () => {
  it("skips Notion and completes first-run setup", async () => {
    const complete = vi.fn(async () => {});
    const onSkip = vi.fn();
    render(
      <Onboarding
        backend={{ ...demoBackend, completeOnboarding: complete }}
        onDone={() => {}}
        onSkip={onSkip}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "暂时不连接" }));
    await waitFor(() => expect(complete).toHaveBeenCalled());
    expect(onSkip).toHaveBeenCalled();
  });

  it("toggles the Notion token visibility and hides it on return", () => {
    render(<Onboarding backend={demoBackend} onDone={() => {}} onSkip={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "连接我的 Notion" }));

    const input = screen.getByLabelText("Notion 集成令牌");
    expect(input).toHaveAttribute("type", "password");

    fireEvent.click(screen.getByRole("button", { name: "显示令牌" }));
    expect(input).toHaveAttribute("type", "text");
    expect(screen.getByRole("button", { name: "隐藏令牌" })).toHaveAttribute("aria-pressed", "true");

    fireEvent.click(screen.getByRole("button", { name: "隐藏令牌" }));
    expect(input).toHaveAttribute("type", "password");

    fireEvent.click(screen.getByRole("button", { name: "显示令牌" }));
    fireEvent.click(screen.getByRole("button", { name: "返回" }));
    fireEvent.click(screen.getByRole("button", { name: "连接我的 Notion" }));

    expect(screen.getByLabelText("Notion 集成令牌")).toHaveAttribute("type", "password");
    expect(screen.getByRole("button", { name: "显示令牌" })).toHaveAttribute("aria-pressed", "false");
  });

  it("lists databases and pages and uses a shared database directly", async () => {
    const validate = vi.fn(async () => workspace);
    const parentPages = vi.fn(async () => ({
      databases: [{ id: "ds1", database_id: "db1", title: "shared memo db", url: "" }],
      pages: [{ id: "p1", title: "archive page", url: "" }],
    }));
    const preview = vi.fn(async () => ({
      id: "ds1",
      title: "shared memo db",
      title_property: "名称",
      missing_properties: [],
      conflicts: [],
      has_existing_rows: false,
    }));
    const connect = vi.fn(async () => ({
      workspace,
      data_source_id: "ds1",
      database_id: "db1",
      database_title: "shared memo db",
      title_property: "名称",
      created_database: false,
    }));
    render(
      <Onboarding
        backend={{ ...demoBackend, validateToken: validate, parentPages, previewArchiveDatabase: preview, connect }}
        onDone={() => {}}
        onSkip={() => {}}
      />,
    );

    await openParentStep();
    expect(await screen.findByRole("button", { name: /shared memo db/ })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /archive page/ })).toBeInTheDocument();
    await waitFor(() => expect(preview).toHaveBeenCalledWith("secret", "ds1"));
    expect(await screen.findByText("该数据库已包含 memo 所需属性，可直接使用。")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "使用此数据库并连接" }));
    await waitFor(() =>
      expect(connect).toHaveBeenCalledWith({
        token: "secret",
        kind: "database",
        target_id: "ds1",
        database_name: "",
        confirm_schema_changes: false,
      }),
    );
  });

  it("renders a ready shared database when the backend omits empty arrays", async () => {
    const parentPages = vi.fn(async () => ({
      databases: [{ id: "ds1", database_id: "db1", title: "shared memo db", url: "" }],
      pages: [],
    }));
    const preview = vi.fn(async () => ({
      id: "ds1",
      title: "shared memo db",
      title_property: "名称",
      missing_properties: null as any,
      conflicts: null as any,
      has_existing_rows: false,
    }));
    render(
      <Onboarding
        backend={{ ...demoBackend, validateToken: async () => workspace, parentPages, previewArchiveDatabase: preview }}
        onDone={() => {}}
        onSkip={() => {}}
      />,
    );

    await openParentStep();
    expect(await screen.findByText("该数据库已包含 memo 所需属性，可直接使用。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "使用此数据库并连接" })).toBeEnabled();
  });

  it("asks for a database name when a page is selected", async () => {
    const parentPages = vi.fn(async () => ({
      databases: [{ id: "ds1", database_id: "db1", title: "shared memo db", url: "" }],
      pages: [{ id: "p1", title: "archive page", url: "" }],
    }));
    const connect = vi.fn(async () => ({
      workspace,
      data_source_id: "ds-p1",
      database_id: "db-p1",
      database_title: "我的备忘",
      title_property: "名称",
      created_database: true,
    }));
    render(
      <Onboarding
        backend={{ ...demoBackend, validateToken: async () => workspace, parentPages, connect }}
        onDone={() => {}}
        onSkip={() => {}}
      />,
    );

    await openParentStep();
    fireEvent.click(await screen.findByText("archive page"));
    expect(await screen.findByLabelText("新数据库名称")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("新数据库名称"), { target: { value: "我的备忘" } });
    fireEvent.click(screen.getByRole("button", { name: "创建数据库并连接" }));

    await waitFor(() =>
      expect(connect).toHaveBeenCalledWith({
        token: "secret",
        kind: "page",
        target_id: "p1",
        database_name: "我的备忘",
        confirm_schema_changes: false,
      }),
    );
  });

  it("requires confirmation before adding missing schema properties", async () => {
    const parentPages = vi.fn(async () => ({
      databases: [{ id: "ds1", database_id: "db1", title: "shared memo db", url: "" }],
      pages: [],
    }));
    const preview = vi.fn(async () => ({
      id: "ds1",
      title: "shared memo db",
      title_property: "名称",
      missing_properties: [
        { name: "Memo ID", type: "rich_text" },
        { name: "Tags", type: "multi_select" },
      ],
      conflicts: [],
      has_existing_rows: true,
    }));
    const connect = vi.fn(async () => ({
      workspace,
      data_source_id: "ds1",
      database_id: "db1",
      database_title: "shared memo db",
      title_property: "名称",
      created_database: false,
    }));
    render(
      <Onboarding
        backend={{ ...demoBackend, validateToken: async () => workspace, parentPages, previewArchiveDatabase: preview, connect }}
        onDone={() => {}}
        onSkip={() => {}}
      />,
    );

    await openParentStep();
    expect(await screen.findByText(/连接时会新增 2 个属性/)).toBeInTheDocument();
    expect(screen.getByText(/这个数据库已有记录/)).toBeInTheDocument();
    const button = screen.getByRole("button", { name: "使用此数据库并连接" });
    expect(button).toBeDisabled();

    fireEvent.click(screen.getByRole("checkbox", { name: /我确认向这个数据库新增上述属性/ }));
    expect(button).toBeEnabled();
    fireEvent.click(button);
    await waitFor(() =>
      expect(connect).toHaveBeenCalledWith({
        token: "secret",
        kind: "database",
        target_id: "ds1",
        database_name: "",
        confirm_schema_changes: true,
      }),
    );
  });

  it("blocks a database with conflicting property types", async () => {
    const parentPages = vi.fn(async () => ({
      databases: [{ id: "ds1", database_id: "db1", title: "conflict db", url: "" }],
      pages: [],
    }));
    const preview = vi.fn(async () => ({
      id: "ds1",
      title: "conflict db",
      title_property: "名称",
      missing_properties: [],
      conflicts: [{ name: "Tags", existing_type: "rich_text", required_type: "multi_select" }],
      has_existing_rows: false,
    }));
    render(
      <Onboarding
        backend={{ ...demoBackend, validateToken: async () => workspace, parentPages, previewArchiveDatabase: preview }}
        onDone={() => {}}
        onSkip={() => {}}
      />,
    );

    await openParentStep();
    expect(await screen.findByText("无法使用这个数据库")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "使用此数据库并连接" })).toBeDisabled();
  });

  it("resolves a pasted database link when search returns nothing", async () => {
    const parentPages = vi.fn(async () => ({ databases: [], pages: [] }));
    const resolve = vi.fn(async () => ({
      kind: "database" as const,
      id: "ds2",
      title: "pasted database",
      url: "https://notion.so/db2",
    }));
    const preview = vi.fn(async () => ({
      id: "ds2",
      title: "pasted database",
      title_property: "名称",
      missing_properties: [],
      conflicts: [],
      has_existing_rows: false,
    }));
    render(
      <Onboarding
        backend={{
          ...demoBackend,
          validateToken: async () => workspace,
          parentPages,
          resolveArchiveTarget: resolve,
          previewArchiveDatabase: preview,
        }}
        onDone={() => {}}
        onSkip={() => {}}
      />,
    );

    await openParentStep();
    expect(await screen.findByText("没有找到已共享的数据库。如果没有合适的页面，也可以直接粘贴数据库链接。")).toBeInTheDocument();

    const link = "https://www.notion.so/memo-0123456789abcdef0123456789abcdef";
    fireEvent.change(screen.getByLabelText("Notion 链接或 ID"), { target: { value: link } });
    fireEvent.click(screen.getByRole("button", { name: "使用此链接" }));
    await waitFor(() => expect(resolve).toHaveBeenCalledWith("secret", link));
    expect(await screen.findByRole("button", { name: /pasted database/ })).toBeInTheDocument();
    await waitFor(() => expect(preview).toHaveBeenCalledWith("secret", "ds2"));
  });
});
