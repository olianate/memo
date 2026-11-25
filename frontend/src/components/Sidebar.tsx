import type { ReactNode } from "react";
import type { Counts, DayCount, ViewId } from "../types";
import {
  CloudIcon,
  LockIcon,
  MoonIcon,
  PaperclipIcon,
  PlusIcon,
  SettingsIcon,
  StatsIcon,
  SunIcon,
  TagIcon,
  TrashIcon,
  CheckIcon,
} from "./Icons";

const filters: { id: ViewId; label: string; icon: ReactNode }[] = [
  { id: "all", label: "全部笔记", icon: <CheckIcon size={16} /> },
  { id: "local", label: "仅本地", icon: <LockIcon size={16} /> },
  { id: "synced", label: "已同步", icon: <CloudIcon size={16} /> },
  { id: "attachments", label: "有附件", icon: <PaperclipIcon size={16} /> },
  { id: "untagged", label: "无标签", icon: <TagIcon size={16} /> },
];

const monthNames = ["一月", "二月", "三月", "四月", "五月", "六月", "七月", "八月", "九月", "十月", "十一月", "十二月"];

function recentMonths(heatmap: DayCount[]): string[] {
  const seen = new Set<string>();
  for (const day of heatmap) {
    if (day.date) seen.add(day.date.slice(0, 7));
  }
  if (seen.size === 0) {
    const now = new Date();
    seen.add(`${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`);
  }
  return Array.from(seen).sort().slice(-5).reverse();
}

export function Sidebar({
  counts,
  activeView,
  activeTag,
  heatmap,
  month,
  theme,
  onNew,
  onView,
  onMonth,
  onSettings,
  onTrash,
  onToggleTheme,
}: {
  counts: Counts;
  activeView: ViewId;
  activeTag: string;
  heatmap: DayCount[];
  month: string;
  theme: "light" | "dark" | "system";
  onNew: () => void;
  onView: (view: ViewId) => void;
  onMonth: (month: string) => void;
  onSettings: () => void;
  onTrash: () => void;
  onToggleTheme: () => void;
}) {
  const months = recentMonths(heatmap);
  const now = new Date();
  const currentMonth = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;

  return (
    <aside className="sidebar" id="app-sidebar" aria-label="memo 导航">
      <div className="rail-top">
        <button
          type="button"
          className="rail-mark"
          onClick={() => onView("all")}
          aria-label="memo 全部笔记"
          title="memo"
        >
          m
        </button>
        <button
          type="button"
          className="rail-new"
          onClick={onNew}
          aria-label="新建记录"
          title="新建记录 · N"
        >
          <PlusIcon size={18} />
        </button>
      </div>

      <div className="rail-months" aria-label="按月份浏览">
        {months.map((key) => {
          const [year, value] = key.split("-");
          const active = month === key;
          return (
            <button
              type="button"
              key={key}
              className={`rail-month ${active ? "is-active" : ""} ${key === currentMonth ? "is-current" : ""}`}
              onClick={() => onMonth(key)}
              aria-current={active ? "true" : undefined}
              aria-label={`${year}年${monthNames[Number(value) - 1]}${active ? "，正在查看" : ""}`}
              title={`${year}年${monthNames[Number(value) - 1]}`}
            >
              {Number(value)}
            </button>
          );
        })}
      </div>

      <nav className="rail-nav" aria-label="浏览">
        {filters.map((filter) => {
          const current = activeView === filter.id && !activeTag;
          return (
            <button
              type="button"
              key={filter.id}
              className={`rail-item ${current ? "is-active" : ""}`}
              onClick={() => onView(filter.id)}
              aria-current={current ? "page" : undefined}
              aria-label={`${filter.label}${filter.id === "all" ? `，${counts.notes} 条` : ""}`}
              title={`${filter.label}${filter.id === "all" ? ` · ${counts.notes}` : ""}`}
            >
              {filter.icon}
              <span className="rail-text">{filter.label}</span>
            </button>
          );
        })}
      </nav>

      <div className="rail-foot">
        <button
          type="button"
          className={`rail-item ${activeView === "stats" ? "is-active" : ""}`}
          onClick={() => onView("stats")}
          aria-current={activeView === "stats" ? "page" : undefined}
          aria-label="记录统计"
          title="记录统计"
        >
          <StatsIcon size={16} />
          <span className="rail-text">记录统计</span>
        </button>
        <button
          type="button"
          className={`rail-item ${activeView === "trash" ? "is-active" : ""}`}
          onClick={onTrash}
          aria-current={activeView === "trash" ? "page" : undefined}
          aria-label="回收站"
          title="回收站"
        >
          <TrashIcon size={16} />
          <span className="rail-text">回收站</span>
        </button>
        <button
          type="button"
          className={`rail-item ${activeView === "settings" ? "is-active" : ""}`}
          onClick={onSettings}
          aria-current={activeView === "settings" ? "page" : undefined}
          aria-label="设置"
          title="设置"
        >
          <SettingsIcon size={16} />
          <span className="rail-text">设置</span>
        </button>
        <button
          type="button"
          className="rail-item"
          onClick={onToggleTheme}
          aria-label="切换主题"
          title="切换主题"
        >
          {theme === "dark" ? <SunIcon size={16} /> : <MoonIcon size={16} />}
          <span className="rail-text">切换主题</span>
        </button>
      </div>
    </aside>
  );
}
