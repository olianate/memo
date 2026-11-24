import { useEffect, useState } from "react";
import type { BackupResult, RestoreProgress, Settings, SyncLog, SyncStatus, Tag, UpdateInfo, UpdateProgress } from "../types";
import type { Backend } from "../backend";
import { onAppEvent } from "../backend";
import { humanBytes, relativeTime } from "../lib/format";
import { Onboarding } from "./Onboarding";
import { CheckIcon, DownloadIcon, RefreshIcon } from "./Icons";

function Card({
  title,
  hint,
  children,
  className,
}: {
  title: string;
  hint?: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <section className={`settings-card${className ? ` ${className}` : ""}`}>
      <div className="settings-card-head">
        <h3>{title}</h3>
        {hint && <p>{hint}</p>}
      </div>
      {children}
    </section>
  );
}

function Toggle({
  checked,
  onChange,
  label,
  disabled,
}: {
  checked: boolean;
  onChange: (value: boolean) => void;
  label: string;
  disabled?: boolean;
}) {
  return (
    <label className={`toggle-row ${disabled ? "is-disabled" : ""}`}>
      <span>{label}</span>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={label}
        className={`switch ${checked ? "is-on" : ""}`}
        disabled={disabled}
        onClick={() => onChange(!checked)}
      >
        <i />
      </button>
    </label>
  );
}

export function SettingsPage({
  backend,
  onSettingsChanged,
  onError,
}: {
  backend: Backend;
  onSettingsChanged: () => void;
  onError: (message: string) => void;
}) {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [sync, setSync] = useState<SyncStatus | null>(null);
  const [tags, setTags] = useState<Tag[]>([]);
  const [errors, setErrors] = useState<SyncLog[]>([]);
  const [hotkey, setHotkey] = useState("");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [backup, setBackup] = useState<BackupResult | null>(null);
  const [logsPath, setLogsPath] = useState("");
  const [restore, setRestore] = useState<RestoreProgress | null>(null);
  const [update, setUpdate] = useState<UpdateInfo | null>(null);
  const [updateProgress, setUpdateProgress] = useState<UpdateProgress | null>(null);
  const [updateBusy, setUpdateBusy] = useState(false);
  const [updateError, setUpdateError] = useState("");
  const [mcpPort, setMcpPort] = useState("8765");

  const reload = async () => {
    const [s, st, nextTags, nextErrors] = await Promise.all([
      backend.settings(),
      backend.syncStatus(),
      backend.tags(),
      backend.errorLogs().catch(() => [] as SyncLog[]),
    ]);
    setSettings(s);
    setSync(st);
    setTags(nextTags);
    setErrors(nextErrors);
    setHotkey(s.hotkey);
    setMcpPort(String(s.mcp_port));
  };

  useEffect(() => {
    void reload().catch((e) => onError(e instanceof Error ? e.message : String(e)));
    void checkUpdates(true);
    const offRestore = onAppEvent("restore:progress", (progress) => setRestore(progress as RestoreProgress));
    const offAvailable = onAppEvent("update:available", (info) => {
      if (info && typeof info === "object") setUpdate(info as UpdateInfo);
    });
    const offProgress = onAppEvent("update:progress", (progress) => {
      setUpdateProgress(progress as UpdateProgress);
      setUpdateBusy(true);
    });
    return () => {
      offRestore();
      offAvailable();
      offProgress();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const checkUpdates = async (silent = false) => {
    setUpdateBusy(true);
    if (!silent) {
      setUpdateError("");
      setMessage("");
    }
    try {
      const info = await backend.checkForUpdates();
      setUpdate(info);
      setUpdateError("");
      if (!silent) setMessage(info.available ? `发现新版本 v${info.latest_version}` : "当前已是最新版本");
    } catch (e) {
      if (!silent) setUpdateError(e instanceof Error ? e.message : String(e));
    } finally {
      setUpdateBusy(false);
    }
  };

  const installUpdate = async () => {
    setUpdateBusy(true);
    setUpdateError("");
    setMessage("");
    setUpdateProgress({ phase: "preparing", percent: 0, message: "正在准备下载更新…" });
    let started = false;
    try {
      await backend.installUpdate();
      started = true;
      setMessage("安装程序已启动，memo 即将重启");
    } catch (e) {
      setUpdateProgress(null);
      const text = e instanceof Error ? e.message : String(e);
      setUpdateError(text);
      onError(text);
    } finally {
      if (!started) setUpdateBusy(false);
    }
  };

  const openReleasePage = async () => {
    try {
      await backend.openReleasePage();
    } catch (e) {
      const text = e instanceof Error ? e.message : String(e);
      setUpdateError(text);
      onError(text);
    }
  };

  const run = async (fn: () => Promise<void>, success?: string) => {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      await fn();
      if (success) setMessage(success);
      await reload();
      onSettingsChanged();
    } catch (e) {
      const text = e instanceof Error ? e.message : String(e);
      setError(text);
      onError(text);
    } finally {
      setBusy(false);
    }
  };

  if (!settings) {
    return (
      <div className="timeline-loading">
        <span className="spinner" /> 正在加载设置…
      </div>
    );
  }

  return (
    <div className="settings-page">
      <div className="settings-head">
        <div>
          <h2>设置</h2>
          <p>本地优先。Notion 只是可选的归档与同步通道。</p>
        </div>
        {message && <span className="save-note"><CheckIcon size={14} /> {message}</span>}
      </div>
      {error && <div className="inline-error">{error}</div>}

      <div className="settings-grid">
        <Card title="外观与启动" hint="主题会立即应用；快捷键注册失败时会显示提示。">
          <div className="settings-row">
            <span>主题</span>
            <div className="segmented">
              {(["light", "dark", "system"] as const).map((theme) => (
                <button
                  key={theme}
                  type="button"
                  className={settings.theme === theme ? "is-active" : ""}
                  onClick={() => void run(() => backend.setTheme(theme), "主题已更新")}
                >
                  {theme === "light" ? "浅色" : theme === "dark" ? "深色" : "跟随系统"}
                </button>
              ))}
            </div>
          </div>
          <div className="settings-row">
            <span>呼出快捷键</span>
            <div className="inline-form">
              <input value={hotkey} onChange={(e) => setHotkey(e.target.value)} spellCheck={false} aria-label="呼出快捷键" />
              <button type="button" className="secondary-button" disabled={busy} onClick={() => void run(() => backend.setHotkey(hotkey), "快捷键已保存")}>
                保存
              </button>
            </div>
          </div>
          {settings.hotkey_error && <div className="inline-error">{settings.hotkey_error}</div>}
          <div className="settings-row">
            <span>编辑保存键</span>
            <div className="segmented">
              <button
                type="button"
                className={settings.submit_on_enter ? "is-active" : ""}
                onClick={() => void run(() => backend.setSubmitOnEnter(true), "编辑快捷键已更新")}
              >
                Enter 保存
              </button>
              <button
                type="button"
                className={!settings.submit_on_enter ? "is-active" : ""}
                onClick={() => void run(() => backend.setSubmitOnEnter(false), "编辑快捷键已更新")}
              >
                Shift+Enter 保存
              </button>
            </div>
          </div>
          <p className="fine-print">
            {settings.submit_on_enter
              ? "Enter 保存，Shift+Enter 换行；快速记录和编辑面板遵循同一设置。"
              : "Enter 换行，Shift+Enter 保存；快速记录和编辑面板遵循同一设置。"}
          </p>
          <Toggle
            label="开机时启动 memo"
            checked={settings.start_at_login}
            onChange={(value) => void run(() => backend.setStartAtLogin(value), "启动设置已更新")}
          />
        </Card>

        <Card title="本地数据" hint="SQLite 与附件都在这个目录。备份会复制一份数据库和附件。">
          <div className="settings-row">
            <span>数据目录</span>
            <code className="path">{settings.data_dir}</code>
          </div>
          <div className="button-row">
            <button type="button" className="secondary-button" onClick={() => void backend.openDataDir().catch((e) => onError(String(e)))}>
              打开目录
            </button>
            <button
              type="button"
              className="secondary-button"
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  setBackup(await backend.exportBackup());
                }, "备份完成")
              }
            >
              <DownloadIcon size={15} /> 导出备份
            </button>
            <button
              type="button"
              className="secondary-button"
              disabled={busy}
              onClick={() => void run(() => backend.rebuildIndex(), "搜索索引已重建")}
            >
              <RefreshIcon size={15} /> 重建搜索索引
            </button>
            <button
              type="button"
              className="secondary-button danger"
              disabled={busy}
              onClick={() => {
                const confirmed = window.confirm(
                  "这会删除本机全部笔记、标签与附件，并断开 Notion、清除系统钥匙串中的令牌。Notion 归档本身不会被删除。确定继续？",
                );
                if (!confirmed) return;
                void run(() => backend.deleteLocalData(), "本地数据已删除，Notion 已断开");
              }}
            >
              删除全部本地数据
            </button>
          </div>
          {backup && (
            <div className="result-line">
              备份：<code>{backup.path}</code>
            </div>
          )}
          <p className="fine-print">本地 SQLite 不加密。需要静态加密时请使用操作系统全盘加密；搜索索引包含分词后的正文。</p>
        </Card>

        <Card title="保留策略" hint="只在同步已确认、且没有待推送改动时清理正文与附件。">
          <div className="settings-row">
            <span>保留期限</span>
            <select
              value={settings.retention_days}
              onChange={(e) => void run(() => backend.setRetention(Number(e.target.value), settings.immediate_eviction), "保留策略已更新")}
            >
              <option value={7}>7 天</option>
              <option value={30}>30 天</option>
              <option value={90}>90 天</option>
              <option value={-1}>永不清理</option>
            </select>
          </div>
          <Toggle
            label="同步完成后立即清理"
            checked={settings.immediate_eviction}
            onChange={(value) => void run(() => backend.setRetention(settings.retention_days, value), "保留策略已更新")}
          />
          <p className="fine-print">清理后正文和附件不再占本地磁盘，但 body_text、标签和搜索索引保留；打开时会从 Notion 恢复。置顶「保留本地」的笔记永远不清理。</p>
        </Card>

        <Card title="标签" hint="v1 标签是扁平的。重命名或合并只改元数据，不改写正文，也不增加版本号。">
          {tags.length === 0 && <div className="muted">还没有标签。在正文里输入 #标签 即可创建。</div>}
          <div className="tag-manage-list">
            {tags.map((tag) => (
              <div className="tag-manage-row" key={tag.id}>
                <span className="tag-manage-name">#{tag.name}</span>
                <em>{tag.count}</em>
                <button
                  type="button"
                  className="link-btn"
                  onClick={() => {
                    const name = window.prompt(`重命名 #${tag.name}`, tag.name);
                    if (!name || name.trim() === tag.name) return;
                    void run(() => backend.renameTag(tag.id, name).then(() => undefined), "标签已重命名");
                  }}
                >
                  重命名
                </button>
                <select
                  value=""
                  aria-label={`把 #${tag.name} 合并到`}
                  onChange={(e) => {
                    const target = e.target.value;
                    if (!target) return;
                    void run(() => backend.mergeTags(tag.id, target), "标签已合并");
                  }}
                >
                  <option value="">合并到…</option>
                  {tags
                    .filter((other) => other.id !== tag.id)
                    .map((other) => (
                      <option value={other.id} key={other.id}>
                        #{other.name}
                      </option>
                    ))}
                </select>
                <button
                  type="button"
                  className="icon-button danger"
                  aria-label={`删除标签 ${tag.name}`}
                  onClick={() => {
                    if (!window.confirm(`从所有笔记中移除 #${tag.name}？`)) return;
                    void run(() => backend.deleteTag(tag.id), "标签已删除");
                  }}
                >
                  ×
                </button>
              </div>
            ))}
          </div>
        </Card>

        <Card title="Notion 归档" hint="可选。断开连接不会删除本地笔记。">
          {settings.notion_connected ? (
            <>
              <div className="connection-line">
                <span className="connection-dot" />
                <div>
                  <strong>{settings.workspace_name}</strong>
                  <em>{sync?.last_success ? `最近成功 ${relativeTime(sync.last_success)}` : "尚未完成首次同步"}</em>
                </div>
                <span className="mini-badge">已连接</span>
              </div>
              <Toggle
                label="启用 Notion 同步"
                checked={settings.sync_enabled}
                onChange={(value) => void run(() => backend.setSyncEnabled(value), value ? "同步已开启" : "同步已关闭")}
              />
              <Toggle
                label="暂停同步"
                checked={settings.sync_paused}
                onChange={(value) => void run(() => backend.setSyncPaused(value), value ? "同步已暂停" : "同步已恢复")}
              />
              <div className="button-row">
                <button type="button" className="secondary-button" disabled={busy} onClick={() => void run(() => backend.syncNow(), "同步完成")}>
                  立即同步
                </button>
                <button
                  type="button"
                  className="secondary-button"
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      setRestore(null);
                      setRestore(await backend.restoreAll());
                    }, "恢复完成")
                  }
                >
                  {busy ? "恢复中…" : "从 Notion 恢复全部"}
                </button>
                <button
                  type="button"
                  className="secondary-button danger"
                  disabled={busy}
                  onClick={() => void run(() => backend.disconnect(), "已断开 Notion")}
                >
                  断开连接
                </button>
              </div>
              {restore && (
                <div className="result-line">
                  恢复：{restore.completed} / {restore.total} 成功{restore.failed > 0 ? `，${restore.failed} 失败` : ""}
                </div>
              )}
              {sync && sync.pending > 0 && (
                <div className="result-line">{sync.pending} 条等待同步{sync.retrying > 0 ? `，${sync.retrying} 条正在重试` : ""}</div>
              )}
              {sync?.last_error && <div className="inline-error">{sync.last_error}</div>}
              <div className="sync-logs">
                <h4>最近同步事件</h4>
                {(sync?.logs ?? []).slice(0, 5).map((log) => (
                  <div className={`sync-log is-${log.level}`} key={log.id}>
                    <span>{relativeTime(log.at)}</span>
                    <em>{log.event}</em>
                    <p>{log.message}</p>
                  </div>
                ))}
                {(sync?.logs ?? []).length === 0 && <div className="muted">还没有同步记录。</div>}
              </div>
            </>
          ) : (
            <>
              <p className="lead-small">连接后，应用会在你选择的页面下创建自己的数据库。旧正文按保留策略清理，搜索仍留在本地。</p>
              <button type="button" className="primary-button" onClick={() => setConnecting(true)}>
                连接 Notion
              </button>
            </>
          )}
        </Card>

        <Card title="MCP 服务" hint="让本地 AI 客户端通过 Model Context Protocol 搜索、创建、修改和删除记录。">
          <Toggle
            label="启用 MCP 服务"
            checked={settings.mcp_enabled}
            onChange={(value) => void run(() => backend.setMCP(value, Number(mcpPort) || settings.mcp_port), value ? "MCP 服务已启用" : "MCP 服务已关闭")}
          />
          <div className="settings-row">
            <span>监听端口</span>
            <div className="inline-form">
              <input
                type="number"
                min={1024}
                max={65535}
                value={mcpPort}
                onChange={(e) => setMcpPort(e.target.value)}
                aria-label="MCP 端口"
              />
              <button
                type="button"
                className="secondary-button"
                disabled={busy}
                onClick={() => void run(() => backend.setMCP(settings.mcp_enabled, Number(mcpPort)), "MCP 端口已保存")}
              >
                保存
              </button>
            </div>
          </div>
          {settings.mcp_running ? (
            <div className="result-line">
              MCP 端点：<code>{settings.mcp_endpoint}</code>
            </div>
          ) : (
            <div className="muted">服务未运行。启用后客户端使用 Streamable HTTP 连接。</div>
          )}
          {settings.mcp_error && <div className="inline-error">{settings.mcp_error}</div>}
          <p className="fine-print">仅监听 127.0.0.1，不对局域网开放；没有额外鉴权，本机进程均可访问。端点路径为 /mcp，提供 search_memos、create_memo、update_memo、delete_memo 工具。</p>
        </Card>

        <Card title="错误日志" hint="同步事件与界面错误共用本地日志；导出时会自动隐藏令牌。">
          <div className="button-row">
            <button type="button" className="secondary-button" disabled={busy} onClick={() => void reload().catch((e) => onError(e instanceof Error ? e.message : String(e)))}>
              <RefreshIcon size={15} /> 刷新日志
            </button>
            <button
              type="button"
              className="secondary-button danger"
              disabled={busy || (errors.length === 0 && (sync?.logs ?? []).length === 0)}
              onClick={() => {
                if (!window.confirm("清空本地日志？同步事件和错误记录都会被删除，此操作不可撤销。")) return;
                void run(() => backend.clearLogs(), "日志已清空");
              }}
            >
              清空日志
            </button>
            <button
              type="button"
              className="secondary-button"
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  setLogsPath(await backend.exportLogs());
                }, "错误日志已导出")
              }
            >
              <DownloadIcon size={15} /> 导出错误日志
            </button>
            <button
              type="button"
              className="secondary-button"
              disabled={errors.length === 0}
              onClick={() => {
                const text = errors
                  .map((log) => `[${new Date(log.at).toISOString()}] ${log.level} ${log.event}\n${log.message}`)
                  .join("\n\n");
                void navigator.clipboard
                  .writeText(text)
                  .then(() => setMessage("错误日志已复制"))
                  .catch((e) => {
                    const detail = e instanceof Error ? e.message : String(e);
                    setError(detail);
                    onError(detail);
                  });
              }}
            >
              复制日志
            </button>
          </div>
          {errors.length === 0 ? (
            <div className="muted">还没有错误记录。</div>
          ) : (
            <div className="error-log-list">
              {errors.map((log) => (
                <details className="error-log-row" key={log.id}>
                  <summary>
                    <span>{relativeTime(log.at)}</span>
                    <em>{log.event}</em>
                    <p>{log.message.split("\n")[0]}</p>
                  </summary>
                  <pre>{log.message}</pre>
                </details>
              ))}
            </div>
          )}
          {logsPath && (
            <div className="result-line">
              日志：<code>{logsPath}</code>
            </div>
          )}
        </Card>

        <Card className="settings-card-wide" title="关于" hint="memo v1 · personal desktop app">
          <div className="update-summary">
            <div className="update-version">
              <span>当前版本</span>
              <strong>v{settings.version}</strong>
              <em>
                {update
                  ? `上次检查：${relativeTime(update.checked_at)}`
                  : updateBusy
                    ? "正在检查更新…"
                    : "尚未检查"}
              </em>
            </div>
            <button
              type="button"
              className="secondary-button"
              disabled={updateBusy}
              onClick={() => void checkUpdates()}
            >
              <RefreshIcon size={15} /> {updateBusy && !updateProgress ? "检查中…" : "检查更新"}
            </button>
          </div>

          {updateError && <div className="inline-error">{updateError}</div>}

          {update?.available ? (
            <div className="update-available">
              <div className="result-line">
                发现新版本 <strong>v{update.latest_version}</strong>
                {update.published_at ? ` · ${relativeTime(update.published_at)}发布` : ""}
                {update.asset_size > 0 ? ` · ${humanBytes(update.asset_size)}` : ""}
              </div>
              {update.notes && <pre className="update-notes">{update.notes}</pre>}
              <div className="button-row">
                {update.can_install ? (
                  <button type="button" className="primary-button" disabled={updateBusy} onClick={() => void installUpdate()}>
                    <DownloadIcon size={15} /> {updateBusy ? "正在更新…" : `安装 v${update.latest_version} 并重启`}
                  </button>
                ) : (
                  <span className="fine-print">当前平台暂不支持自动安装，请从发布页手动下载。</span>
                )}
                <button type="button" className="secondary-button" disabled={updateBusy} onClick={() => void openReleasePage()}>
                  打开发布页
                </button>
              </div>
            </div>
          ) : update ? (
            <div className="result-line">当前已是最新版本。</div>
          ) : (
            <div className="muted">检查是否有新版本可用。</div>
          )}

          {updateProgress && (
            <div className="update-progress">
              <div className="progress">
                <i style={{ width: `${Math.max(0, Math.min(100, updateProgress.percent))}%` }} />
              </div>
              <div className="update-progress-line">
                <span>{updateProgress.message}</span>
                <em>{Math.round(updateProgress.percent)}%</em>
              </div>
            </div>
          )}

          <div className="about-grid">
            <div><span>存储</span><strong>SQLite (modernc)</strong></div>
            <div><span>搜索</span><strong>FTS5 trigram + LIKE</strong></div>
            <div><span>文件上限</span><strong>{settings.workspace_file_limit ? `${Math.round(settings.workspace_file_limit / 1024 / 1024)} MB` : "未连接"}</strong></div>
          </div>
          <p className="fine-print">Notion 无法通过 API 永久删除或撤销已上传的文件；需要真正删除时请使用本地-only 模式并在本地删除。</p>
        </Card>
      </div>

      {connecting && (
        <Onboarding
          backend={backend}
          connectOnly
          onDone={() => {
            setConnecting(false);
            void run(async () => undefined, "Notion 已连接");
          }}
          onSkip={() => setConnecting(false)}
        />
      )}
    </div>
  );
}
