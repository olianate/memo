import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MarkdownEditor } from "./MarkdownEditor";
import { demoBackend } from "../demoBackend";
import { editorView, typeInEditor } from "../test/editor";

function Harness({
  initial = "",
  onBodyChange,
  onAddAttachment,
  onSubmit,
  submitOnEnter,
}: {
  initial?: string;
  onBodyChange?: (body: string) => void;
  onAddAttachment?: (files: File[]) => void;
  onSubmit?: () => void;
  submitOnEnter?: boolean;
}) {
  const [body, setBody] = useState(initial);
  return (
    <>
      <MarkdownEditor
        backend={demoBackend}
        body={body}
        tags={[]}
        attachments={[]}
        onBodyChange={(next) => {
          setBody(next);
          onBodyChange?.(next);
        }}
        onTagsChange={() => {}}
        onAddAttachment={onAddAttachment}
        onSubmit={onSubmit}
        submitOnEnter={submitOnEnter}
      />
      <pre data-testid="body">{body}</pre>
    </>
  );
}

function text(selector: string): string {
  return document.querySelector(selector)?.textContent ?? "";
}

describe("MarkdownEditor live preview", () => {
  it("renders supported Markdown and keeps unsupported syntax literal", () => {
    render(
      <Harness
        initial={"# 标题\n\n- [x] 完成\n- 项目\n\n> 引用\n\n```go\nfmt.Println(1)\n```\n\n---\n\n| a | b |"}
      />,
    );
    expect(text(".cm-atomic-h1")).toBe("标题");
    expect(screen.getByRole("checkbox")).toBeChecked();
    expect(screen.getByText("项目")).toBeInTheDocument();
    expect(screen.getByText("引用")).toBeInTheDocument();
    expect(screen.getByText("fmt.Println(1)")).toBeInTheDocument();
    expect(document.querySelector(".cm-atomic-hr")).not.toBeNull();
    expect(screen.getByText("| a | b |")).toBeInTheDocument();
  });

  it("shows a word count below the editor", () => {
    render(<Harness initial={"# 标题\n\nhello 世界"} />);
    expect(screen.getByLabelText("字数 5")).toHaveTextContent("字数 5");
  });

  it("edits one range and preserves every other byte", () => {
    const changed = vi.fn();
    render(<Harness initial={"# 标题\n\n第一行  \n\n第二行"} onBodyChange={changed} />);

    const view = editorView();
    act(() => {
      view.dispatch({
        changes: { from: 11, insert: "!" },
        selection: { anchor: 12 },
      });
    });

    expect(changed.mock.calls[changed.mock.calls.length - 1]?.[0]).toBe("# 标题\n\n第一行  !\n\n第二行");
  });

  it("renders inline styles and keeps literal syntax", () => {
    render(<Harness initial={"**粗** *斜* ~~删~~ `码` [链接](https://example.com) <div>"} />);
    expect(text(".cm-atomic-strong")).toBe("粗");
    expect(text(".cm-atomic-em")).toBe("斜");
    expect(text(".cm-atomic-strike")).toBe("删");
    expect(text(".cm-atomic-inline-code")).toBe("码");
    expect(text(".cm-atomic-link")).toBe("链接");
    expect(screen.getAllByText(/<div>/).length).toBeGreaterThan(0);
  });

  it("forwards pasted files to the attachment hook", () => {
    const onAddAttachment = vi.fn();
    render(<Harness initial={"hello"} onAddAttachment={onAddAttachment} />);
    const file = new File(["x"], "a.png", { type: "image/png" });
    const content = document.querySelector(".cm-content");
    expect(content).not.toBeNull();
    if (!content) return;
    fireEvent.paste(content, {
      clipboardData: {
        items: [{ kind: "file", getAsFile: () => file }],
        files: [file],
      },
    });
    expect(onAddAttachment).toHaveBeenCalledWith([file]);
  });

  it("undoes and redoes edits without rewriting the rest of the body", async () => {
    render(<Harness initial={"a\nrest"} />);
    const view = editorView();
    view.focus();
    view.dispatch({ selection: { anchor: 1 } });
    typeInEditor("b");
    expect(screen.getByTestId("body").textContent).toBe("ab\nrest");

    await userEvent.keyboard("{Control>}z{/Control}");
    expect(screen.getByTestId("body").textContent).toBe("a\nrest");

    await userEvent.keyboard("{Control>}y{/Control}");
    expect(screen.getByTestId("body").textContent).toBe("ab\nrest");
  });

  it("moves the selection with the arrow keys without losing bytes", async () => {
    render(<Harness initial={"第一行\n第二行"} />);
    const view = editorView();
    view.focus();
    view.dispatch({ selection: { anchor: 0 } });
    await userEvent.keyboard("{ArrowDown}");
    expect(view.state.selection.main.head).toBeGreaterThan(0);
    expect(screen.getByTestId("body").textContent).toBe("第一行\n第二行");
  });

  it("offers an editable surface for an empty body", () => {
    render(<Harness initial="" />);
    expect(document.querySelector(".cm-content")).not.toBeNull();
    expect(document.querySelector(".cm-placeholder")).toHaveTextContent("记录此刻的想法");
  });

  it("uses Enter to save and Shift+Enter to insert a newline", () => {
    const onSubmit = vi.fn();
    render(<Harness initial="abc" onSubmit={onSubmit} submitOnEnter />);
    const view = editorView();
    view.focus();
    view.dispatch({ selection: { anchor: 3 } });

    fireEvent.keyDown(view.contentDOM, { key: "Enter", code: "Enter", keyCode: 13 });
    expect(onSubmit).toHaveBeenCalledTimes(1);

    fireEvent.keyDown(view.contentDOM, { key: "Enter", code: "Enter", keyCode: 13, shiftKey: true });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("body").textContent).toBe("abc\n");
  });

  it("uses Enter to insert a newline and Shift+Enter to save", () => {
    const onSubmit = vi.fn();
    render(<Harness initial="abc" onSubmit={onSubmit} submitOnEnter={false} />);
    const view = editorView();
    view.focus();
    view.dispatch({ selection: { anchor: 3 } });

    fireEvent.keyDown(view.contentDOM, { key: "Enter", code: "Enter", keyCode: 13 });
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByTestId("body").textContent).toBe("abc\n");

    fireEvent.keyDown(view.contentDOM, { key: "Enter", code: "Enter", keyCode: 13, shiftKey: true });
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });
});
