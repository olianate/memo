import { useEffect, useState } from "react";
import type { Attachment, Memo } from "../types";
import type { MemoSheetBackend } from "../backend";
import { MarkdownEditor } from "./MarkdownEditor";
import { formatDate } from "../lib/format";
import { fileToBase64 } from "../lib/files";
import { CloseIcon, LockIcon, PinIcon, TrashIcon } from "./Icons";
import { StatusBadge } from "./common";

export function MemoSheet({
  memo,
  backend,
  onClose,
  onChanged,
  onError,
  submitOnEnter = true,
  inline = false,
}: {
  memo: Memo;
  backend: MemoSheetBackend;
  onClose: () => void;
  onChanged: (memo: Memo | null) => void;
  onError: (message: string) => void;
  submitOnEnter?: boolean;
  inline?: boolean;
}) {
  const [body, setBody] = useState(memo.body ?? "");
  const [tags, setTags] = useState<string[]>((memo.tags ?? []).map((t) => t.name));
  const [attachments, setAttachments] = useState<Attachment[]>(memo.attachments ?? []);
  const [keepLocal, setKeepLocal] = useState(memo.keep_local);
  const [pinned, setPinned] = useState(memo.pinned);
  const [deleted, setDeleted] = useState(memo.deleted);
  const [saving, setSaving] = useState(false);
  const [purging, setPurging] = useState(false);
  const [error, setError] = useState("");
  const [evicted, setEvicted] = useState(memo.local_state === "evicted");
  const [hydrating, setHydrating] = useState(false);

  // The sheet is mounted before an evicted memo finishes hydrating; resync when
  // the parent replaces the placeholder with the hydrated memo.
  useEffect(() => {
    setBody(memo.body ?? "");
    setTags((memo.tags ?? []).map((t) => t.name));
    setAttachments(memo.attachments ?? []);
    setKeepLocal(memo.keep_local);
    setPinned(memo.pinned);
    setDeleted(memo.deleted);
    setEvicted(memo.local_state === "evicted");
  }, [memo.id, memo.revision, memo.local_state]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const refresh = async (id: string) => {
    const fresh = await backend.get(id);
    setBody(fresh.body ?? "");
    setTags((fresh.tags ?? []).map((t) => t.name));
    setAttachments(fresh.attachments ?? []);
    setKeepLocal(fresh.keep_local);
    setPinned(fresh.pinned);
    setDeleted(fresh.deleted);
    setEvicted(fresh.local_state === "evicted");
    onChanged(fresh);
    return fresh;
  };

  const hydrate = async () => {
    setHydrating(true);
    try {
      await backend.hydrate(memo.id);
      await refresh(memo.id);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setHydrating(false);
    }
  };

  const save = async () => {
    if (saving || deleted) return;
    setSaving(true);
    setError("");
    try {
      const updated = await backend.update(memo.id, body, tags, keepLocal);
      onChanged(updated);
      onClose();
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setError(message);
      onError(message);
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    try {
      await backend.remove(memo.id);
      onChanged(null);
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const purge = async () => {
    if (!window.confirm("永久删除这条笔记？本地内容和附件会被删除，此操作不可撤销。")) return;
    setPurging(true);
    setError("");
    try {
      await backend.purgeMemo(memo.id);
      onChanged(null);
      onClose();
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setError(message);
      onError(message);
    } finally {
      setPurging(false);
    }
  };

  const restore = async () => {
    try {
      const restored = await backend.restore(memo.id);
      setDeleted(false);
      onChanged(restored);
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const toggleKeepLocal = async () => {
    const next = !keepLocal;
    setKeepLocal(next);
    try {
      await backend.setKeepLocal(memo.id, next);
      const fresh = await backend.get(memo.id);
      onChanged(fresh);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const togglePinned = async () => {
    const next = !pinned;
    setPinned(next);
    try {
      await backend.setPinned(memo.id, next);
      const fresh = await backend.get(memo.id);
      onChanged(fresh);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const addFiles = async (files: File[]) => {
    if (!files.length) return;
    try {
      const payloads = await Promise.all(files.map(fileToBase64));
      await backend.addAttachments(memo.id, payloads);
      await refresh(memo.id);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const removeAttachment = async (id: string) => {
    try {
      await backend.removeAttachment(id);
      await refresh(memo.id);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const created = new Date(memo.created_at);
  const weekday = ["日", "一", "二", "三", "四", "五", "六"][created.getDay()];
  const readerTitle = `${created.getMonth() + 1}月${created.getDate()}日 星期${weekday}`;

  const panel = (
    <div
      className={`sheet ${inline ? "is-inline" : ""}`}
      role={inline ? undefined : "dialog"}
      aria-modal={inline ? undefined : true}
      aria-label="编辑笔记"
      onClick={(e) => e.stopPropagation()}
    >
        <header className="sheet-header">
          <div>
            <h2>{deleted ? "回收站中的笔记" : inline ? readerTitle : "编辑笔记"}</h2>
            <p>
              {formatDate(memo.created_at)}
              {memo.updated_at !== memo.created_at && ` · 编辑于 ${formatDate(memo.updated_at)}`}
              {` · 版本 ${memo.revision}`}
            </p>
          </div>
          <div className="sheet-actions">
            <StatusBadge memo={{ ...memo, deleted }} />
            <button
              type="button"
              className={`icon-button ${pinned ? "is-on" : ""}`}
              onClick={() => void togglePinned()}
              title={pinned ? "取消置顶" : "置顶"}
              aria-label={pinned ? "取消置顶" : "置顶"}
            >
              <PinIcon size={17} />
            </button>
            <button
              type="button"
              className={`icon-button ${keepLocal ? "is-on" : ""}`}
              onClick={() => void toggleKeepLocal()}
              title={keepLocal ? "取消保留本地" : "保留本地"}
              aria-label={keepLocal ? "取消保留本地" : "保留本地"}
            >
              <LockIcon size={17} />
            </button>
            <button
              type="button"
              className="icon-button danger"
              onClick={() => void (deleted ? purge() : remove())}
              title={deleted ? "永久删除" : "删除"}
              aria-label={deleted ? "永久删除" : "删除"}
              disabled={purging}
            >
              <TrashIcon size={17} />
            </button>
            <button type="button" className="icon-button" onClick={onClose} aria-label="关闭">
              <CloseIcon size={18} />
            </button>
          </div>
        </header>

        {evicted ? (
          <div className="hydrate-panel">
            <h3>正文已从本地清理</h3>
            <p>元数据、标签和搜索索引仍然在。打开这条笔记会从 Notion 重新取回正文与附件。</p>
            <button type="button" className="primary-button" onClick={() => void hydrate()} disabled={hydrating}>
              {hydrating ? "正在恢复…" : "从 Notion 恢复"}
            </button>
            {error && <div className="inline-error">{error}</div>}
          </div>
        ) : (
          <>
            <div className="sheet-editor">
              <MarkdownEditor
                backend={backend}
                body={body}
                tags={tags}
                attachments={attachments}
                onBodyChange={setBody}
                onTagsChange={setTags}
                onSubmit={() => void save()}
                submitOnEnter={submitOnEnter}
                autoFocus
                onAddAttachment={(files) => void addFiles(files)}
                onRemoveAttachment={(id) => void removeAttachment(id)}
              />
            </div>
            {error && <div className="inline-error sheet-error">{error}</div>}
            <footer className="sheet-footer">
              <div className="sheet-toggles">
                <label className="checkbox-row">
                  <input type="checkbox" checked={pinned} onChange={() => void togglePinned()} />
                  <span>置顶（显示在最前）</span>
                </label>
                <label className="checkbox-row">
                  <input type="checkbox" checked={keepLocal} onChange={() => void toggleKeepLocal()} />
                  <span>保留本地（不参与清理）</span>
                </label>
              </div>
              <div className="sheet-footer-actions">
                {deleted ? (
                  <>
                    <button type="button" className="secondary-button" onClick={() => void restore()}>
                      恢复
                    </button>
                    <button
                      type="button"
                      className="secondary-button danger"
                      onClick={() => void purge()}
                      disabled={purging}
                    >
                      {purging ? "删除中…" : "永久删除"}
                    </button>
                  </>
                ) : (
                  <button type="button" className="secondary-button" onClick={onClose}>
                    取消
                  </button>
                )}
                <button type="button" className="primary-button" onClick={() => void save()} disabled={saving || deleted}>
                  {saving ? "保存中…" : "保存"}
                </button>
              </div>
            </footer>
          </>
        )}
      </div>
  );

  if (inline) {
    return (
      <div className="reader-page">
        <button type="button" className="reader-back" onClick={onClose}>
          <span aria-hidden="true">←</span> 返回时间线
        </button>
        {panel}
      </div>
    );
  }

  return (
    <div className="sheet-backdrop" onClick={onClose}>
      {panel}
    </div>
  );
}
