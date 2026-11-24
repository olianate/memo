import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Timeline } from "../components/Timeline";
import { demoBackend } from "../demoBackend";
import { memo } from "../test/fixtures";

describe("Timeline", () => {
  it("groups memos under day headers and shows tags", () => {
    const memos = [
      memo({
        id: "m1",
        day: "2026-11-21",
        tags: [{ id: "t1", name: "产品", normalized: "产品", count: 1 }],
        body: "第一条",
      }),
      memo({
        id: "m2",
        day: "2026-11-20",
        body: "第二条",
      }),
    ];
    render(
      <Timeline
        memos={memos}
        backend={demoBackend}
        loading={false}
        onOpen={() => {}}
        onChanged={() => {}}
        onError={() => {}}
      />,
    );
    expect(screen.getByText("第一条")).toBeInTheDocument();
    expect(screen.getByText("第二条")).toBeInTheDocument();
    expect(screen.getByText("#产品")).toBeInTheDocument();
    expect(screen.getAllByText(/\d+ 条/)).toHaveLength(2);
  });

  it("opens a memo when the card is clicked", async () => {
    const open = vi.fn();
    const item = memo({ id: "m1", day: "2026-11-21" });
    render(
      <Timeline memos={[item]} backend={demoBackend} loading={false} onOpen={open} onChanged={() => {}} onError={() => {}} />,
    );
    await userEvent.click(screen.getByText("示例正文"));
    expect(open).toHaveBeenCalledWith(expect.objectContaining({ id: "m1" }));
  });

  it("shows the evicted placeholder instead of a body", () => {
    const item = memo({ id: "m1", day: "2026-11-21", local_state: "evicted", body: "" });
    render(
      <Timeline memos={[item]} backend={demoBackend} loading={false} onOpen={() => {}} onChanged={() => {}} onError={() => {}} />,
    );
    expect(screen.getByText(/正文与附件已清理/)).toBeInTheDocument();
    expect(screen.getByText("打开并恢复")).toBeInTheDocument();
  });

  it("renders the empty state", () => {
    render(
      <Timeline memos={[]} backend={demoBackend} loading={false} onOpen={() => {}} onChanged={() => {}} onError={() => {}} />,
    );
    expect(screen.getByText("这里还很安静")).toBeInTheDocument();
  });
});
