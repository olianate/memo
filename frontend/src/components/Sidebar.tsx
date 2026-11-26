import type { DayCount, ViewId } from "../types";
import {
  MoonIcon,
  PlusIcon,
  SettingsIcon,
  StatsIcon,
  SunIcon,
  TrashIcon,
} from "./Icons";

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
  activeView,
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
  activeView: ViewId;
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
              {String(Number(value)).padStart(2, "0")}
            </button>
          );
        })}
      </div>

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
        <span className="rail-sep" aria-hidden="true" />
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
