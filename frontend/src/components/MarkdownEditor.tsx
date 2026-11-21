import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Attachment, Block } from "../types";
import type { Backend } from "../backend";
import { inlineMarkdownToSpans, removeSpanRange, spansFromDOM, spansToHtml, blockText } from "../lib/markdown";
import { extractTagToken, matchInputRule, removeTagToken, type TagToken } from "../lib/tag";
import { humanBytes } from "../lib/format";
import { CloseIcon, FileIcon, ImageIcon, PaperclipIcon, PlusIcon } from "./Icons";

let idCounter = 0;
const nextId = () => `blk-${Date.now().toString(36)}-${(idCounter++).toString(36)}`;

function caretOffset(el: HTMLElement): number {
  const selection = window.getSelection();
  if (!selection || selection.rangeCount === 0) return (el.textContent ?? "").length;
  const range = selection.getRangeAt(0);
  if (!el.contains(range.startContainer)) return (el.textContent ?? "").length;
  const before = document.createRange();
  before.selectNodeContents(el);
  before.setEnd(range.startContainer, range.startOffset);
  return before.toString().length;
}

function setCaret(el: HTMLElement, offset: number, toEnd = false) {
  const range = document.createRange();
  const selection = window.getSelection();
  if (!selection) return;
  const walk = (node: Node): { node: Node; offset: number } | null => {
    let remaining = offset;
    const visit = (n: Node): { node: Node; offset: number } | null => {
      if (n.nodeType === Node.TEXT_NODE) {
        const len = n.textContent?.length ?? 0;
        if (remaining <= len) return { node: n, offset: remaining };
        remaining -= len;
        return null;
      }
      for (const child of Array.from(n.childNodes)) {
        const found = visit(child);
        if (found) return found;
      }
      return null;
    };
    return visit(node);
  };
  const position = toEnd ? null : walk(el);
  if (position) {
    range.setStart(position.node, position.offset);
    range.collapse(true);
  } else {
    range.selectNodeContents(el);
    range.collapse(false);
  }
  selection.removeAllRanges();
  selection.addRange(range);
}

interface EditorProps {
  backend: Backend;
  blocks: Block[];
  tags: string[];
  attachments: Attachment[];
  onChange: (blocks: Block[]) => void;
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

export function MarkdownEditor({
  backend,
  blocks,
  tags,
  attachments,
  onChange,
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
  const [mode, setMode] = useState<"live" | "source">("live");
  const [source, setSource] = useState("");
  const [switching, setSwitching] = useState(false);
  const [activeId, setActiveId] = useState<string | null>(blocks[0]?.id ?? null);
  const [focusId, setFocusId] = useState<string | null>(null);
  const [tagToken, setTagToken] = useState<(TagToken & { blockId: string }) | null>(null);
  const [suggestions, setSuggestions] = useState<{ id: string; name: string; count: number }[]>([]);
  const [addingTag, setAddingTag] = useState(false);
  const [tagDraft, setTagDraft] = useState("");
  const [dragging, setDragging] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const editorRoot = useRef<HTMLDivElement>(null);
  const tagInput = useRef<HTMLInputElement>(null);

  const effectiveBlocks = useMemo(
    () => (blocks.length ? blocks : [{ id: "empty", type: "paragraph" as const, spans: [] }]),
    [blocks],
  );
  const activeBlock = effectiveBlocks.find((b) => b.id === activeId) ?? effectiveBlocks[0];

  useEffect(() => {
    if (autoFocus && editorRoot.current) {
      const first = editorRoot.current.querySelector<HTMLElement>(".ed-input");
      first?.focus();
    }
  }, [autoFocus]);

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
  }, [backend, tagToken?.token, tagToken?.blockId]);

  useEffect(() => {
    if (addingTag) tagInput.current?.focus();
  }, [addingTag]);

  // Source mode edits must reach the parent before a save. Parse on a short
  // debounce so the canonical block AST is current even if the user clicks the
  // send button without switching back to live mode.
  const sourceSeq = useRef(0);
  useEffect(() => {
    if (mode !== "source") return;
    const seq = ++sourceSeq.current;
    const timer = window.setTimeout(() => {
      backend
        .parseMarkdown(source)
        .then((parsed) => {
          if (seq !== sourceSeq.current) return;
          onChange(parsed.length ? parsed : [{ id: nextId(), type: "paragraph", spans: [] }]);
        })
        .catch(() => {});
    }, 80);
    return () => window.clearTimeout(timer);
  }, [backend, mode, onChange, source]);

  const updateBlock = useCallback(
    (id: string, patch: Partial<Block>) => {
      const next = effectiveBlocks.map((b) => (b.id === id ? { ...b, ...patch } : b));
      onChange(next);
    },
    [effectiveBlocks, onChange],
  );

  const addTag = useCallback(
    (raw: string) => {
      const name = raw.trim().replace(/^#+/, "").trim();
      if (!name) return;
      if (tags.some((tag) => tag.toLowerCase() === name.toLowerCase())) return;
      onTagsChange([...tags, name]);
    },
    [onTagsChange, tags],
  );

  const commitTag = useCallback(
    (name: string) => {
      if (!tagToken) return;
      const token = tagToken;
      const block = effectiveBlocks.find((b) => b.id === token.blockId);
      if (!block) return;
      const text = blockText(block);
      const nextText = removeTagToken(text, token.start, token.end);
      const nextSpans = removeSpanRange(block.spans ?? [], token.start, token.end);
      updateBlock(token.blockId, { spans: nextSpans });
      const exists = tags.some((t) => t.toLowerCase() === name.toLowerCase());
      if (!exists) onTagsChange([...tags, name]);
      setTagToken(null);
      setSuggestions([]);
      requestAnimationFrame(() => {
        const el = editorRoot.current?.querySelector<HTMLElement>(`[data-block="${token.blockId}"] .ed-input`);
        if (el) {
          // The block stays focused while the chip is committed; update the
          // contentEditable DOM directly so the token disappears immediately.
          el.innerHTML = spansToHtml(nextSpans);
          el.focus();
          setCaret(el, Math.min(token.start, nextText.length));
        }
      });
    },
    [effectiveBlocks, onTagsChange, tags, tagToken, updateBlock],
  );

  const splitBlock = useCallback(
    (index: number, before: string, after: string) => {
      const current = effectiveBlocks[index];
      if (!current) return;
      const keepType = ["bullet", "ordered", "todo"].includes(current.type);
      if (keepType && !before && !after) {
        updateBlock(current.id, { type: "paragraph", checked: false });
        return;
      }
      const updatedCurrent = { ...current, spans: inlineMarkdownToSpans(before) };
      const newBlock: Block = {
        id: nextId(),
        type: keepType ? current.type : "paragraph",
        checked: false,
        level: undefined,
        spans: inlineMarkdownToSpans(after),
      };
      const next = [...effectiveBlocks.slice(0, index), updatedCurrent, newBlock, ...effectiveBlocks.slice(index + 1)];
      onChange(next);
      setFocusId(newBlock.id);
    },
    [effectiveBlocks, onChange, updateBlock],
  );

  const onBlockKeyDown = (event: React.KeyboardEvent<HTMLElement>, block: Block, index: number) => {
    if (disabled) return;
    const native = event.nativeEvent as KeyboardEvent;
    if (native.isComposing || native.keyCode === 229) return;
    const el = event.currentTarget as HTMLElement;
    const text = el.textContent ?? "";
    const caret = el.textContent === "" && el.innerHTML.includes("<br>") ? 0 : caretOffset(el);

    if (event.key === "Escape") {
      if (onEscape) {
        event.preventDefault();
        onEscape();
      }
      return;
    }

    if (event.key === "Enter") {
      if (submitOnEnter && !event.shiftKey) {
        event.preventDefault();
        onSubmit?.();
        return;
      }
      event.preventDefault();
      const before = text.slice(0, caret);
      const after = text.slice(caret);
      splitBlock(index, before, after);
      return;
    }

    if (event.key === " " || event.key === "Tab") {
      const token = extractTagToken(text, caret);
      if (token) {
        event.preventDefault();
        if (event.key === "Tab" && suggestions.length > 0) commitTag(suggestions[0].name);
        else commitTag(token.token);
        return;
      }
      if (event.key === " ") {
        const rule = matchInputRule(text.slice(0, caret));
        if (rule && caret === text.length) {
          event.preventDefault();
          const patch: Partial<Block> = { type: rule.type as Block["type"], spans: [] };
          if (rule.level) patch.level = rule.level;
          updateBlock(block.id, patch);
          return;
        }
      }
    }

    if (event.key === "Backspace" && caret === 0 && window.getSelection()?.isCollapsed) {
      if (block.type !== "paragraph" && block.type !== "heading") {
        event.preventDefault();
        updateBlock(block.id, { type: "paragraph", level: undefined, checked: false });
        return;
      }
      if (block.type === "heading") {
        event.preventDefault();
        updateBlock(block.id, { type: "paragraph", level: undefined });
        return;
      }
      if (index > 0) {
        event.preventDefault();
        const prev = effectiveBlocks[index - 1];
        const merged = `${blockText(prev)}${text}`;
        const next = [...effectiveBlocks];
        next[index - 1] = { ...prev, spans: [{ text: merged }] };
        next.splice(index, 1);
        onChange(next);
        setFocusId(prev.id);
        requestAnimationFrame(() => {
          const target = editorRoot.current?.querySelector<HTMLElement>(`[data-block="${prev.id}"] .ed-input`);
          if (target) {
            target.focus();
            setCaret(target, blockText(prev).length);
          }
        });
      }
    }
  };

  const onBlockInput = (event: React.FormEvent<HTMLDivElement>, block: Block) => {
    const el = event.currentTarget;
    const spans = spansFromDOM(el);
    updateBlock(block.id, { spans });
    const text = el.textContent ?? "";
    const caret = caretOffset(el);
    const token = extractTagToken(text, caret);
    if (token) setTagToken({ ...token, blockId: block.id });
    else if (tagToken?.blockId === block.id) setTagToken(null);
  };

  const onBlockBlur = (event: React.FocusEvent<HTMLDivElement>, block: Block) => {
    const el = event.currentTarget;
    const text = el.textContent ?? "";
    // Preserve marks created by the toolbar or rich paste. Only parse Markdown
    // syntax when the DOM itself has no formatting.
    const domSpans = spansFromDOM(el);
    const hasMarks = domSpans.some((s) => s.bold || s.italic || s.strike || s.code || s.link);
    const parsed = hasMarks ? domSpans : inlineMarkdownToSpans(text);
    updateBlock(block.id, { spans: parsed });
    // The block is no longer focused, so render the parsed marks immediately
    // instead of waiting for a state change that may be a no-op.
    el.innerHTML = spansToHtml(parsed);
  };

  const applyType = (type: Block["type"], level?: number) => {
    if (!activeBlock) return;
    updateBlock(activeBlock.id, { type, level, checked: false });
    setFocusId(activeBlock.id);
  };

  const addBlock = (afterIndex = effectiveBlocks.length - 1, type: Block["type"] = "paragraph") => {
    const block: Block = { id: nextId(), type, spans: [] };
    const next = [...effectiveBlocks.slice(0, afterIndex + 1), block, ...effectiveBlocks.slice(afterIndex + 1)];
    onChange(next);
    setFocusId(block.id);
    setActiveId(block.id);
  };

  const removeBlock = (id: string) => {
    const index = effectiveBlocks.findIndex((b) => b.id === id);
    const next = effectiveBlocks.filter((b) => b.id !== id);
    onChange(next.length ? next : [{ id: nextId(), type: "paragraph", spans: [] }]);
    const target = next[Math.max(0, index - 1)];
    if (target) setFocusId(target.id);
  };

  const executeInline = (command: "bold" | "italic" | "strikeThrough" | "code") => {
    if (!activeBlock) return;
    const el = editorRoot.current?.querySelector<HTMLElement>(`[data-block="${activeBlock.id}"] .ed-input`);
    if (!el) return;
    el.focus();
    if (command === "code") {
      const selection = window.getSelection();
      const selected = selection?.toString() ?? "";
      if (selected) document.execCommand("insertHTML", false, `<code>${selected.replaceAll("<", "&lt;").replaceAll(">", "&gt;")}</code>`);
    } else {
      document.execCommand(command, false);
    }
    updateBlock(activeBlock.id, { spans: spansFromDOM(el) });
  };

  const switchToSource = async () => {
    setSwitching(true);
    try {
      setSource(await backend.serializeMarkdown(effectiveBlocks));
      setMode("source");
      setTagToken(null);
    } finally {
      setSwitching(false);
    }
  };

  const switchToLive = async () => {
    setSwitching(true);
    try {
      const parsed = await backend.parseMarkdown(source);
      onChange(parsed.length ? parsed : [{ id: nextId(), type: "paragraph", spans: [] }]);
      setMode("live");
    } finally {
      setSwitching(false);
    }
  };

  const handleFiles = (files: File[]) => {
    if (!onAddAttachment || files.length === 0) return;
    onAddAttachment(files);
  };

  const onPaste = (event: React.ClipboardEvent) => {
    const items = event.clipboardData?.items;
    if (!items || !onAddAttachment) return;
    const files: File[] = [];
    for (const item of Array.from(items)) {
      if (item.kind === "file") {
        const file = item.getAsFile();
        if (file) files.push(file);
      }
    }
    if (files.length) {
      event.preventDefault();
      handleFiles(files);
    }
  };

  const onDrop = (event: React.DragEvent) => {
    if (!onAddAttachment) return;
    const files = Array.from(event.dataTransfer?.files ?? []);
    if (files.length) {
      event.preventDefault();
      setDragging(false);
      handleFiles(files);
    }
  };

  const attachmentFor = (block: Block) => attachments.find((a) => a.id === block.attachment_id);

  return (
    <div
      className={`editor ${compact ? "editor-compact" : ""} ${dragging ? "is-dragging" : ""}`}
      onDragOver={(e) => {
        if (onAddAttachment && e.dataTransfer?.types.includes("Files")) {
          e.preventDefault();
          setDragging(true);
        }
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={onDrop}
      onPaste={onPaste}
    >
      <div className="editor-toolbar" role="toolbar" aria-label="格式工具栏">
        <div className="toolbar-group">
          <button type="button" className="tool-btn toolbar-text" title="标题 1" onMouseDown={(e) => e.preventDefault()} onClick={() => applyType("heading", 1)}>
            H1
          </button>
          <button type="button" className="tool-btn toolbar-text" title="标题 2" onMouseDown={(e) => e.preventDefault()} onClick={() => applyType("heading", 2)}>
            H2
          </button>
          <button type="button" className="tool-btn toolbar-text" title="标题 3" onMouseDown={(e) => e.preventDefault()} onClick={() => applyType("heading", 3)}>
            H3
          </button>
          <button type="button" className="tool-btn toolbar-text" title="无序列表" onMouseDown={(e) => e.preventDefault()} onClick={() => applyType("bullet")}>
            •
          </button>
          <button type="button" className="tool-btn toolbar-text" title="有序列表" onMouseDown={(e) => e.preventDefault()} onClick={() => applyType("ordered")}>
            1.
          </button>
          <button type="button" className="tool-btn toolbar-text" title="待办" onMouseDown={(e) => e.preventDefault()} onClick={() => applyType("todo")}>
            ☐
          </button>
          <button type="button" className="tool-btn toolbar-text" title="引用" onMouseDown={(e) => e.preventDefault()} onClick={() => applyType("quote")}>
            “
          </button>
        </div>
        <div className="toolbar-group">
          <button type="button" className="tool-btn" title="粗体" aria-label="粗体" onMouseDown={(e) => e.preventDefault()} onClick={() => executeInline("bold")}>
            <strong>B</strong>
          </button>
          <button type="button" className="tool-btn" title="斜体" aria-label="斜体" onMouseDown={(e) => e.preventDefault()} onClick={() => executeInline("italic")}>
            <em>I</em>
          </button>
          <button type="button" className="tool-btn" title="删除线" aria-label="删除线" onMouseDown={(e) => e.preventDefault()} onClick={() => executeInline("strikeThrough")}>
            <s>S</s>
          </button>
          <button type="button" className="tool-btn" title="行内代码" aria-label="行内代码" onMouseDown={(e) => e.preventDefault()} onClick={() => executeInline("code")}>
            <code>{"</>"}</code>
          </button>
        </div>
        <div className="toolbar-group toolbar-spacer">
          {onAddAttachment && (
            <>
              <button
                type="button"
                className="tool-btn"
                title="添加图片或文件"
                aria-label="添加图片或文件"
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => fileInput.current?.click()}
              >
                <PaperclipIcon size={16} />
              </button>
              <input
                ref={fileInput}
                type="file"
                multiple
                className="file-input"
                onChange={(e) => {
                  handleFiles(Array.from(e.target.files ?? []));
                  e.target.value = "";
                }}
              />
            </>
          )}
          <div className="mode-switch" role="group" aria-label="编辑模式">
            <button
              type="button"
              className={mode === "live" ? "is-active" : ""}
              onClick={() => {
                if (mode === "source") void switchToLive();
              }}
              disabled={switching}
            >
              实时渲染
            </button>
            <button
              type="button"
              className={mode === "source" ? "is-active" : ""}
              onClick={() => {
                if (mode === "live") void switchToSource();
              }}
              disabled={switching}
            >
              源码
            </button>
          </div>
        </div>
      </div>

      {mode === "source" ? (
        <textarea
          className="source-editor"
          value={source}
          onChange={(e) => setSource(e.target.value)}
          spellCheck={false}
          aria-label="Markdown 源码"
          rows={Math.max(4, source.split("\n").length)}
        />
      ) : (
        <div className="editor-blocks" ref={editorRoot}>
          {effectiveBlocks.map((block, index) => {
            const attachment = attachmentFor(block);
            if (block.type === "divider") {
              return (
                <div className="ed-row ed-divider" key={block.id}>
                  <hr />
                  <button type="button" className="block-remove" aria-label="删除分隔线" onClick={() => removeBlock(block.id)}>
                    <CloseIcon size={14} />
                  </button>
                </div>
              );
            }
            if (block.type === "image" || block.type === "file") {
              return (
                <div className="ed-row ed-attachment" key={block.id} data-block={block.id}>
                  {attachment ? (
                    <AttachmentBlock backend={backend} attachment={attachment} onRemove={onRemoveAttachment ? () => onRemoveAttachment(attachment.id) : undefined} />
                  ) : (
                    <span className="muted">附件信息缺失</span>
                  )}
                </div>
              );
            }
            if (block.type === "code") {
              return (
                <div className="ed-row ed-code" key={block.id} data-block={block.id}>
                  <textarea
                    className="ed-code-input"
                    value={blockText(block)}
                    placeholder="代码"
                    spellCheck={false}
                    onFocus={() => setActiveId(block.id)}
                    onChange={(e) => updateBlock(block.id, { spans: [{ text: e.target.value }] })}
                  />
                  <button type="button" className="block-remove" aria-label="删除代码块" onClick={() => removeBlock(block.id)}>
                    <CloseIcon size={14} />
                  </button>
                </div>
              );
            }
            return (
              <BlockRow
                key={block.id}
                block={block}
                index={index}
                active={activeBlock?.id === block.id}
                shouldFocus={focusId === block.id}
                placeholder={index === 0 ? placeholder : ""}
                onFocus={() => setActiveId(block.id)}
                onFocused={() => setFocusId(null)}
                onInput={(e) => onBlockInput(e, block)}
                onBlur={(e) => onBlockBlur(e, block)}
                onKeyDown={(e) => onBlockKeyDown(e, block, index)}
                onToggle={() => updateBlock(block.id, { checked: !block.checked })}
                onRemove={() => removeBlock(block.id)}
              />
            );
          })}
          <button type="button" className="add-block" onClick={() => addBlock()}>
            <PlusIcon size={14} /> 添加块
          </button>
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
              onMouseDown={(e) => {
                e.preventDefault();
                commitTag(s.name);
              }}
            >
              <span>#{s.name}</span>
              <span className="tag-count">{s.count}</span>
            </button>
          ))}
        </div>
      )}

      <div className="tag-row">
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
            onChange={(e) => setTagDraft(e.target.value)}
            onBlur={() => {
              addTag(tagDraft);
              setTagDraft("");
              setAddingTag(false);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addTag(tagDraft);
                setTagDraft("");
                setAddingTag(false);
              }
              if (e.key === "Escape") {
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
        <span className="tag-hint">#标签 直接转换 · # 空格 = 标题</span>
      </div>

      {dragging && <div className="drop-hint">松开以添加图片或文件</div>}
    </div>
  );
}

function BlockRow({
  block,
  index,
  active,
  shouldFocus,
  placeholder,
  onFocus,
  onFocused,
  onInput,
  onBlur,
  onKeyDown,
  onToggle,
  onRemove,
}: {
  block: Block;
  index: number;
  active: boolean;
  shouldFocus: boolean;
  placeholder: string;
  onFocus: () => void;
  onFocused: () => void;
  onInput: (e: React.FormEvent<HTMLDivElement>) => void;
  onBlur: (e: React.FocusEvent<HTMLDivElement>) => void;
  onKeyDown: (e: React.KeyboardEvent<HTMLDivElement>) => void;
  onToggle: () => void;
  onRemove: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const focused = useRef(false);

  useEffect(() => {
    if (!ref.current || focused.current) return;
    const html = spansToHtml(block.spans);
    if (ref.current.innerHTML !== html) ref.current.innerHTML = html;
  }, [block.id, block.spans]);

  useEffect(() => {
    if (!shouldFocus || !ref.current) return;
    ref.current.focus();
    setCaret(ref.current, 0);
    onFocused();
  }, [shouldFocus, onFocused]);

  const prefix =
    block.type === "bullet" ? "•" : block.type === "ordered" ? "1." : block.type === "todo" ? null : null;

  return (
    <div
      className={`ed-row ${active ? "is-active" : ""} ed-${block.type} ${block.checked ? "is-done" : ""}`}
      data-block={block.id}
    >
      {block.type === "todo" && (
        <input type="checkbox" className="todo-box" checked={Boolean(block.checked)} onChange={onToggle} aria-label="完成待办" />
      )}
      {prefix && <span className="ed-prefix">{prefix}</span>}
      {block.type === "quote" && <span className="quote-bar" aria-hidden="true" />}
      <div
        ref={ref}
        className="ed-input"
        contentEditable={true}
        suppressContentEditableWarning
        role="textbox"
        aria-multiline="true"
        aria-label={index === 0 ? "记录内容" : `第 ${index + 1} 块`}
        data-placeholder={placeholder}
        onFocus={() => {
          focused.current = true;
          onFocus();
        }}
        onBlur={(e) => {
          focused.current = false;
          onBlur(e);
        }}
        onInput={onInput}
        onKeyDown={onKeyDown}
      />
      <button type="button" className="block-remove" aria-label="删除块" onClick={onRemove}>
        <CloseIcon size={13} />
      </button>
    </div>
  );
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
