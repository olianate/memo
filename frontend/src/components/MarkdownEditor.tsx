import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AtomicCodeMirrorEditor, type AtomicCodeMirrorEditorHandle } from "@atomic-editor/editor";
import "@atomic-editor/editor/styles.css";
import { Prec, type Extension } from "@codemirror/state";
import { EditorView, ViewPlugin, keymap, placeholder as cmPlaceholder, type ViewUpdate } from "@codemirror/view";
import type { Attachment } from "../types";
import type { Backend } from "../backend";
import { blockText, blocksFromMarkdown, safeLinkUrl } from "../lib/markdown";
import { extractTagToken, normalizeTag, removeTagToken, type TagToken } from "../lib/tag";
import { countWords, humanBytes } from "../lib/format";
import { CloseIcon, FileIcon, ImageIcon, PaperclipIcon } from "./Icons";

interface EditorProps {
  backend: Backend;
  body: string;
  tags: string[];
  attachments: Attachment[];
  onBodyChange: (body: string) => void;
  onTagsChange: (tags: string[]) => void;
  onSubmit?: () => void;
  onEscape?: () => void;
  submitOnEnter?: boolean;
  autoFocus?: boolean;
  compact?: boolean;
  placeholder?: string;
  disabled?: boolean;
  onAddAttachment?: (files: File[]) => void;
  onRemoveAttachment?: (id: string) => void;
}

function sameTagToken(a: TagToken | null, b: TagToken | null): boolean {
  if (!a || !b) return a === b;
  return a.token === b.token && a.start === b.start && a.end === b.end;
}

function textChange(previous: string, next: string): { from: number; to: number; insert: string } {
  let from = 0;
  const max = Math.min(previous.length, next.length);
  while (from < max && previous[from] === next[from]) from++;
  let previousEnd = previous.length;
  let nextEnd = next.length;
  while (previousEnd > from && nextEnd > from && previous[previousEnd - 1] === next[nextEnd - 1]) {
    previousEnd--;
    nextEnd--;
  }
  return { from, to: previousEnd, insert: next.slice(from, nextEnd) };
}

/**
 * Markdown editor built on Atomic Editor (CodeMirror 6). The document is kept
 * as verbatim Markdown: the editor owns the text while typing, and external
 * updates replace the editor instance only when the body changes elsewhere.
 */
export function MarkdownEditor({
  backend,
  body,
  tags,
  attachments,
  onBodyChange,
  onTagsChange,
  onSubmit,
  onEscape,
  submitOnEnter = false,
  autoFocus = false,
  compact = false,
  placeholder = "记录此刻的想法… 支持 Markdown",
  disabled = false,
  onAddAttachment,
  onRemoveAttachment,
}: EditorProps) {
  const [tagToken, setTagToken] = useState<TagToken | null>(null);
  const [suggestions, setSuggestions] = useState<{ id: string; name: string; count: number }[]>([]);
  const [addingTag, setAddingTag] = useState(false);
  const [tagDraft, setTagDraft] = useState("");
  const [dragging, setDragging] = useState(false);
  const [editorEpoch, setEditorEpoch] = useState(0);
  const [imagePreview, setImagePreview] = useState<{ url: string; name: string } | null>(null);

  const handleRef = useRef<AtomicCodeMirrorEditorHandle>(null);
  const viewRef = useRef<EditorView | null>(null);
  const shellRef = useRef<HTMLDivElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const tagInput = useRef<HTMLInputElement>(null);

  const onBodyChangeRef = useRef(onBodyChange);
  const onTagsChangeRef = useRef(onTagsChange);
  const onSubmitRef = useRef(onSubmit);
  const onEscapeRef = useRef(onEscape);
  const onAddAttachmentRef = useRef(onAddAttachment);
  const submitOnEnterRef = useRef(submitOnEnter);
  const suggestionsRef = useRef(suggestions);
  const addTagRef = useRef<(raw: string) => void>(() => {});
  const commitTagRef = useRef<(view: EditorView, preferSuggestion: boolean) => boolean>(() => false);
  const commitPendingTagRef = useRef<(view: EditorView) => boolean>(() => false);
  const lastEmittedRef = useRef(body);

  useEffect(() => {
    onBodyChangeRef.current = onBodyChange;
    onTagsChangeRef.current = onTagsChange;
    onSubmitRef.current = onSubmit;
    onEscapeRef.current = onEscape;
    onAddAttachmentRef.current = onAddAttachment;
    submitOnEnterRef.current = submitOnEnter;
    suggestionsRef.current = suggestions;
  }, [onAddAttachment, onBodyChange, onEscape, onSubmit, onTagsChange, submitOnEnter, suggestions]);

  const referencedAttachments = useMemo(() => {
    const ids = new Set<string>();
    for (const match of body.matchAll(/attachment:\/\/([^\s)]+)/g)) ids.add(match[1]);
    return ids;
  }, [body]);
  const looseAttachments = attachments.filter((attachment) => !referencedAttachments.has(attachment.id));
  const wordCount = useMemo(() => {
    const plain = blocksFromMarkdown(body)
      .map(blockText)
      .join(" ");
    return countWords(plain);
  }, [body]);

  const addTag = useCallback(
    (raw: string) => {
      const name = raw.trim().replace(/^#+/, "").trim();
      if (!name) return;
      if (tags.some((tag) => normalizeTag(tag) === normalizeTag(name))) return;
      onTagsChangeRef.current([...tags, name]);
    },
    [tags],
  );
  addTagRef.current = addTag;

  const handleMarkdownChange = useCallback((markdown: string) => {
    if (markdown === lastEmittedRef.current) return;
    lastEmittedRef.current = markdown;
    onBodyChangeRef.current(markdown);
  }, []);

  // An external body update (memo switch, attachment upload) replaces the
  // editor document. Local typing updates lastEmittedRef first, so normal
  // edits never remount the view.
  useEffect(() => {
    if (body === lastEmittedRef.current) return;
    lastEmittedRef.current = body;
    setEditorEpoch((value) => value + 1);
  }, [body]);

  const commitTagResult = useCallback((view: EditorView, caret: number, next: string, name: string) => {
    const document = view.state.doc.toString();
    view.dispatch({
      changes: textChange(document, next),
      selection: { anchor: caret },
      scrollIntoView: true,
    });
    view.focus();
    addTagRef.current(name);
    setTagToken(null);
    setSuggestions([]);
  }, []);

  const commitTagToken = useCallback(
    (view: EditorView, token: TagToken, name: string) => {
      const document = view.state.doc.toString();
      commitTagResult(view, token.start, removeTagToken(document, token.start, token.end), name);
    },
    [commitTagResult],
  );

  const commitTag = useCallback(
    (name: string) => {
      const view = viewRef.current;
      if (!view) return;
      const document = view.state.doc.toString();
      const token = extractTagToken(document, view.state.selection.main.head);
      if (!token) return;
      commitTagToken(view, token, name);
    },
    [commitTagToken],
  );

  const commitTagInView = useCallback(
    (view: EditorView, preferSuggestion: boolean): boolean => {
      if (view.composing) return false;
      const document = view.state.doc.toString();
      const token = extractTagToken(document, view.state.selection.main.head);
      if (!token) return false;
      const suggestion = suggestionsRef.current[0];
      const name = preferSuggestion && suggestion ? suggestion.name : token.token;
      commitTagToken(view, token, name);
      return true;
    },
    [commitTagToken],
  );
  commitTagRef.current = commitTagInView;

  // Typing a space (or newline) right after `#tag` commits the tag even if a
  // competing keymap inserts the whitespace first. Runs after the edit lands,
  // so it also covers IME and paste input.
  const commitPendingTagAfterWhitespace = useCallback((view: EditorView): boolean => {
    if (view.composing) return false;
    const document = view.state.doc.toString();
    const head = view.state.selection.main.head;
    const whitespace = document[head - 1];
    if (whitespace !== " " && whitespace !== "\n") return false;
    const token = extractTagToken(document, head - 1);
    if (!token) return false;
    const next =
      whitespace === " "
        ? document.slice(0, token.start) + document.slice(head)
        : removeTagToken(document, token.start, token.end);
    commitTagResult(view, token.start, next, token.token);
    return true;
  }, [commitTagResult]);
  commitPendingTagRef.current = commitPendingTagAfterWhitespace;

  const extensions = useMemo<Extension[]>(() => {
    const syncTagToken = (view: EditorView) => {
      const next = extractTagToken(view.state.doc.toString(), view.state.selection.main.head);
      setTagToken((previous) => (sameTagToken(previous, next) ? previous : next));
    };
    const submit = (view: EditorView): boolean => {
      if (view.composing || !onSubmitRef.current) return false;
      onSubmitRef.current();
      return true;
    };
    return [
      EditorView.contentAttributes.of({ "aria-label": "Markdown 正文", spellcheck: "false" }),
      cmPlaceholder(placeholder),
      Prec.highest(
        keymap.of([
          {
            key: "Enter",
            run: (view) => (submitOnEnterRef.current ? submit(view) : false),
          },
          {
            key: "Shift-Enter",
            run: (view) => (submitOnEnterRef.current ? false : submit(view)),
          },
          {
            key: "Escape",
            run: () => {
              if (handleRef.current?.isSearchOpen()) return false;
              if (!onEscapeRef.current) return false;
              onEscapeRef.current();
              return true;
            },
          },
          { key: "Space", run: (view) => commitTagRef.current(view, false) },
          { key: "Tab", run: (view) => commitTagRef.current(view, true) },
        ]),
      ),
      ViewPlugin.fromClass(
        class {
          constructor(private view: EditorView) {
            viewRef.current = view;
            syncTagToken(view);
          }

          update(update: ViewUpdate) {
            this.view = update.view;
            viewRef.current = update.view;
            if (update.docChanged || update.selectionSet) syncTagToken(update.view);
            if (update.docChanged) {
              const view = update.view;
              queueMicrotask(() => {
                if (viewRef.current === view) commitPendingTagRef.current(view);
              });
            }
          }

          destroy() {
            if (viewRef.current === this.view) viewRef.current = null;
          }
        },
      ),
      EditorView.domEventHandlers({
        paste(event) {
          const files = filesFromClipboard(event);
          if (!files.length) return false;
          event.preventDefault();
          onAddAttachmentRef.current?.(files);
          return true;
        },
        drop(event) {
          const files = filesFromDrop(event);
          if (!files.length) return false;
          event.preventDefault();
          onAddAttachmentRef.current?.(files);
          return true;
        },
      }),
    ];
  }, [placeholder]);

  useEffect(() => {
    if (!tagToken) {
      setSuggestions([]);
      return;
    }
    let alive = true;
    backend
      .suggestTags(tagToken.token)
      .then((rows) => {
        if (alive) setSuggestions(rows);
      })
      .catch(() => {
        if (alive) setSuggestions([]);
      });
    return () => {
      alive = false;
    };
  }, [backend, tagToken?.token]);

  useEffect(() => {
    if (addingTag) tagInput.current?.focus();
  }, [addingTag]);

  useEffect(() => {
    if (!autoFocus) return;
    const frame = requestAnimationFrame(() => {
      handleRef.current?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, [autoFocus, editorEpoch]);

  // Atomic's image widget renders the raw `attachment://id` source. Replace
  // each rendered image with the thumbnail served by the backend.
  useEffect(() => {
    const shell = shellRef.current;
    if (!shell) return;
    let alive = true;
    const fixed = new WeakSet<HTMLImageElement>();
    const fix = () => {
      for (const image of shell.querySelectorAll<HTMLImageElement>('img[src^="attachment://"]')) {
        if (fixed.has(image)) continue;
        fixed.add(image);
        const id = image.getAttribute("src")?.slice("attachment://".length) ?? "";
        if (!id) continue;
        image.dataset.attachmentId = id;
        image.classList.add("attachment-inline-image");
        backend
          .attachmentThumb(id)
          .then((url) => {
            if (alive) image.src = url;
          })
          .catch(() => {
            if (alive) image.dataset.attachmentError = "true";
          });
      }
    };
    fix();
    const observer = new MutationObserver(fix);
    observer.observe(shell, { childList: true, subtree: true });
    return () => {
      alive = false;
      observer.disconnect();
    };
  }, [backend, editorEpoch]);

  // Inline attachment images open full-size when clicked.
  useEffect(() => {
    const shell = shellRef.current;
    if (!shell) return;
    const onClick = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null;
      const image = target?.closest?.("img[data-attachment-id]") as HTMLImageElement | null;
      if (!image) return;
      const id = image.dataset.attachmentId;
      if (!id) return;
      event.preventDefault();
      event.stopPropagation();
      const name = image.alt || "图片";
      backend
        .attachmentData(id)
        .then((url) => setImagePreview({ url, name }))
        .catch(() => {});
    };
    shell.addEventListener("click", onClick, true);
    return () => shell.removeEventListener("click", onClick, true);
  }, [backend, editorEpoch]);

  const openLink = (url: string) => {
    if (url.startsWith("attachment://")) {
      void backend.openAttachment(url.slice("attachment://".length)).catch(() => {});
      return;
    }
    const safe = safeLinkUrl(url);
    if (safe) void backend.openURL(safe).catch(() => {});
  };

  const handleFiles = (files: File[]) => {
    if (!onAddAttachmentRef.current || files.length === 0) return;
    onAddAttachmentRef.current(files);
  };

  const tagRow = (
    <div className="tag-row tag-row-editor">
      {tags.map((tag) => (
        <span className="tag-chip" key={tag}>
          #{tag}
          <button type="button" aria-label={`移除标签 ${tag}`} onClick={() => onTagsChange(tags.filter((t) => t !== tag))}>
            ×
          </button>
        </span>
      ))}
      {addingTag ? (
        <input
          ref={tagInput}
          className="tag-input"
          value={tagDraft}
          placeholder="标签名"
          onChange={(event) => setTagDraft(event.target.value)}
          onBlur={() => {
            addTag(tagDraft);
            setTagDraft("");
            setAddingTag(false);
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              addTag(tagDraft);
              setTagDraft("");
              setAddingTag(false);
            }
            if (event.key === "Escape") {
              setTagDraft("");
              setAddingTag(false);
            }
          }}
        />
      ) : (
        <button type="button" className="tag-add" onClick={() => setAddingTag(true)}>
          + 标签
        </button>
      )}
      <span className="tag-hint">#标签 直接转换</span>
    </div>
  );

  return (
    <div
      className={`editor ${compact ? "editor-compact" : ""} ${dragging ? "is-dragging" : ""}`}
      onDragOver={(event) => {
        if (onAddAttachment && event.dataTransfer?.types.includes("Files")) {
          event.preventDefault();
          setDragging(true);
        }
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={() => setDragging(false)}
    >
      <div className="editor-toolbar" role="toolbar" aria-label="Markdown 工具栏">
        <span className="editor-mode-label">Markdown</span>
        <div className="toolbar-group toolbar-spacer">
          {onAddAttachment && (
            <>
              <button
                type="button"
                className="tool-btn"
                title="添加图片或文件"
                aria-label="添加图片或文件"
                onClick={() => fileInput.current?.click()}
              >
                <PaperclipIcon size={16} />
              </button>
              <input
                ref={fileInput}
                type="file"
                multiple
                className="file-input"
                onChange={(event) => {
                  handleFiles(Array.from(event.target.files ?? []));
                  event.target.value = "";
                }}
              />
            </>
          )}
        </div>
      </div>

      {tagRow}

      <div className="atomic-editor-shell" ref={shellRef}>
        <AtomicCodeMirrorEditor
          key={editorEpoch}
          documentId={`memo-${editorEpoch}`}
          markdownSource={body}
          onMarkdownChange={handleMarkdownChange}
          onLinkClick={openLink}
          editorHandleRef={handleRef}
          readOnly={disabled}
          blurEditorOnMount={!autoFocus}
          extensions={extensions}
        />
      </div>

      <div className="editor-word-count" aria-label={`字数 ${wordCount}`}>
        字数 {wordCount}
      </div>

      {imagePreview && (
        <div className="lightbox" role="dialog" aria-modal="true" onClick={() => setImagePreview(null)}>
          <img src={imagePreview.url} alt={imagePreview.name} />
        </div>
      )}

      {looseAttachments.length > 0 && (
        <div className="editor-attachments">
          {looseAttachments.map((attachment) => (
            <AttachmentBlock
              key={attachment.id}
              backend={backend}
              attachment={attachment}
              onRemove={onRemoveAttachment ? () => onRemoveAttachment(attachment.id) : undefined}
            />
          ))}
        </div>
      )}

      {tagToken && suggestions.length > 0 && (
        <div className="tag-popover" role="listbox" aria-label="标签建议">
          {suggestions.map((s) => (
            <button
              type="button"
              key={s.id}
              role="option"
              aria-selected="false"
              onMouseDown={(event) => {
                event.preventDefault();
                commitTag(s.name);
              }}
            >
              <span>#{s.name}</span>
              <span className="tag-count">{s.count}</span>
            </button>
          ))}
        </div>
      )}

      {dragging && <div className="drop-hint">松开以添加图片或文件</div>}
    </div>
  );
}

function filesFromClipboard(event: ClipboardEvent): File[] {
  const items = event.clipboardData?.items;
  if (!items) return [];
  const files: File[] = [];
  for (const item of Array.from(items)) {
    if (item.kind !== "file") continue;
    const file = item.getAsFile();
    if (file) files.push(file);
  }
  return files;
}

function filesFromDrop(event: DragEvent): File[] {
  return Array.from(event.dataTransfer?.files ?? []);
}

export function AttachmentBlock({
  backend,
  attachment,
  onRemove,
}: {
  backend: Backend;
  attachment: Attachment;
  onRemove?: () => void;
}) {
  const [thumb, setThumb] = useState<string | null>(null);
  const [full, setFull] = useState<string | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (attachment.kind !== "image" || !attachment.present) return;
    let alive = true;
    backend
      .attachmentThumb(attachment.id)
      .then((url) => {
        if (alive) setThumb(url);
      })
      .catch((e) => {
        if (alive) setError(String(e));
      });
    return () => {
      alive = false;
    };
  }, [attachment.id, attachment.kind, attachment.present, backend]);

  const open = async () => {
    if (attachment.kind === "image") {
      try {
        setFull(await backend.attachmentData(attachment.id));
      } catch (e) {
        setError(String(e));
      }
      return;
    }
    try {
      await backend.openAttachment(attachment.id);
    } catch (e) {
      setError(String(e));
    }
  };

  if (attachment.kind === "image") {
    return (
      <div className="attachment-image">
        {thumb ? (
          <button type="button" className="image-button" onClick={() => void open()} aria-label={`查看图片 ${attachment.name}`}>
            <img src={thumb} alt={attachment.name} />
          </button>
        ) : (
          <div className="image-placeholder">
            <ImageIcon size={20} />
            <span>{attachment.present ? "加载中…" : "图片已清理，打开时从 Notion 恢复"}</span>
          </div>
        )}
        {onRemove && (
          <button type="button" className="attachment-remove" onClick={onRemove} aria-label="移除图片">
            <CloseIcon size={13} />
          </button>
        )}
        {error && <div className="inline-error">{error}</div>}
        {full && (
          <div className="lightbox" role="dialog" aria-modal="true" onClick={() => setFull(null)}>
            <img src={full} alt={attachment.name} />
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="attachment-file">
      <span className="file-icon">
        <FileIcon size={18} />
      </span>
      <span className="file-name">{attachment.name}</span>
      <span className="file-size">{humanBytes(attachment.bytes)}</span>
      <button type="button" className="link-btn" onClick={() => void open()}>
        打开
      </button>
      {onRemove && (
        <button type="button" className="attachment-remove" onClick={onRemove} aria-label="移除文件">
          <CloseIcon size={13} />
        </button>
      )}
      {error && <div className="inline-error">{error}</div>}
    </div>
  );
}
