import type { ReactNode } from "react";
import type { Counts, DayCount, Tag, ViewId } from "../types";
import { normalizeTag } from "../lib/tag";
import { Heatmap } from "./common";
import {
  CloudIcon,
  LockIcon,
  PaperclipIcon,
  SettingsIcon,
  StatsIcon,
  TagIcon,
  TrashIcon,
  CheckIcon,
} from "./Icons";

const filters: { id: ViewId; label: string; icon: ReactNode; key: string }[] = [
  { id: "all", label: "全部笔记", icon: <CheckIcon size={16} />, key: "notes" },
  { id: "local", label: "本地 only", icon: <LockIcon size={16} />, key: "local" },
  { id: "synced", label: "已同步", icon: <CloudIcon size={16} />, key: "synced" },
  { id: "attachments", label: "有附件", icon: <PaperclipIcon size={16} />, key: "attachments" },
  { id: "untagged", label: "无标签", icon: <TagIcon size={16} />, key: "untagged" },
  { id: "stats", label: "记录统计", icon: <StatsIcon size={16} />, key: "stats" },
];

export function Sidebar({
  counts,
  tags,
  activeView,
  activeTag,
  heatmap,
  onView,
  onTag,
  onSettings,
  onTrash,
}: {
  counts: Counts;
  tags: Tag[];
  activeView: ViewId;
  activeTag: string;
  heatmap: DayCount[];
  onView: (view: ViewId) => void;
  onTag: (tag: string) => void;
  onSettings: () => void;
  onTrash: () => void;
}) {
  return (
    <aside className="sidebar">
      <div className="brand">
        <div className="brand-mark" aria-hidden="true">
          m
        </div>
        <div>
          <div className="brand-name">memo</div>
          <div className="brand-sub">local-first</div>
        </div>
      </div>

      <div className="totals">
        <div>
          <strong>{counts.notes}</strong>
          <span>笔记</span>
        </div>
        <div>
          <strong>{counts.tags}</strong>
          <span>标签</span>
        </div>
        <div>
          <strong>{counts.days}</strong>
          <span>天</span>
        </div>
      </div>

      <Heatmap days={heatmap} weeks={18} compact />

      <nav className="filters" aria-label="筛选">
        {filters.map((f) => {
          if (f.id === "stats") {
            return (
              <div key={f.id} className="filter-separated">
                <button
                  type="button"
                  className={`filter-row ${activeView === f.id ? "is-active" : ""}`}
                  onClick={() => onView(f.id)}
                >
                  {f.icon}
                  <span>{f.label}</span>
                </button>
              </div>
            );
          }
          const badge = f.key === "notes" ? counts.notes : undefined;
          return (
            <button
              type="button"
              key={f.id}
              className={`filter-row ${activeView === f.id ? "is-active" : ""}`}
              onClick={() => onView(f.id)}
            >
              {f.icon}
              <span>{f.label}</span>
              {badge !== undefined && <em>{badge}</em>}
            </button>
          );
        })}
      </nav>

      <div className="sidebar-section-title">
        <span>全部标签</span>
        <TagIcon size={13} />
      </div>
      <div className="tag-list">
        {tags.map((tag) => (
          <button
            type="button"
            key={tag.id}
            className={`tag-row ${normalizeTag(activeTag) === tag.normalized ? "is-active" : ""}`}
            onClick={() => onTag(tag.name)}
          >
            <span>#{tag.name}</span>
            <em>{tag.count}</em>
          </button>
        ))}
        {tags.length === 0 && <div className="tag-empty">还没有标签</div>}
      </div>

      <div className="sidebar-footer">
        <button type="button" className="filter-row" onClick={onSettings}>
          <SettingsIcon size={16} />
          <span>设置</span>
        </button>
        <button type="button" className="filter-row" onClick={onTrash}>
          <TrashIcon size={16} />
          <span>回收站</span>
        </button>
      </div>
    </aside>
  );
}
