import { useRef } from "react";
import type { ReactNode } from "react";
import type { Counts, DayCount, SyncStatus, Tag, ViewId } from "../types";
import { relativeTime } from "../lib/format";
import { Heatmap } from "./common";
import {
  CloudIcon,
  CloseIcon,
  PanelLeftIcon,
  PlusIcon,
  SearchIcon,
  RefreshIcon,
} from "./Icons";

export function Topbar({
  query,
  onQuery,
  sync,
  title,
  sidebarOpen,
  onToggleSidebar,
  onToggleSync,
  onNew,
}: {
  query: string;
  onQuery: (value: string) => void;
  sync: SyncStatus | null;
  title: string;
  sidebarOpen: boolean;
  onToggleSidebar: () => void;
  onToggleSync: () => void;
  onNew: () => void;
}) {
  const state = sync?.state ?? "idle";
  const label = !sync
    ? "同步未就绪"
    : !sync.configured
      ? "未连接 Notion"
      : !sync.enabled
        ? "同步已关闭"
        : state === "offline"
          ? "离线"
          : state === "syncing"
            ? "同步中…"
            : state === "paused"
              ? "同步已暂停"
              : state === "error"
                ? "同步错误"
                : `已同步 · ${relativeTime(sync.last_success)}`;
  const action = !sync
    ? "同步未就绪"
    : !sync.configured
      ? "点击前往设置连接 Notion"
      : !sync.enabled
        ? "点击开启同步"
        : sync.paused || state === "paused"
          ? "点击恢复同步"
          : "点击暂停同步";
  const searchRef = useRef<HTMLInputElement>(null);
  return (
    <header className="topbar">
      <div className="topbar-brand">
        <button
          type="button"
          className="icon-button sidebar-toggle"
          onClick={onToggleSidebar}
          aria-label={sidebarOpen ? "收起侧边栏" : "展开侧边栏"}
          title={sidebarOpen ? "收起侧边栏" : "展开侧边栏"}
          aria-expanded={sidebarOpen}
          aria-controls="app-sidebar"
        >
          <PanelLeftIcon size={17} />
        </button>
        {!sidebarOpen && (
          <>
            <span className="topbar-brand-mark" aria-hidden="true">
              m
            </span>
            <span className="topbar-brand-name">memo</span>
          </>
        )}
        <span className="topbar-title">{title}</span>
      </div>

      <div className="topbar-tools">
        <div className="search-box" onClick={() => searchRef.current?.focus()}>
          <SearchIcon size={14} />
          <input
            ref={searchRef}
            value={query}
            onChange={(e) => onQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape" && query) onQuery("");
            }}
            placeholder="搜索"
            aria-label="搜索笔记"
            spellCheck={false}
          />
          {query ? (
            <button
              type="button"
              className="search-clear"
              aria-label="清空搜索"
              title="清空搜索"
              onClick={(e) => {
                e.stopPropagation();
                onQuery("");
                searchRef.current?.focus();
              }}
            >
              <CloseIcon size={13} />
            </button>
          ) : (
            <kbd>Ctrl K</kbd>
          )}
        </div>
        <button
          type="button"
          className="topbar-new"
          onClick={onNew}
          aria-label="新建记录"
          title="新建记录 · N"
        >
          <PlusIcon size={15} />
        </button>
      </div>

      <div className="topbar-actions">
        <button
          type="button"
          className={`sync-chip is-${state}`}
          onClick={onToggleSync}
          title={sync?.last_error || action}
          aria-label={action}
        >
          <CloudIcon size={14} />
          <span>{label}</span>
          {sync && sync.enabled && sync.pending > 0 && <em>{sync.pending} 待同步</em>}
        </button>
      </div>
    </header>
  );
}

export function ViewHeader({
  title,
  subtitle,
  onRefresh,
  right,
}: {
  title: string;
  subtitle?: string;
  onRefresh?: () => void;
  right?: ReactNode;
}) {
  return (
    <div className="view-header">
      <div>
        <h2>{title}</h2>
        {subtitle && <p>{subtitle}</p>}
      </div>
      <div className="view-actions">
        {onRefresh && (
          <button type="button" className="icon-button" onClick={onRefresh} aria-label="刷新" title="刷新">
            <RefreshIcon size={16} />
          </button>
        )}
        {right}
      </div>
    </div>
  );
}

const weekdayEn = ["SUNDAY", "MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY"];
const weekdayCn = ["日", "一", "二", "三", "四", "五", "六"];
const filters: { id: ViewId; label: string }[] = [
  { id: "all", label: "全部" },
  { id: "local", label: "仅本地" },
  { id: "synced", label: "已同步" },
  { id: "attachments", label: "有附件" },
  { id: "untagged", label: "无标签" },
];

function localIso(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

function dayCounts(heatmap: DayCount[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const day of heatmap) {
    counts.set(day.date, (counts.get(day.date) ?? 0) + day.count);
  }
  return counts;
}

export function JournalHeader({
  view,
  title,
  subtitle,
  tags,
  counts,
  activeTag,
  activeView,
  heatmap,
  onView,
  onTag,
  onRefresh,
  right,
}: {
  view: ViewId;
  title: string;
  subtitle?: string;
  tags: Tag[];
  counts: Counts;
  activeTag: string;
  activeView: ViewId;
  heatmap: DayCount[];
  onView: (view: ViewId) => void;
  onTag: (tag: string) => void;
  onRefresh?: () => void;
  right?: ReactNode;
}) {
  const now = new Date();
  const countsByDay = dayCounts(heatmap);
  const today = localIso(now);
  const todayCount = countsByDay.get(today) ?? 0;
  let weekCount = 0;
  for (let index = 0; index < 7; index += 1) {
    const cursor = new Date(now);
    cursor.setDate(cursor.getDate() - index);
    weekCount += countsByDay.get(localIso(cursor)) ?? 0;
  }
  let streak = 0;
  const cursor = new Date(now);
  if (!(countsByDay.get(localIso(cursor)) ?? 0)) cursor.setDate(cursor.getDate() - 1);
  while (streak < 400 && (countsByDay.get(localIso(cursor)) ?? 0) > 0) {
    streak += 1;
    cursor.setDate(cursor.getDate() - 1);
  }

  const dateView = view !== "tag" && view !== "trash";
  const scope = view === "tag" ? `#${activeTag}` : title;

  return (
    <div className="journal-head">
      <div className="masthead">
        <div className="mast-top">
          <div className="mast-lead">
            <div className="mast-dow">
              {weekdayEn[now.getDay()]} · {now.getFullYear()}
            </div>
            <h1>
              {dateView ? (
                <>
                  {now.getMonth() + 1}月{now.getDate()}日 <em>星期{weekdayCn[now.getDay()]}</em>
                </>
              ) : (
                scope
              )}
            </h1>
            <div className="mast-stats">
              <span>
                今天 <b>{todayCount}</b> 条
              </span>
              <span>
                本周 <b>{weekCount}</b> 条
              </span>
              <span>
                连续记录 <b>{streak}</b> 天
              </span>
            </div>
            {subtitle && <p className="mast-sub">{subtitle}</p>}
          </div>
          <div className="mast-side">
            <Heatmap days={heatmap} weeks={18} compact />
            <div className="mast-actions">
              {onRefresh && (
                <button
                  type="button"
                  className="text-button"
                  onClick={onRefresh}
                  aria-label="刷新"
                  title="刷新"
                >
                  刷新
                </button>
              )}
              {right}
            </div>
          </div>
        </div>
      </div>

      <div className="filters-row" aria-label="筛选">
        {filters.map((filter) => {
          const active = activeView === filter.id && !activeTag;
          return (
            <button
              type="button"
              key={filter.id}
              className={`filter-text ${active ? "is-active" : ""}`}
              onClick={() => onView(filter.id)}
              aria-current={active ? "page" : undefined}
            >
              {filter.label}
              {filter.id === "all" && <em>{counts.notes}</em>}
            </button>
          );
        })}
      </div>

      <div className="tagline" aria-label="标签">
        {tags.map((tag) => (
          <button
            type="button"
            key={tag.id}
            className={activeTag === tag.name ? "is-active" : ""}
            onClick={() => onTag(tag.name)}
            aria-current={activeTag === tag.name ? "page" : undefined}
          >
            #{tag.name}
          </button>
        ))}
        {tags.length === 0 && <span>还没有标签</span>}
      </div>
    </div>
  );
}
