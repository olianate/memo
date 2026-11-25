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
import { Topbar, ViewHeader } from "./components/Header";
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
  local: "本地 only",
  synced: "已同步",
  attachments: "有附件",
  untagged: "无标签",
  stats: "记录统计",
  settings: "设置",
  trash: "回收站",
  tag: "标签",
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

  const emptyTrash = async () => {
    if (!window.confirm("永久删除回收站中的全部笔记？本地内容和附件会被删除，此操作不可撤销。")) {
      return;
    }
    await controller.emptyTrash();
  };

  return (
    <div className="app-shell">
      {sidebarOpen && (
        <Sidebar
          counts={counts}
          tags={tags}
          activeView={view}
          activeTag={activeTag}
          heatmap={dashboard?.year ?? []}
          onView={controller.setView}
          onTag={controller.setTag}
          onSettings={() => controller.setView("settings")}
          onTrash={() => controller.setView("trash")}
        />
      )}
      <div className="main-area">
        <Topbar
          query={query}
          onQuery={controller.setQuery}
          sync={sync && !online ? { ...sync, state: "offline" as const } : sync}
          theme={(settings?.theme ?? "light") as "light" | "dark" | "system"}
          sidebarOpen={sidebarOpen}
          onToggleSidebar={controller.toggleSidebar}
          onToggleTheme={() => void controller.toggleTheme()}
          onToggleSync={() => void controller.toggleSync()}
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

        <main className={`content ${view === "stats" || view === "settings" ? "is-wide" : ""}`}>
          {view === "stats" ? (
            <StatsPage backend={b} onError={controller.fail} />
          ) : view === "settings" ? (
            <SettingsPage
              backend={b}
              controller={settingsController}
              onSettingsChanged={controller.refresh}
              onError={controller.fail}
            />
          ) : (
            <div className="stream-wrap">
              <ViewHeader
                title={queryText ? "搜索结果" : view === "tag" ? `#${activeTag}` : viewTitles[view]}
                subtitle={
                  queryText
                    ? `关键词「${queryText}」`
                    : view === "trash"
                      ? "删除的笔记会保留 30 天，到期后自动永久删除；期间可以恢复。"
                      : "先写下来，再慢慢整理。"
                }
                onRefresh={controller.refresh}
                right={
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
                        className={`secondary-button small ${showFilters ? "is-active" : ""}`}
                        onClick={controller.toggleFilters}
                      >
                        <FilterIcon size={15} /> {dateLabel || "日期"}
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
                            <button
                              type="button"
                              className="link-btn"
                              onClick={controller.clearDates}
                            >
                              清除
                            </button>
                            <button
                              type="button"
                              className="secondary-button small"
                              onClick={controller.toggleFilters}
                            >
                              完成
                            </button>
                          </div>
                        </div>
                      )}
                    </div>
                  </>
                }
              />

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

      {selected && (
        <MemoSheet
          memo={selected}
          backend={b}
          submitOnEnter={settings?.submit_on_enter ?? true}
          onClose={controller.closeMemo}
          onChanged={controller.memoChanged}
          onError={controller.fail}
        />
      )}

      {onboarding && settings && (
        <Onboarding backend={b} onDone={controller.refresh} onSkip={controller.refresh} />
      )}

      {toast && <div className="toast">{toast}</div>}
    </div>
  );
}
