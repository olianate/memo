import type { ReactNode } from "react";
import type { DayCount, Memo } from "../types";

export function TagChip({
  name,
  count,
  active,
  onClick,
  onRemove,
}: {
  name: string;
  count?: number;
  active?: boolean;
  onClick?: () => void;
  onRemove?: () => void;
}) {
  const content = (
    <>
      #{name}
      {typeof count === "number" && <span className="tag-count">{count}</span>}
      {onRemove && (
        <span
          role="button"
          tabIndex={0}
          aria-label={`移除标签 ${name}`}
          className="chip-remove"
          onClick={(e) => {
            e.stopPropagation();
            onRemove();
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") onRemove();
          }}
        >
          ×
        </span>
      )}
    </>
  );
  if (onClick) {
    return (
      <button type="button" className={`tag-chip ${active ? "is-active" : ""}`} onClick={onClick}>
        {content}
      </button>
    );
  }
  return <span className="tag-chip">{content}</span>;
}

export function StatusBadge({ memo }: { memo: Memo }) {
  if (memo.local_state === "evicted") {
    return <span className="status-badge is-muted">已清理正文</span>;
  }
  if (memo.sync_error) {
    return <span className="status-badge is-error" title={memo.sync_error}>同步失败</span>;
  }
  if (!memo.notion_page_id) {
    return <span className="status-badge is-local">仅本地</span>;
  }
  if ((memo.attachments ?? []).some((a) => !a.notion_file_id)) {
    return <span className="status-badge is-local">附件待同步</span>;
  }
  return <span className="status-badge is-synced">已同步</span>;
}

export function Heatmap({
  days,
  weeks = 18,
  compact = false,
  onSelect,
}: {
  days: DayCount[];
  weeks?: number;
  compact?: boolean;
  onSelect?: (date: string) => void;
}) {
  if (!days.length) return <div className={`heatmap ${compact ? "is-compact" : ""}`} aria-hidden="true" />;
  const wanted = weeks * 7;
  const slice = days.slice(Math.max(0, days.length - wanted));
  const first = new Date(`${slice[0]?.date ?? days[0].date}T12:00:00`);
  const offset = Number.isNaN(first.getTime()) ? 0 : first.getDay();
  const max = Math.max(1, ...slice.map((d) => d.count));
  const cells: (DayCount | null)[] = [
    ...Array.from({ length: offset }, () => null),
    ...slice,
  ];
  while (cells.length % 7 !== 0) cells.push(null);
  return (
    <div className={`heatmap ${compact ? "is-compact" : ""}`} style={{ gridTemplateRows: `repeat(7, ${compact ? 8 : 10}px)` }}>
      {cells.map((day, index) => {
        if (!day) return <span className="heat-cell is-empty" key={`empty-${index}`} />;
        const level = day.count === 0 ? 0 : Math.min(4, Math.ceil((day.count / max) * 4));
        return (
          <button
            type="button"
            key={day.date}
            className={`heat-cell level-${level}`}
            title={`${day.date} · ${day.count} 条`}
            aria-label={`${day.date}，${day.count} 条笔记`}
            onClick={onSelect ? () => onSelect(day.date) : undefined}
            disabled={!onSelect}
          />
        );
      })}
    </div>
  );
}

export function EmptyState({ title, hint, action }: { title: string; hint?: string; action?: ReactNode }) {
  return (
    <div className="empty-state">
      <div className="empty-mark">m</div>
      <h3>{title}</h3>
      {hint && <p>{hint}</p>}
      {action}
    </div>
  );
}
