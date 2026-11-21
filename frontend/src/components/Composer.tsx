import { useRef, useState } from "react";
import type { Attachment, Block } from "../types";
import type { Backend } from "../backend";
import { MarkdownEditor } from "./MarkdownEditor";
import { SendIcon } from "./Icons";
import { fileToBase64 } from "../lib/files";

function newParagraph(): Block {
  return { id: `draft-${Math.random().toString(36).slice(2, 10)}`, type: "paragraph", spans: [] };
}

export function Composer({
  backend,
  onSaved,
  onError,
}: {
  backend: Backend;
  onSaved: () => void;
  onError: (message: string) => void;
}) {
  const [blocks, setBlocks] = useState<Block[]>([newParagraph()]);
  const [tags, setTags] = useState<string[]>([]);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [draftId, setDraftId] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const uploading = useRef(false);

  const reset = () => {
    setBlocks([newParagraph()]);
    setTags([]);
    setAttachments([]);
    setDraftId(null);
    setError("");
  };

  const hasContent = () => {
    const text = blocks.map((b) => (b.spans ?? []).map((s) => s.text).join("")).join("").trim();
    return text.length > 0 || attachments.length > 0 || blocks.some((b) => b.type === "divider");
  };

  const save = async () => {
    if (saving || uploading.current) return;
    if (!hasContent() && !draftId) return;
    setSaving(true);
    setError("");
    try {
      if (draftId) await backend.update(draftId, blocks, tags, false);
      else await backend.create(blocks, tags);
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
      let id = draftId;
      if (!id) {
        const memo = await backend.create(blocks, tags);
        id = memo.id;
        setDraftId(id);
      }
      for (const file of files) {
        const payload = await fileToBase64(file);
        await backend.addAttachmentBase64(id, payload.name, payload.mime, payload.data);
      }
      const memo = await backend.get(id);
      setBlocks(memo.blocks ?? [newParagraph()]);
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
      setBlocks(memo.blocks ?? [newParagraph()]);
      setAttachments(memo.attachments ?? []);
      onSaved();
    } catch (e) {
      onError(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div className="composer">
      <MarkdownEditor
        backend={backend}
        blocks={blocks}
        tags={tags}
        attachments={attachments}
        onChange={setBlocks}
        onTagsChange={setTags}
        onSubmit={() => void save()}
        submitOnEnter
        placeholder="记录此刻的想法… 支持 Markdown"
        onAddAttachment={(files) => void addFiles(files)}
        onRemoveAttachment={(id) => void removeAttachment(id)}
      />
      <div className="composer-footer">
        <span className="composer-hint">
          {draftId ? "草稿已保存，继续编辑后按 Enter 更新" : "Enter 保存 · Shift+Enter 换行"}
        </span>
        {error && <span className="inline-error">{error}</span>}
        <button type="button" className="send-button" onClick={() => void save()} disabled={saving} aria-label="保存笔记">
          {saving ? <span className="spinner" /> : <SendIcon size={17} />}
        </button>
      </div>
    </div>
  );
}
