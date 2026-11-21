import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Composer } from "./Composer";
import { demoBackend } from "../demoBackend";

describe("Composer", () => {
  it("saves typed text through the backend seam", async () => {
    const create = vi.spyOn(demoBackend, "create").mockResolvedValue({
      id: "new",
      created_at: "2026-11-21T09:00:00Z",
      updated_at: "2026-11-21T09:00:00Z",
      revision: 1,
      snippet: "hello",
      deleted: false,
      keep_local: false,
      local_state: "present",
      blocks: [],
      tags: [],
      attachments: [],
      day: "2026-11-21",
    });
    render(<Composer backend={demoBackend} onSaved={() => {}} onError={() => {}} />);
    const editor = document.querySelector<HTMLElement>(".ed-input");
    expect(editor).not.toBeNull();
    if (editor) {
      editor.textContent = "hello from composer";
      fireEvent.input(editor);
    }
    fireEvent.click(screen.getByRole("button", { name: "保存笔记" }));
    await waitFor(() => expect(create).toHaveBeenCalled());
    const blocks = create.mock.calls[0][0];
    expect(blocks[0].spans?.[0]?.text).toBe("hello from composer");
    create.mockRestore();
  });

  it("does not save an empty composer", () => {
    const create = vi.spyOn(demoBackend, "create").mockResolvedValue({} as never);
    render(<Composer backend={demoBackend} onSaved={() => {}} onError={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "保存笔记" }));
    expect(create).not.toHaveBeenCalled();
    create.mockRestore();
  });
});
