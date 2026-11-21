import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MarkdownEditor } from "./MarkdownEditor";
import { demoBackend } from "../demoBackend";
import type { Backend } from "../backend";

describe("MarkdownEditor tag autocomplete", () => {
  it("shows suggestions for a #token and commits the picked tag", async () => {
    const backend: Backend = {
      ...demoBackend,
      suggestTags: async () => [{ id: "t1", name: "work", normalized: "work", count: 3 }],
    };
    const onTagsChange = vi.fn();
    render(
      <MarkdownEditor
        backend={backend}
        blocks={[{ id: "b1", type: "paragraph", spans: [{ text: "hello" }] }]}
        tags={[]}
        attachments={[]}
        onChange={() => {}}
        onTagsChange={onTagsChange}
      />,
    );
    const input = document.querySelector<HTMLElement>(".ed-input");
    expect(input).not.toBeNull();
    if (input) {
      input.textContent = "hello #wo";
      fireEvent.input(input);
    }
    const option = await screen.findByRole("option");
    expect(option).toHaveTextContent("#work");
    fireEvent.mouseDown(option);
    await waitFor(() => expect(onTagsChange).toHaveBeenCalledWith(["work"]));
  });
});
