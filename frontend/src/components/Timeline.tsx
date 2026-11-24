import { useEffect, useState } from "react";
import type { Attachment, Memo } from "../types";
import type { Backend } from "../backend";
import { blocksFromMarkdown, BlockPreview } from "../lib/markdown";
import { groupByDay } from "../lib/group";
import { dayLabel, formatTime, humanBytes } from "../lib/format";
import { EmptyState, StatusBadge, TagChip } from "./common";
import { ChevronIcon, CloseIcon, FileIcon, PinIcon, TrashIcon } from "./Icons";

export function Timeline({
  memos,
  backend,
  loading,
  onOpen,
  onChanged,
  onTag,
  onError,
  emptyTitle = "这里还很安静",
  emptyHint = "在上面的输入框记下第一个想法。Enter 保存，Shift+Enter 换行。",
}: {
  memos: Memo[];
  backend: Backend;
  loading: boolean;
  onOpen: (memo: Memo) => void;
  onChanged: () => void;
  onTag?: (tag: string) => void;
  onError: (message: string) => void;
  emptyTitle?: string;
  emptyHint?: string;
}) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const toggle = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const remove = async (memo: Memo) => {
    try {
      if (memo.deleted) {
        if (!window.confirm("永久删除这条笔记？本地内容和附件会被删除，此操作不可撤销。")) return;
        await backend.purgeMemo(memo.id);
      } else {
        await backend.remove(memo.id);
      }
      onChanged();
    } catch (e) {
      onError(e instanceof Error ? e.message : String(e));
    }
  };

  const pin = async (memo: Memo) => {
    try {
      await backend.setKeepLocal(memo.id, !memo.keep_local);
      onChanged();
    } catch (e) {
      onError(e instanceof Error ? e.message : String(e));
    }
  };

  if (loading && memos.length === 0) {
    return (
      <div className="timeline-loading">
        <span className="spinner" />
        正在加载…
      </div>
    );
  }

  if (!loading && memos.length === 0) {
    return <EmptyState title={emptyTitle} hint={emptyHint} />;
  }

  const groups = groupByDay(memos);
  return (
    <div className="timeline">
      {groups.map((group) => {
        const label = dayLabel(group.day);
        return (
          <section className="day-group" key={group.day}>
            <div className="day-header">
              <h3>{label.title}</h3>
              <span>{label.sub}</span>
              <em>{group.memos.length} 条</em>
            </div>
            {group.memos.map((memo) => {
              const open = expanded.has(memo.id);
              const blocks = memo.body ? blocksFromMarkdown(memo.body) : [];
              // Evicted attachments keep metadata but no bytes; the eviction
              // banner explains the state instead of rendering broken media.
              const images = (memo.attachments ?? []).filter((a) => a.kind === "image" && a.present);
              const files = (memo.attachments ?? []).filter((a) => a.kind === "file" && a.present);
              const long = memo.body.split("\n").filter((line) => line.trim()).length > 6;
              const updated = memo.updated_at !== memo.created_at;
              return (
                <article
                  key={memo.id}
                  className={`memo-card ${open ? "is-open" : ""} ${memo.deleted ? "is-deleted" : ""}`}
                  onClick={() => onOpen(memo)}
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && e.target === e.currentTarget) onOpen(memo);
                  }}
                >
                  <div className="memo-meta">
                    <time dateTime={memo.created_at}>{formatTime(memo.created_at)}</time>
                    {updated && <span className="edited">已编辑 {formatTime(memo.updated_at)}</span>}
                    <StatusBadge memo={memo} />
                    <div className="memo-actions" onClick={(e) => e.stopPropagation()}>
                      <button
                        type="button"
                        className={`icon-button ${memo.keep_local ? "is-on" : ""}`}
                        title={memo.keep_local ? "取消保留本地" : "保留本地"}
                        aria-label={memo.keep_local ? "取消保留本地" : "保留本地"}
                        onClick={() => void pin(memo)}
                      >
                        <PinIcon size={15} />
                      </button>
                      <button
                        type="button"
                        className="icon-button danger"
                        title={memo.deleted ? "永久删除" : "删除"}
                        aria-label={memo.deleted ? "永久删除笔记" : "删除笔记"}
                        onClick={() => void remove(memo)}
                      >
                        <TrashIcon size={15} />
                      </button>
                    </div>
                  </div>

                  {(memo.tags ?? []).length > 0 && (
                    <div className="memo-tags" onClick={(e) => e.stopPropagation()}>
                      {(memo.tags ?? []).map((tag) => (
                        <TagChip key={tag.id} name={tag.name} onClick={onTag ? () => onTag(tag.name) : undefined} />
                      ))}
                    </div>
                  )}

                  {memo.local_state === "evicted" ? (
                    <div className="evicted-banner">
                      <span>正文与附件已清理，打开这条笔记时从 Notion 恢复。</span>
                      <button type="button" className="link-btn" onClick={() => onOpen(memo)}>
                        打开并恢复
                      </button>
                    </div>
                  ) : (
                    <div className={`memo-body ${open ? "is-open" : ""}`}>
                      <BlockPreview
                        blocks={blocks}
                        limit={open ? 500 : 4}
                        onOpenLink={(url) => {
                          backend.openURL(url).catch((err) => onError(String(err)));
                        }}
                      />
                    </div>
                  )}

                  {images.length > 0 && (
                    <TimelineImages
                      backend={backend}
                      attachments={images}
                      collapsed={!open && images.length > 2}
                      onError={onError}
                    />
                  )}
                  {files.length > 0 && (
                    <div className="timeline-files">
                      {files.slice(0, open ? 20 : 2).map((file) => (
                        <button
                          type="button"
                          className="file-chip"
                          key={file.id}
                          onClick={(e) => {
                            e.stopPropagation();
                            backend.openAttachment(file.id).catch((err) => onError(String(err)));
                          }}
                        >
                          <FileIcon size={14} />
                          <span>{file.name}</span>
                          <em>{humanBytes(file.bytes)}</em>
                        </button>
                      ))}
                    </div>
                  )}

                  {(long || (memo.attachments ?? []).length > 0) && (
                    <button
                      type="button"
                      className="expand-button"
                      onClick={(e) => {
                        e.stopPropagation();
                        toggle(memo.id);
                      }}
                    >
                      {open ? "收起" : "展开"}
                      <ChevronIcon size={14} className={open ? "rotate-180" : ""} />
                    </button>
                  )}
                </article>
              );
            })}
          </section>
        );
      })}
    </div>
  );
}

function TimelineImages({
  backend,
  attachments,
  collapsed,
  onError,
}: {
  backend: Backend;
  attachments: Attachment[];
  collapsed: boolean;
  onError: (message: string) => void;
}) {
  const visible = collapsed ? attachments.slice(0, 2) : attachments;
  return (
    <div className={`timeline-images ${collapsed ? "is-collapsed" : ""}`} onClick={(e) => e.stopPropagation()}>
      {visible.map((attachment) => (
        <TimelineImage key={attachment.id} backend={backend} attachment={attachment} onError={onError} />
      ))}
      {collapsed && attachments.length > 2 && <span className="more-images">+{attachments.length - 2}</span>}
    </div>
  );
}

function TimelineImage({ backend, attachment, onError }: { backend: Backend; attachment: Attachment; onError: (message: string) => void }) {
  const [thumb, setThumb] = useState<string | null>(null);
  const [full, setFull] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    backend
      .attachmentThumb(attachment.id)
      .then((url) => {
        if (alive) setThumb(url);
      })
      .catch((e) => {
        if (alive) onError(String(e));
      });
    return () => {
      alive = false;
    };
  }, [attachment.id, backend, onError]);

  const open = async () => {
    try {
      setFull(await backend.attachmentData(attachment.id));
    } catch (e) {
      onError(String(e));
    }
  };

  return (
    <>
      <button type="button" className="timeline-image" onClick={() => void open()} aria-label={`查看图片 ${attachment.name}`}>
        {thumb ? <img src={thumb} alt={attachment.name} /> : <span className="image-skeleton" />}
      </button>
      {full && (
        <div className="lightbox" role="dialog" aria-modal="true" onClick={() => setFull(null)}>
          <button type="button" className="lightbox-close" aria-label="关闭图片" onClick={() => setFull(null)}>
            <CloseIcon size={18} />
          </button>
          <img src={full} alt={attachment.name} onClick={(e) => e.stopPropagation()} />
        </div>
      )}
    </>
  );
}
