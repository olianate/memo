import { useCallback, useEffect, useRef, useState } from "react";
import type { Backend } from "./backend";
import { isWailsRuntime, onAppEvent } from "./backend";
import { wailsBackend } from "./wailsBackend";
import { demoBackend } from "./demoBackend";
import type { Counts, Dashboard, Filter, Memo, SearchResult, Settings, SyncStatus, Tag, UpdateInfo, ViewId } from "./types";
import { currentMonth, startOfDayIso, endOfDayIso, relativeTime } from "./lib/format";
import { installGlobalErrorHandlers, reportError, setErrorSink } from "./lib/errorLog";
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

const emptyCounts: Counts = { notes: 0, tags: 0, days: 0 };
const sidebarPrefKey = "memo.sidebar.open";

function readSidebarOpen(): boolean {
  if (typeof window === "undefined") return true;
  try {
    return window.localStorage.getItem(sidebarPrefKey) !== "0";
  } catch {
    return true;
  }
}

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

function viewFilter(view: ViewId, tag: string, from: string, to: string): Filter {
  const filter: Filter = {};
  if (view === "local") filter.local_only = true;
  if (view === "synced") filter.synced = true;
  if (view === "attachments") filter.with_attachment = true;
  if (view === "untagged") filter.untagged = true;
  if (view === "trash") filter.deleted = "deleted";
  if (view === "tag" && tag) filter.tag = tag;
  if (from) filter.from = startOfDayIso(from);
  if (to) filter.to = endOfDayIso(to);
  return filter;
}

export default function App({ backend }: { backend?: Backend }) {
  const b = backend ?? (isWailsRuntime() ? wailsBackend : demoBackend);
  const [view, setView] = useState<ViewId>("all");
  const [activeTag, setActiveTag] = useState("");
  const [memos, setMemos] = useState<Memo[]>([]);
  const [tags, setTags] = useState<Tag[]>([]);
  const [counts, setCounts] = useState<Counts>(emptyCounts);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [sync, setSync] = useState<SyncStatus | null>(null);
  const [dashboard, setDashboard] = useState<Dashboard | null>(null);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<Memo | null>(null);
  const [query, setQuery] = useState("");
  const [searching, setSearching] = useState(false);
  const [results, setResults] = useState<SearchResult[]>([]);
  const [error, setError] = useState("");
  const [toast, setToast] = useState("");
  const [tick, setTick] = useState(0);
  const [showFilters, setShowFilters] = useState(false);
  const [emptyingTrash, setEmptyingTrash] = useState(false);
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [online, setOnline] = useState(typeof navigator === "undefined" ? true : navigator.onLine);
  const [sidebarOpen, setSidebarOpen] = useState(readSidebarOpen);
  const toastTimer = useRef<number | undefined>(undefined);
  const refreshTimer = useRef<number | undefined>(undefined);

  useEffect(() => {
    setErrorSink((report) => b.reportError(report));
    const removeHandlers = installGlobalErrorHandlers();
    return () => {
      removeHandlers();
      setErrorSink(null);
    };
  }, [b]);

  const scheduleRefresh = useCallback(() => {
    window.clearTimeout(refreshTimer.current);
    refreshTimer.current = window.setTimeout(() => setTick((n) => n + 1), 150);
  }, []);

  useEffect(() => () => window.clearTimeout(refreshTimer.current), []);

  const toggleSidebar = useCallback(() => {
    setSidebarOpen((open) => {
      const next = !open;
      try {
        window.localStorage.setItem(sidebarPrefKey, next ? "1" : "0");
      } catch {
        // The layout preference is optional; ignore storage failures.
      }
      return next;
    });
  }, []);

  const notify = useCallback((message: string) => {
    setToast(message);
    window.clearTimeout(toastTimer.current);
    toastTimer.current = window.setTimeout(() => setToast(""), 3200);
  }, []);

  const fail = useCallback((message: string) => {
    reportError(message, { kind: "ui.error_banner" }, "app");
    setError(message);
  }, []);

  const loadChrome = useCallback(async () => {
    const [nextCounts, nextTags, nextSettings, nextSync, nextDashboard] = await Promise.all([
      b.counts(),
      b.tags(),
      b.settings(),
      b.syncStatus(),
      b.dashboard(currentMonth()),
    ]);
    setCounts(nextCounts);
    setTags(nextTags);
    setSettings(nextSettings);
    setSync(nextSync);
    setDashboard(nextDashboard);
  }, [b]);

  const loadMemos = useCallback(async () => {
    if (view === "stats" || view === "settings") return;
    setLoading(true);
    try {
      const rows = await b.list(viewFilter(view, activeTag, from, to));
      setMemos(rows);
    } catch (e) {
      fail(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [activeTag, b, fail, from, to, view]);

  useEffect(() => {
    void loadChrome().catch((e) => fail(e instanceof Error ? e.message : String(e)));
  }, [loadChrome, fail, tick]);

  useEffect(() => {
    if (!settings) return;
    if (!settings.onboarded) return;
    void loadMemos();
  }, [loadMemos, settings?.onboarded, tick]);

  useEffect(() => {
    const text = query.trim();
    if (!text) {
      setResults([]);
      setSearching(false);
      return;
    }
    let alive = true;
    setSearching(true);
    const timer = window.setTimeout(() => {
      b.search(text, activeTag, from ? startOfDayIso(from) : "", to ? endOfDayIso(to) : "")
        .then((rows) => {
          if (alive) setResults(rows);
        })
        .catch((e) => {
          if (alive) fail(e instanceof Error ? e.message : String(e));
        })
        .finally(() => {
          if (alive) setSearching(false);
        });
    }, 220);
    return () => {
      alive = false;
      window.clearTimeout(timer);
    };
  }, [activeTag, b, fail, from, query, to]);

  useEffect(() => {
    const offs = [
      onAppEvent("memos:changed", scheduleRefresh),
      onAppEvent("tags:changed", scheduleRefresh),
      onAppEvent("sync:status", (data) => {
        if (data && typeof data === "object") setSync(data as SyncStatus);
        scheduleRefresh();
      }),
      onAppEvent("sync:error", (message) => fail(String(message))),
      onAppEvent("retention:evicted", scheduleRefresh),
      onAppEvent("restore:done", scheduleRefresh),
      onAppEvent("ui:navigate", (target) => {
        if (target === "settings") setView("settings");
      }),
      onAppEvent("update:available", (data) => {
        if (data && typeof data === "object") {
          const info = data as UpdateInfo;
          notify(`发现新版本 v${info.latest_version}，可在设置中安装`);
        }
      }),
    ];
    return () => offs.forEach((off) => off());
  }, [fail, notify, scheduleRefresh]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        const input = document.querySelector<HTMLInputElement>(".search-box input");
        input?.focus();
        input?.select();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    const update = () => setOnline(navigator.onLine);
    window.addEventListener("online", update);
    window.addEventListener("offline", update);
    return () => {
      window.removeEventListener("online", update);
      window.removeEventListener("offline", update);
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    const applyPendingView = () => {
      void b
        .takePendingView()
        .then((target) => {
          if (!cancelled && target === "settings") setView("settings");
        })
        .catch(() => {});
    };
    applyPendingView();
    window.addEventListener("focus", applyPendingView);
    return () => {
      cancelled = true;
      window.removeEventListener("focus", applyPendingView);
    };
  }, [b]);

  useEffect(() => {
    if (!settings) return;
    const apply = () => {
      const resolved =
        settings.theme === "system"
          ? window.matchMedia("(prefers-color-scheme: dark)").matches
            ? "dark"
            : "light"
          : settings.theme;
      document.documentElement.dataset.theme = resolved;
    };
    apply();
    if (settings.theme !== "system") return;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [settings]);

  useEffect(() => {
    if (!isWailsRuntime()) return;
    void b.setWindowActive(document.hasFocus()).catch(() => {});
    const focus = () => void b.setWindowActive(true).catch(() => {});
    const blur = () => void b.setWindowActive(false).catch(() => {});
    window.addEventListener("focus", focus);
    window.addEventListener("blur", blur);
    return () => {
      window.removeEventListener("focus", focus);
      window.removeEventListener("blur", blur);
    };
  }, [b]);

  const refresh = () => setTick((n) => n + 1);

  const emptyTrash = async () => {
    if (!window.confirm("永久删除回收站中的全部笔记？本地内容和附件会被删除，此操作不可撤销。")) return;
    setEmptyingTrash(true);
    try {
      const count = await b.emptyTrash();
      notify(count > 0 ? `已永久删除 ${count} 条笔记` : "回收站已经是空的");
      refresh();
    } catch (e) {
      fail(e instanceof Error ? e.message : String(e));
      refresh();
    } finally {
      setEmptyingTrash(false);
    }
  };

  const openMemo = useCallback(
    async (memo: Memo) => {
      setSelected(memo);
      if (memo.local_state !== "evicted") return;
      // Selecting an evicted memo hydrates it from Notion. If that fails (for
      // example offline), the sheet still opens with a retry placeholder.
      try {
        const fresh = await b.get(memo.id);
        setSelected(fresh);
        scheduleRefresh();
      } catch {
        /* keep the placeholder */
      }
    },
    [b, scheduleRefresh],
  );

  useEffect(() => {
    // WebView2 can restore the previous scroll offset; a memo app should always
    // open at the newest entry and the composer.
    document.querySelector<HTMLElement>(".content")?.scrollTo?.({ top: 0 });
  }, []);

  const onView = (next: ViewId) => {
    setView(next);
    setActiveTag("");
    setQuery("");
    setShowFilters(false);
  };

  const onTag = (name: string) => {
    setActiveTag(name);
    setView("tag");
    setQuery("");
  };

  const onSyncNow = async () => {
    if (!sync?.configured || !sync.enabled) {
      setView("settings");
      notify("请先在设置中连接 Notion 并启用同步");
      return;
    }
    try {
      await b.syncNow();
      notify("同步完成");
      refresh();
    } catch (e) {
      fail(e instanceof Error ? e.message : String(e));
    }
  };

  const toggleSync = async () => {
    if (!sync?.configured) {
      setView("settings");
      notify("请先在设置中连接 Notion");
      return;
    }
    try {
      if (!sync.enabled) {
        await b.setSyncEnabled(true);
        if (sync.paused) await b.setSyncPaused(false);
        notify("同步已开启");
      } else if (sync.paused) {
        await b.setSyncPaused(false);
        notify("同步已恢复");
      } else {
        await b.setSyncPaused(true);
        notify("同步已暂停");
      }
      const fresh = await b.syncStatus();
      setSync(fresh);
      refresh();
    } catch (e) {
      fail(e instanceof Error ? e.message : String(e));
    }
  };

  const toggleTheme = async () => {
    const resolved =
      settings?.theme === "system"
        ? window.matchMedia("(prefers-color-scheme: dark)").matches
          ? "dark"
          : "light"
        : settings?.theme ?? "light";
    const next = resolved === "dark" ? "light" : "dark";
    try {
      await b.setTheme(next);
      const fresh = await b.settings();
      setSettings(fresh);
    } catch (e) {
      fail(e instanceof Error ? e.message : String(e));
    }
  };

  const selectedTagLabel = activeTag;
  const title =
    view === "tag" ? `#${selectedTagLabel}` : viewTitles[view];
  const dateLabel = from || to ? `${from || "…"} → ${to || "…"}` : "";
  const onboarding = Boolean(settings && !settings.onboarded);

  const sidebar = (
    <Sidebar
      counts={counts}
      tags={tags}
      activeView={view}
      activeTag={activeTag}
      heatmap={dashboard?.year ?? []}
      onView={onView}
      onTag={onTag}
      onSettings={() => setView("settings")}
      onTrash={() => setView("trash")}
    />
  );

  return (
    <div className="app-shell">
      {sidebarOpen && sidebar}
      <div className="main-area">
        <Topbar
          query={query}
          onQuery={setQuery}
          sync={sync && !online ? { ...sync, state: "offline" } : sync}
          theme={(settings?.theme ?? "light") as "light" | "dark" | "system"}
          sidebarOpen={sidebarOpen}
          onToggleSidebar={toggleSidebar}
          onToggleTheme={() => void toggleTheme()}
          onToggleSync={() => void toggleSync()}
        />

        {!online && <div className="offline-banner">当前离线。笔记仍可本地记录与编辑，改动会在恢复连接后同步。</div>}
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
            <button type="button" className="link-btn" onClick={() => void onSyncNow()}>
              重试
            </button>
          </div>
        )}
        {error && (
          <div className="error-banner" role="alert">
            {error}
            <button type="button" className="link-btn" onClick={() => setError("")}>
              关闭
            </button>
          </div>
        )}

        <main className={`content ${view === "stats" || view === "settings" ? "is-wide" : ""}`}>
          {view === "stats" ? (
            <StatsPage backend={b} onError={fail} />
          ) : view === "settings" ? (
            <SettingsPage
              backend={b}
              onSettingsChanged={refresh}
              onError={fail}
            />
          ) : (
            <div className="stream-wrap">
              <ViewHeader
                title={query.trim() ? "搜索结果" : title}
                subtitle={
                  query.trim()
                    ? `关键词「${query.trim()}」`
                    : view === "trash"
                      ? "软删除的笔记仍保留在本地，恢复后会重新进入同步队列。"
                      : "先写下来，再慢慢整理。"
                }
                onRefresh={refresh}
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
                        onClick={() => setShowFilters((v) => !v)}
                      >
                        <FilterIcon size={15} /> {dateLabel || "日期"}
                      </button>
                      {showFilters && (
                        <div className="filter-popover">
                          <label>
                            <span>开始日期</span>
                            <input type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
                          </label>
                          <label>
                            <span>结束日期</span>
                            <input type="date" value={to} onChange={(e) => setTo(e.target.value)} />
                          </label>
                          <div className="filter-actions">
                            <button
                              type="button"
                              className="link-btn"
                              onClick={() => {
                                setFrom("");
                                setTo("");
                              }}
                            >
                              清除
                            </button>
                            <button type="button" className="secondary-button small" onClick={() => setShowFilters(false)}>
                              完成
                            </button>
                          </div>
                        </div>
                      )}
                    </div>
                  </>
                }
              />

              {query.trim() ? (
                <SearchPanel
                  results={results}
                  query={query.trim()}
                  loading={searching}
                  onOpen={(result) => void openMemo(result.memo)}
                />
              ) : (
                <>
                  {view !== "trash" && (
                    <Composer backend={b} onSaved={refresh} onError={fail} submitOnEnter={settings?.submit_on_enter ?? true} />
                  )}
                  <Timeline
                    memos={memos}
                    backend={b}
                    loading={loading}
                    onOpen={(memo) => void openMemo(memo)}
                    onChanged={refresh}
                    onTag={onTag}
                    onError={fail}
                    emptyTitle={view === "trash" ? "回收站是空的" : undefined}
                    emptyHint={
                      view === "trash"
                        ? "删除的笔记会出现在这里，可以恢复或永久删除。"
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
          onClose={() => setSelected(null)}
          onChanged={(memo) => {
            if (memo) setSelected(memo);
            else setSelected(null);
            refresh();
          }}
          onError={fail}
        />
      )}

      {onboarding && settings && (
        <Onboarding
          backend={b}
          onDone={() => {
            refresh();
          }}
          onSkip={() => {
            refresh();
          }}
        />
      )}

      {toast && <div className="toast">{toast}</div>}
    </div>
  );
}
