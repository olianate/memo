import { useEffect, useRef, useState } from "react";
import type { Attachment, Block, SyncStatus } from "./types";
import type { Backend } from "./backend";
import { isWailsRuntime, onAppEvent } from "./backend";
import { wailsBackend } from "./wailsBackend";
import { demoBackend } from "./demoBackend";
import { MarkdownEditor } from "./components/MarkdownEditor";
import { fileToBase64 } from "./lib/files";
import { installGlobalErrorHandlers, reportError, setErrorSink } from "./lib/errorLog";
import { CloseIcon, PaperclipIcon, SendIcon } from "./components/Icons";

function newParagraph(): Block {
  return { id: `sticky-${Math.random().toString(36).slice(2, 10)}`, type: "paragraph", spans: [] };
}

export default function StickyApp({ backend }: { backend?: Backend }) {
  const b = backend ?? (isWailsRuntime() ? wailsBackend : demoBackend);
  const [blocks, setBlocks] = useState<Block[]>([newParagraph()]);
  const [tags, setTags] = useState<string[]>([]);
  const [knownTags, setKnownTags] = useState<{ id: string; name: string; count: number }[]>([]);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [draftId, setDraftId] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");
  const [syncLabel, setSyncLabel] = useState("本地优先");
  const uploading = useRef(false);

  useEffect(() => {
    setErrorSink((report) => b.reportError(report));
    const removeHandlers = installGlobalErrorHandlers();
    return () => {
      removeHandlers();
      setErrorSink(null);
    };
  }, [b]);

  useEffect(() => {
    void b.tags().then((rows) => setKnownTags(rows.map((t) => ({ id: t.id, name: t.name, count: t.count }))));
  }, [b]);

  useEffect(() => {
    const apply = (status: SyncStatus) => {
      setSyncLabel(
        !status.enabled
          ? "本地优先"
          : status.state === "syncing"
            ? "同步中…"
            : status.state === "error"
              ? "同步错误"
              : status.state === "paused"
                ? "同步已暂停"
                : "已同步",
      );
    };
    void b.syncStatus().then(apply).catch(() => {});
    return onAppEvent("sync:status", (data) => apply(data as SyncStatus));
  }, [b]);

  const reset = () => {
    setBlocks([newParagraph()]);
    setTags([]);
    setAttachments([]);
    setDraftId(null);
  };

  const save = async () => {
    if (saving || uploading.current) return;
    const text = blocks.map((block) => (block.spans ?? []).map((s) => s.text).join("")).join("").trim();
    if (!text && attachments.length === 0 && !draftId) return;
    setSaving(true);
    setError("");
    try {
      if (draftId) await b.update(draftId, blocks, tags, false);
      else await b.create(blocks, tags);
      reset();
      setSaved(true);
      window.setTimeout(() => setSaved(false), 2000);
    } catch (e) {
      reportError(e, { action: "save", draft_id: draftId }, "sticky");
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  };

  const hide = () => {
    void b.hideSticky().catch(() => {});
  };

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !event.defaultPrevented) {
        event.preventDefault();
        hide();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const addFiles = async (files: File[]) => {
    if (!files.length) return;
    uploading.current = true;
    try {
      let id = draftId;
      if (!id) {
        const memo = await b.create(blocks, tags);
        id = memo.id;
        setDraftId(id);
      }
      for (const file of files) {
        const payload = await fileToBase64(file);
        await b.addAttachmentBase64(id, payload.name, payload.mime, payload.data);
      }
      const memo = await b.get(id);
      setBlocks(memo.blocks ?? [newParagraph()]);
      setTags((memo.tags ?? []).map((t) => t.name));
      setAttachments(memo.attachments ?? []);
    } catch (e) {
      reportError(e, { action: "add_attachment", draft_id: draftId }, "sticky");
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      uploading.current = false;
    }
  };

  const removeAttachment = async (id: string) => {
    if (!draftId) return;
    try {
      await b.removeAttachment(id);
      const memo = await b.get(draftId);
      setBlocks(memo.blocks ?? [newParagraph()]);
      setAttachments(memo.attachments ?? []);
    } catch (e) {
      reportError(e, { action: "remove_attachment", draft_id: draftId, attachment_id: id }, "sticky");
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div className="sticky-root">
      <header className="sticky-header sticky-drag">
        <div className="sticky-title">
          <span className="sticky-dot" />
          <strong>快速记录</strong>
          <kbd>Ctrl Shift Space</kbd>
        </div>
        <div className="sticky-actions">
          {saved && <span className="saved-pill">已保存</span>}
          <button
            type="button"
            className="icon-button"
            aria-label="添加附件"
            title="添加附件"
            onClick={() => document.querySelector<HTMLInputElement>(".sticky-file-input")?.click()}
          >
            <PaperclipIcon size={16} />
          </button>
          <button type="button" className="icon-button" aria-label="关闭快速记录" title="Esc 关闭" onClick={hide}>
            <CloseIcon size={16} />
          </button>
        </div>
      </header>

      <div className="sticky-editor">
        <MarkdownEditor
          backend={b}
          blocks={blocks}
          tags={tags}
          attachments={attachments}
          onChange={setBlocks}
          onTagsChange={setTags}
          onSubmit={() => void save()}
          onEscape={hide}
          submitOnEnter
          autoFocus
          compact
          placeholder="记下此刻的想法…"
          onAddAttachment={(files) => void addFiles(files)}
          onRemoveAttachment={(id) => void removeAttachment(id)}
        />
        <input
          type="file"
          multiple
          className="file-input sticky-file-input"
          onChange={(e) => {
            void addFiles(Array.from(e.target.files ?? []));
            e.target.value = "";
          }}
        />
      </div>

      <footer className="sticky-footer">
        <span>
          {error ? <span className="inline-error">{error}</span> : `Enter 保存 · Shift+Enter 换行 · Esc 隐藏 · ${syncLabel}`}
        </span>
        <button type="button" className="send-button small" onClick={() => void save()} disabled={saving} aria-label="保存">
          {saving ? <span className="spinner" /> : <SendIcon size={15} />}
        </button>
      </footer>

      {draftId && <div className="sticky-draft-hint">草稿已存在本地，继续输入后 Enter 更新</div>}
      <span className="sr-only">{knownTags.length} 个已有标签</span>
    </div>
  );
}
