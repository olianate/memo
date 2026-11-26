import { useRef, useState } from "react";
import type { Attachment } from "../types";
import type { MemoDraftBackend } from "../backend";
import { MarkdownEditor } from "./MarkdownEditor";
import { fileToBase64 } from "../lib/files";
import { countWords } from "../lib/format";
import { blockText, blocksFromMarkdown } from "../lib/markdown";

export function Composer({
  backend,
  onSaved,
  onError,
  submitOnEnter = true,
}: {
  backend: MemoDraftBackend;
  onSaved: () => void;
  onError: (message: string) => void;
  submitOnEnter?: boolean;
}) {
  const [body, setBody] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [draftId, setDraftId] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const uploading = useRef(false);

  const reset = () => {
    setBody("");
    setTags([]);
    setAttachments([]);
    setDraftId(null);
    setError("");
  };

  const hasContent = () => body.trim().length > 0 || attachments.length > 0;
  const saveKey = submitOnEnter ? "Enter" : "Shift+Enter";
  const newlineKey = submitOnEnter ? "Shift+Enter" : "Enter";
  const now = new Date();
  const nowTime = `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}`;
  const todayLabel = `${now.getMonth() + 1}月${now.getDate()}日 · ${nowTime}`;
  const wordCount = countWords(blocksFromMarkdown(body).map(blockText).join(" "));

  const save = async () => {
    if (saving || uploading.current) return;
    if (!hasContent() && !draftId) return;
    setSaving(true);
    setError("");
    try {
      if (draftId) await backend.update(draftId, body, tags, false);
      else await backend.create(body, tags);
      reset();
      onSaved();
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setError(message);
      onError(message);
    } finally {
      setSaving(false);
    }
  };

  const addFiles = async (files: File[]) => {
    if (files.length === 0) return;
    uploading.current = true;
    setError("");
    try {
      const payloads = await Promise.all(files.map(fileToBase64));
      let id = draftId;
      if (!id) {
        const memo = await backend.createWithAttachments(body, tags, payloads);
        id = memo.id;
        setDraftId(id);
      } else {
        await backend.addAttachments(id, payloads);
      }
      const memo = await backend.get(id);
      setBody(memo.body ?? "");
      setTags((memo.tags ?? []).map((t) => t.name));
      setAttachments(memo.attachments ?? []);
      onSaved();
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setError(message);
      onError(message);
    } finally {
      uploading.current = false;
    }
  };

  const removeAttachment = async (attachmentId: string) => {
    if (!draftId) return;
    try {
      await backend.removeAttachment(attachmentId);
      const memo = await backend.get(draftId);
      setBody(memo.body ?? "");
      setAttachments(memo.attachments ?? []);
      onSaved();
    } catch (e) {
      onError(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div className="composer">
      <div className="composer-head">
        <span className="composer-eyebrow">现在</span>
        <span className="composer-date">{todayLabel}</span>
      </div>
      <MarkdownEditor
        backend={backend}
        body={body}
        tags={tags}
        attachments={attachments}
        onBodyChange={setBody}
        onTagsChange={setTags}
        onSubmit={() => void save()}
        submitOnEnter={submitOnEnter}
        placeholder="写点什么，不必想好标题…"
        toolsInFooter
        footerHint={
          draftId ? `草稿已保存，继续编辑后按 ${saveKey} 更新` : `${saveKey} 保存 · ${newlineKey} 换行`
        }
        onAddAttachment={(files) => void addFiles(files)}
        onRemoveAttachment={(id) => void removeAttachment(id)}
      />
      <div className="composer-footer">
        {error && <span className="inline-error">{error}</span>}
        <span className="composer-word-count" aria-label={`字数 ${wordCount}`}>
          字数 {wordCount}
        </span>
        <button type="button" className="send-button" onClick={() => void save()} disabled={saving} aria-label="保存笔记">
          {saving ? (
            <span className="spinner" />
          ) : (
            <>
              <span>保存</span>
              <span className="send-arrow" aria-hidden="true">
                ↑
              </span>
            </>
          )}
        </button>
      </div>
    </div>
  );
}
