import { useEffect, useMemo, useSyncExternalStore } from "react";
import type { Backend } from "./backend";
import { isWailsRuntime } from "./backend";
import { wailsBackend } from "./wailsBackend";
import { demoBackend } from "./demoBackend";
import type { ViewId } from "./types";
import { relativeTime } from "./lib/format";
import { SettingsController } from "./lib/settings";
import { WorkspaceController, useWorkspace } from "./lib/workspace";
import { Sidebar } from "./components/Sidebar";
import { JournalHeader, Topbar, ViewHeader } from "./components/Header";
import { Composer } from "./components/Composer";
import { Timeline } from "./components/Timeline";
import { MemoSheet } from "./components/MemoSheet";
import { SearchPanel } from "./components/SearchPanel";
import { StatsPage } from "./components/StatsPage";
import { SettingsPage } from "./components/SettingsPage";
import { Onboarding } from "./components/Onboarding";
import { FilterIcon } from "./components/Icons";

const viewTitles: Record<ViewId, string> = {
  all: "全部笔记",
  local: "仅本地",
  synced: "已同步",
  attachments: "有附件",
  untagged: "无标签",
  stats: "记录统计",
  settings: "设置",
  trash: "回收站",
  tag: "标签",
};

const monthNames = ["一月", "二月", "三月", "四月", "五月", "六月", "七月", "八月", "九月", "十月", "十一月", "十二月"];

const viewSubtitles: Record<Exclude<ViewId, "stats" | "settings" | "trash" | "tag">, string> = {
  all: "先写下来，再慢慢整理。",
  local: "只留在本机、还没有归档的笔记。",
  synced: "已经进入 Notion 的笔记。",
  attachments: "带着图片与文件的笔记。",
  untagged: "还没有归类的想法。",
};


export default function App({ backend }: { backend?: Backend }) {
  const b = backend ?? (isWailsRuntime() ? wailsBackend : demoBackend);
  const settingsController = useMemo(() => new SettingsController(b), [b]);
  const controller = useMemo(
    () => new WorkspaceController({ backend: b, settings: settingsController }),
    [b, settingsController],
  );
  const state = useWorkspace(controller);
  const settingsState = useSyncExternalStore(
    settingsController.subscribe,
    settingsController.getSnapshot,
  );

  useEffect(() => {
    controller.start();
    return () => controller.dispose();
  }, [controller]);

  const {
    view,
    activeTag,
    from,
    to,
    memos,
    counts,
    dashboard,
    loading,
    selected,
    query,
    searching,
    results,
    error,
    toast,
    showFilters,
    emptyingTrash,
    online,
    sidebarOpen,
  } = state;
  const { settings, sync, tags } = settingsState;
  const queryText = query.trim();
  const dateLabel = from || to ? `${from || "…"} → ${to || "…"}` : "";
  const onboarding = Boolean(settings && !settings.onboarded);

  useEffect(() => {
    if (window.matchMedia("(max-width: 720px)").matches && sidebarOpen) {
      controller.setSidebarOpen(false);
    }
    // 只在首次挂载时处理移动端的默认抽屉状态。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const now = new Date();
  const topbarTitle = selected
    ? "编辑笔记"
    : queryText
      ? "搜索"
      : view === "tag"
        ? `#${activeTag}`
        : view === "all"
          ? `时间线 ${now.getFullYear()} · ${monthNames[now.getMonth()]}`
          : viewTitles[view];
  const month = from.slice(0, 7);

  const setMonth = (value: string) => {
    if (!value) return;
    if (month === value) {
      controller.clearDates();
      return;
    }
    controller.setView("all");
    const [year, monthNumber] = value.split("-").map(Number);
    const lastDay = new Date(year, monthNumber, 0).getDate();
    controller.setFrom(`${value}-01`);
    controller.setTo(`${value}-${String(lastDay).padStart(2, "0")}`);
  };

  const emptyTrash = async () => {
    if (!window.confirm("永久删除回收站中的全部笔记？本地内容和附件会被删除，此操作不可撤销。")) {
      return;
    }
    await controller.emptyTrash();
  };

  const startNew = () => {
    if (view === "settings" || view === "stats" || view === "trash") {
      controller.setView("all");
    }
    window.setTimeout(() => {
      const composer = document.querySelector<HTMLElement>(".composer");
      composer?.scrollIntoView({ block: "center", behavior: "smooth" });
      composer?.querySelector<HTMLElement>(".cm-content")?.focus();
    }, 80);
  };

  const subtitle = queryText
    ? `关键词「${queryText}」`
    : view === "trash"
      ? "删除的笔记会保留 30 天，到期后自动永久删除；期间可以恢复。"
      : view === "tag"
        ? "按标签回看这一组想法。"
        : view in viewSubtitles
          ? viewSubtitles[view as keyof typeof viewSubtitles]
          : "先写下来，再慢慢整理。";

  const headerActions = (
    <>
      {view === "trash" && (
        <button
          type="button"
          className="secondary-button small danger"
          onClick={() => void emptyTrash()}
          disabled={emptyingTrash || memos.length === 0}
        >
          {emptyingTrash ? "清空中…" : "清空回收站"}
        </button>
      )}
      <div className="filter-anchor">
        <button
          type="button"
          className={`text-button ${showFilters ? "is-active" : ""}`}
          onClick={controller.toggleFilters}
          aria-label="选择日期"
        >
          <FilterIcon size={13} /> {dateLabel || "日期"}
        </button>
        {showFilters && (
          <div className="filter-popover">
            <label>
              <span>开始日期</span>
              <input
                type="date"
                value={from}
                onChange={(e) => controller.setFrom(e.target.value)}
              />
            </label>
            <label>
              <span>结束日期</span>
              <input
                type="date"
                value={to}
                onChange={(e) => controller.setTo(e.target.value)}
              />
            </label>
            <div className="filter-actions">
              <button type="button" className="link-btn" onClick={controller.clearDates}>
                清除
              </button>
              <button type="button" className="secondary-button small" onClick={controller.toggleFilters}>
                完成
              </button>
            </div>
          </div>
        )}
      </div>
    </>
  );

  return (
    <div className="app-shell">
      {sidebarOpen && (
        <>
          <div className="sidebar-scrim" aria-hidden="true" onClick={controller.toggleSidebar} />
          <Sidebar
            counts={counts}
            activeView={view}
            activeTag={activeTag}
            heatmap={dashboard?.year ?? []}
            month={month}
            theme={(settings?.theme ?? "light") as "light" | "dark" | "system"}
            onNew={startNew}
            onView={controller.setView}
            onMonth={setMonth}
            onSettings={() => controller.setView("settings")}
            onTrash={() => controller.setView("trash")}
            onToggleTheme={() => void controller.toggleTheme()}
          />
        </>
      )}
      <div className="main-area">
        <Topbar
          query={query}
          onQuery={controller.setQuery}
          sync={sync && !online ? { ...sync, state: "offline" as const } : sync}
          title={topbarTitle}
          sidebarOpen={sidebarOpen}
          onToggleSidebar={controller.toggleSidebar}
          onToggleSync={() => void controller.toggleSync()}
          onNew={startNew}
        />

        {!online && (
          <div className="offline-banner">
            当前离线。笔记仍可本地记录与编辑，改动会在恢复连接后同步。
          </div>
        )}
        {sync?.enabled && !sync.paused && sync.state !== "paused" && (sync.state === "syncing" || sync.pending > 0) && (
          <div className="sync-progress">
            <span className="spinner" />
            <span>
              {sync.state === "syncing" ? "正在同步" : "等待同步"} · {sync.pending} 条待推送
              {sync.last_success ? ` · 最近成功 ${relativeTime(sync.last_success)}` : " · 首次同步中"}
            </span>
          </div>
        )}
        {sync?.state === "error" && sync.last_error && (
          <div className="error-banner" role="alert">
            同步错误：{sync.last_error}
            <button type="button" className="link-btn" onClick={() => void controller.syncNow()}>
              重试
            </button>
          </div>
        )}
        {error && (
          <div className="error-banner" role="alert">
            {error}
            <button type="button" className="link-btn" onClick={controller.clearError}>
              关闭
            </button>
          </div>
        )}

        <main className={`content ${view === "stats" || view === "settings" ? "is-wide" : ""} ${selected ? "is-reader" : ""}`}>
          {selected ? (
            <MemoSheet
              inline
              memo={selected}
              backend={b}
              submitOnEnter={settings?.submit_on_enter ?? true}
              onClose={controller.closeMemo}
              onChanged={controller.memoChanged}
              onError={controller.fail}
            />
          ) : view === "stats" ? (
            <StatsPage backend={b} onError={controller.fail} />
          ) : view === "settings" ? (
            <SettingsPage
              backend={b}
              controller={settingsController}
              onSettingsChanged={controller.refresh}
              onError={controller.fail}
            />
          ) : (
            <div className={`stream-wrap ${queryText ? "is-search" : ""}`}>
              {queryText ? (
                <ViewHeader
                  title="搜索结果"
                  subtitle={subtitle}
                  onRefresh={controller.refresh}
                  right={headerActions}
                />
              ) : (
                <JournalHeader
                  view={view}
                  title={view === "tag" ? `#${activeTag}` : viewTitles[view]}
                  subtitle={subtitle}
                  tags={tags}
                  counts={counts}
                  activeTag={activeTag}
                  activeView={view}
                  heatmap={dashboard?.year ?? []}
                  onView={controller.setView}
                  onTag={(name) => (activeTag === name ? controller.setView("all") : controller.setTag(name))}
                  onRefresh={controller.refresh}
                  right={headerActions}
                />
              )}

              {queryText ? (
                <SearchPanel
                  results={results}
                  query={queryText}
                  loading={searching}
                  onOpen={(result) => void controller.openMemo(result.memo)}
                />
              ) : (
                <>
                  {view !== "trash" && (
                    <Composer
                      backend={b}
                      onSaved={controller.refresh}
                      onError={controller.fail}
                      submitOnEnter={settings?.submit_on_enter ?? true}
                    />
                  )}
                  <Timeline
                    memos={memos}
                    backend={b}
                    loading={loading}
                    onOpen={(memo) => void controller.openMemo(memo)}
                    onChanged={controller.refresh}
                    onTag={controller.setTag}
                    onError={controller.fail}
                    emptyTitle={view === "trash" ? "回收站是空的" : undefined}
                    emptyHint={
                      view === "trash"
                        ? "删除的笔记会在这里保留 30 天，可以恢复；到期后自动永久删除。"
                        : undefined
                    }
                  />
                </>
              )}
            </div>
          )}
        </main>
      </div>

      {onboarding && settings && (
        <Onboarding backend={b} onDone={controller.refresh} onSkip={controller.refresh} />
      )}

      {toast && (
        <div className="toast" role="status" aria-live="polite">
          {toast}
        </div>
      )}
    </div>
  );
}
