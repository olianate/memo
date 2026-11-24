import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MarkdownEditor } from "./MarkdownEditor";
import { demoBackend } from "../demoBackend";
import type { Backend } from "../backend";
import { editorView, typeInEditor } from "../test/editor";

function Harness({
  backend,
  onTagsChange,
  initialBody = "hello",
}: {
  backend: Backend;
  onTagsChange: (tags: string[]) => void;
  initialBody?: string;
}) {
  const [body, setBody] = useState(initialBody);
  return (
    <MarkdownEditor
      backend={backend}
      body={body}
      tags={[]}
      attachments={[]}
      onBodyChange={setBody}
      onTagsChange={onTagsChange}
    />
  );
}

describe("MarkdownEditor tag autocomplete", () => {
  it("shows suggestions for a #token and commits the picked tag", async () => {
    const backend: Backend = {
      ...demoBackend,
      suggestTags: async () => [{ id: "t1", name: "work", normalized: "work", count: 3 }],
    };
    const onTagsChange = vi.fn();
    render(<Harness backend={backend} onTagsChange={onTagsChange} />);

    const view = editorView();
    view.dispatch({ selection: { anchor: view.state.doc.length } });
    typeInEditor(" #wo");

    const option = await screen.findByRole("option");
    expect(option).toHaveTextContent("#work");
    await userEvent.click(option);
    await waitFor(() => expect(onTagsChange).toHaveBeenCalledWith(["work"]));
    expect(editorView().state.doc.toString()).toBe("hello ");
  });

  it("commits a typed #tag on the following space", async () => {
    const backend: Backend = {
      ...demoBackend,
      suggestTags: async () => [],
    };
    const onTagsChange = vi.fn();
    render(<Harness backend={backend} onTagsChange={onTagsChange} initialBody="" />);

    const view = editorView();
    view.focus();
    await userEvent.type(view.contentDOM, "#工作 ");

    await waitFor(() => expect(onTagsChange).toHaveBeenCalledWith(["工作"]));
    expect(editorView().state.doc.toString()).toBe("");
  });

  it("shows the tag row above the editor body", () => {
    render(<Harness backend={demoBackend} onTagsChange={() => {}} />);
    const tagRow = document.querySelector(".tag-row");
    const shell = document.querySelector(".atomic-editor-shell");
    expect(tagRow).not.toBeNull();
    expect(shell).not.toBeNull();
    if (!tagRow || !shell) return;
    expect(tagRow.compareDocumentPosition(shell) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});
