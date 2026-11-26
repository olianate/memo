import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import type { Attachment, Memo } from "../types";
import type { MemoListBackend } from "../backend";
import { BlockPreview } from "../lib/markdown";
import { memoPreview } from "../lib/preview";
import { dayLabel, formatTime, humanBytes } from "../lib/format";
import { EmptyState } from "./common";
import { ChevronIcon, CloseIcon, FileIcon, LockIcon, PinIcon, TrashIcon } from "./Icons";

interface MemoDay {
  day: string;
  memos: Memo[];
}

/** Groups memos by local day, newest day first; pinned memos keep their day. */
function groupByDay(memos: Memo[]): MemoDay[] {
  const days = new Map<string, Memo[]>();
  for (const memo of memos) {
    const day = memo.day || memo.created_at.slice(0, 10);
    const bucket = days.get(day);
    if (bucket) bucket.push(memo);
    else days.set(day, [memo]);
  }
  return Array.from(days.entries())
    .sort(([a], [b]) => (a < b ? 1 : a > b ? -1 : 0))
    .map(([day, list]) => ({ day, memos: list }));
}

function localIso(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

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
  backend: MemoListBackend;
  loading: boolean;
  onOpen: (memo: Memo) => void;
  onChanged: () => void;
  onTag?: (tag: string) => void;
  onError: (message: string) => void;
  emptyTitle?: string;
  emptyHint?: string;
}) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const timelineRef = useRef<HTMLDivElement>(null);
  const shadeRef = useRef<HTMLDivElement>(null);

  const groups = useMemo(() => groupByDay(memos), [memos]);
  const today = localIso(new Date());

  // Once the timeline reaches the top bar, paint an opaque band behind the
  // sticky day headers so entries scrolled underneath can never show through.
  useEffect(() => {
    const scroller = document.querySelector<HTMLElement>(".content");
    const shade = shadeRef.current;
    if (!scroller || !shade) return;
    let frame = 0;
    const update = () => {
      frame = 0;
      const stuck = shade.getBoundingClientRect().top <= 53;
      timelineRef.current?.classList.toggle("is-past-top", stuck);
    };
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    update();
    scroller.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", onScroll);
    return () => {
      scroller.removeEventListener("scroll", onScroll);
      window.removeEventListener("resize", onScroll);
      if (frame) cancelAnimationFrame(frame);
    };
  }, [groups.length]);

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

  const togglePin = async (memo: Memo) => {
    try {
      await backend.setPinned(memo.id, !memo.pinned);
      onChanged();
    } catch (e) {
      onError(e instanceof Error ? e.message : String(e));
    }
  };

  const toggleKeepLocal = async (memo: Memo) => {
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

  return (
    <div className="timeline" ref={timelineRef}>
      <div className="timeline-shade" ref={shadeRef} aria-hidden="true" />
      {groups.map((group) => {
        const label = dayLabel(group.day);
        const isToday = group.day === today;
        return (
          <Fragment key={group.day}>
            {!isToday && (
              <div className="day-header">
                <h3>{label.title}</h3>
                <span className="day-week">{label.sub}</span>
                <i className="day-line" aria-hidden="true" />
                <em>{group.memos.length} 条</em>
              </div>
            )}
            <section className="day-group">
              {group.memos.map((memo) => (
                <MemoCard
                  key={memo.id}
                  memo={memo}
                  backend={backend}
                  open={expanded.has(memo.id)}
                  onToggle={() => toggle(memo.id)}
                  onOpen={() => onOpen(memo)}
                  onTag={onTag}
                  onError={onError}
                  onRemove={() => void remove(memo)}
                  onPin={() => void togglePin(memo)}
                  onKeepLocal={() => void toggleKeepLocal(memo)}
                />
              ))}
            </section>
          </Fragment>
        );
      })}
    </div>
  );
}

function memoState(memo: Memo): string {
  if (memo.local_state === "evicted") return "已清理正文";
  if (memo.sync_error) return "同步失败";
  if (!memo.notion_page_id) return "仅本地";
  if ((memo.attachments ?? []).some((a) => !a.notion_file_id)) return "附件待同步";
  return "已同步";
}

function memoFoot(memo: Memo): string {
  const parts = [memoState(memo)];
  const edited = memo.updated_at !== memo.created_at;
  if (memo.pinned) parts.push(`置顶于 ${formatTime(memo.updated_at)}`);
  else if (edited) parts.push(`编辑于 ${formatTime(memo.updated_at)}`);
  else if ((memo.tags ?? []).length === 0) parts.push("无标签");
  return parts.join(" · ");
}

function MemoCard({
  memo,
  backend,
  open,
  onToggle,
  onOpen,
  onTag,
  onError,
  onRemove,
  onPin,
  onKeepLocal,
}: {
  memo: Memo;
  backend: MemoListBackend;
  open: boolean;
  onToggle: () => void;
  onOpen: () => void;
  onTag?: (tag: string) => void;
  onError: (message: string) => void;
  onRemove: () => void;
  onPin: () => void;
  onKeepLocal: () => void;
}) {
  const preview = memoPreview(memo);

  return (
    <article
      className={`memo-card ${open ? "is-open" : ""} ${memo.deleted ? "is-deleted" : ""} ${memo.pinned ? "is-pinned" : ""}`}
      onClick={onOpen}
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === "Enter" && e.target === e.currentTarget) onOpen();
      }}
    >
      <div className="entry-time">
        {memo.pinned && <PinIcon size={11} />}
        <time dateTime={memo.created_at}>{formatTime(memo.created_at)}</time>
      </div>

      <div className="entry-main">
        {memo.local_state === "evicted" ? (
          <div className="evicted-banner">
            <span>正文与附件已清理，打开这条笔记时从 Notion 恢复。</span>
            <button type="button" className="link-btn" onClick={() => onOpen()}>
              打开并恢复
            </button>
          </div>
        ) : (
          <div className={`memo-body ${open ? "is-open" : ""}`}>
            <BlockPreview
              blocks={preview.blocks}
              limit={open ? 500 : 4}
              onOpenLink={(url) => {
                backend.openURL(url).catch((err) => onError(String(err)));
              }}
            />
          </div>
        )}

        {preview.images.length > 0 && (
          <TimelineImages
            backend={backend}
            attachments={preview.images}
            collapsed={!open && preview.images.length > 2}
            onError={onError}
          />
        )}
        {preview.files.length > 0 && (
          <div className="timeline-files">
            {preview.files.slice(0, open ? 20 : 2).map((file) => (
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

        {(preview.long || (memo.attachments ?? []).length > 0) && (
          <button
            type="button"
            className="expand-button"
            onClick={(e) => {
              e.stopPropagation();
              onToggle();
            }}
          >
            {open ? "收起" : "展开"}
            <ChevronIcon size={14} className={open ? "rotate-180" : ""} />
          </button>
        )}

        {(memo.tags ?? []).length > 0 && (
          <div className="entry-tags" onClick={(e) => e.stopPropagation()}>
            {(memo.tags ?? []).map((tag) => (
              <button
                type="button"
                key={tag.id}
                className="tag-chip"
                onClick={onTag ? () => onTag(tag.name) : undefined}
              >
                <span className="hash" aria-hidden="true">#</span>
                {tag.name}
              </button>
            ))}
          </div>
        )}

        <div className="entry-foot">
          <span className="entry-status">{memoFoot(memo)}</span>
          <div className="memo-actions" onClick={(e) => e.stopPropagation()}>
            <button
              type="button"
              className={`icon-button ${memo.pinned ? "is-on" : ""}`}
              title={memo.pinned ? "取消置顶" : "置顶"}
              aria-label={memo.pinned ? "取消置顶" : "置顶"}
              onClick={onPin}
            >
              <PinIcon size={13} />
            </button>
            <button
              type="button"
              className={`icon-button ${memo.keep_local ? "is-on" : ""}`}
              title={memo.keep_local ? "取消保留本地" : "保留本地"}
              aria-label={memo.keep_local ? "取消保留本地" : "保留本地"}
              onClick={onKeepLocal}
            >
              <LockIcon size={13} />
            </button>
            <button
              type="button"
              className="icon-button danger"
              title={memo.deleted ? "永久删除" : "删除"}
              aria-label={memo.deleted ? "永久删除笔记" : "删除笔记"}
              onClick={onRemove}
            >
              <TrashIcon size={13} />
            </button>
          </div>
        </div>
      </div>
    </article>
  );
}

function TimelineImages({
  backend,
  attachments,
  collapsed,
  onError,
}: {
  backend: MemoListBackend;
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

function TimelineImage({ backend, attachment, onError }: { backend: MemoListBackend; attachment: Attachment; onError: (message: string) => void }) {
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
