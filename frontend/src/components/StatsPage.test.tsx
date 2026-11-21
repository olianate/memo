import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { StatsPage } from "./StatsPage";
import { demoBackend } from "../demoBackend";
import { dashboard } from "../test/fixtures";
import type { Backend } from "../backend";

function backendWith(data: ReturnType<typeof dashboard>): Backend {
  return { ...demoBackend, dashboard: async () => data };
}

describe("StatsPage", () => {
  it("renders the metadata-driven totals and charts", async () => {
    render(<StatsPage backend={backendWith(dashboard())} onError={() => {}} />);
    expect(await screen.findByText("记录统计")).toBeInTheDocument();
    expect(screen.getByText("120")).toBeInTheDocument();
    expect(screen.getByText("每日记录")).toBeInTheDocument();
    expect(screen.getByText("内容构成")).toBeInTheDocument();
    expect(screen.getByText(/共 9 块/)).toBeInTheDocument();
    expect(screen.getByText("标签共现")).toBeInTheDocument();
    expect(screen.getByText("同步与存储健康")).toBeInTheDocument();
  });

  it("shows a zero state for an empty month", async () => {
    const empty = dashboard({
      totals: { notes: 0, words: 0, tags: 0, attachments: 0, active_days: 0 },
      top_tags: [],
      co_occurrence: [],
      daily: [],
    });
    render(<StatsPage backend={backendWith(empty)} onError={() => {}} />);
    expect(await screen.findByText(/这个月还没有记录/)).toBeInTheDocument();
    expect(screen.getByText("还没有足够的标签形成共现。")).toBeInTheDocument();
  });
});
