import type { ReactNode } from "react";
import type { SyncStatus } from "../types";
import { relativeTime } from "../lib/format";
import { CloudIcon, MoonIcon, SearchIcon, SunIcon, RefreshIcon } from "./Icons";

export function Topbar({
  query,
  onQuery,
  sync,
  theme,
  onToggleTheme,
  onToggleSync,
}: {
  query: string;
  onQuery: (value: string) => void;
  sync: SyncStatus | null;
  theme: "light" | "dark" | "system";
  onToggleTheme: () => void;
  onToggleSync: () => void;
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
  return (
    <header className="topbar">
      <div className="topbar-brand">
        <span className="topbar-dot" aria-hidden="true" />
        <span>memo</span>
        <span className="mini-badge">local-first</span>
      </div>

      <label className="search-box">
        <SearchIcon size={16} />
        <input
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          placeholder="搜索 / 快速记录"
          aria-label="搜索笔记"
          spellCheck={false}
        />
        <kbd>Ctrl K</kbd>
      </label>

      <div className="topbar-actions">
        <button
          type="button"
          className={`sync-chip is-${state}`}
          onClick={onToggleSync}
          title={sync?.last_error || action}
          aria-label={action}
        >
          <CloudIcon size={15} />
          <span>{label}</span>
          {sync && sync.enabled && sync.pending > 0 && <em>{sync.pending} 待同步</em>}
        </button>
        <button type="button" className="icon-button" onClick={onToggleTheme} aria-label="切换主题" title="切换主题">
          {theme === "dark" ? <SunIcon size={17} /> : <MoonIcon size={17} />}
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
