import { useEffect, useState } from "react";
import type { BackupResult, RestoreProgress, Settings, SyncStatus, Tag } from "../types";
import type { Backend } from "../backend";
import { onAppEvent } from "../backend";
import { relativeTime } from "../lib/format";
import { Onboarding } from "./Onboarding";
import { CheckIcon, DownloadIcon, RefreshIcon } from "./Icons";

function Card({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section className="settings-card">
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
  const [hotkey, setHotkey] = useState("");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [backup, setBackup] = useState<BackupResult | null>(null);
  const [logsPath, setLogsPath] = useState("");
  const [restore, setRestore] = useState<RestoreProgress | null>(null);

  const reload = async () => {
    const [s, st, nextTags] = await Promise.all([backend.settings(), backend.syncStatus(), backend.tags()]);
    setSettings(s);
    setSync(st);
    setTags(nextTags);
    setHotkey(s.hotkey);
  };

  useEffect(() => {
    void reload().catch((e) => onError(e instanceof Error ? e.message : String(e)));
    const off = onAppEvent("restore:progress", (progress) => setRestore(progress as RestoreProgress));
    return off;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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
              onClick={() =>
                void run(async () => {
                  setLogsPath(await backend.exportLogs());
                }, "日志已导出")
              }
            >
              导出日志
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
          {logsPath && (
            <div className="result-line">
              日志：<code>{logsPath}</code>
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
                {(sync?.logs ?? []).slice(0, 8).map((log) => (
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

        <Card title="关于" hint="memo v1 · personal, local-first desktop app">
          <div className="about-grid">
            <div><span>版本</span><strong>{settings.version}</strong></div>
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
