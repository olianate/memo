import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { SearchPanel } from "./SearchPanel";
import { searchResult } from "../test/fixtures";

describe("SearchPanel", () => {
  it("highlights the matched snippet without injecting HTML", () => {
    render(<SearchPanel results={[searchResult()]} query="产品" loading={false} onOpen={() => {}} />);
    const mark = screen.getByText("产品");
    expect(mark.tagName).toBe("MARK");
    expect(screen.getByText(/找到/)).toBeInTheDocument();
  });

  it("shows the zero-result state", () => {
    render(<SearchPanel results={[]} query="缺失" loading={false} onOpen={() => {}} />);
    expect(screen.getByText("没有找到匹配的笔记")).toBeInTheDocument();
  });
});
