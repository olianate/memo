import { act } from "@testing-library/react";
import { EditorView } from "@codemirror/view";

export function editorView(): EditorView {
  const content = document.querySelector<HTMLElement>(".cm-content");
  if (!content) throw new Error("editor content not found");
  const view = EditorView.findFromDOM(content);
  if (!view) throw new Error("editor view not found");
  return view;
}

/** Replace the editor document, as a user edit would. */
export function setEditorBody(body: string) {
  const view = editorView();
  act(() => {
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: body },
      selection: { anchor: body.length },
    });
  });
}

/** Insert text at the current selection, as typing would. */
export function typeInEditor(text: string) {
  const view = editorView();
  act(() => {
    const { from, to } = view.state.selection.main;
    view.dispatch({
      changes: { from, to, insert: text },
      selection: { anchor: from + text.length },
    });
  });
}
