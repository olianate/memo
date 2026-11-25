import { useEffect, useId, useState } from "react";
import type {
  ArchiveDatabase,
  ArchiveDatabasePreview,
  ConnectResult,
  ParentPage,
  Workspace,
} from "../types";
import type { OnboardingBackend } from "../backend";
import { reportError } from "../lib/errorLog";
import { CheckIcon, EyeIcon, EyeOffIcon, SearchIcon } from "./Icons";

type Step = "welcome" | "token" | "parent" | "done";

type Selection =
  | { kind: "database"; value: ArchiveDatabase }
  | { kind: "page"; value: ParentPage };

export function Onboarding({
  backend,
  onDone,
  onSkip,
  connectOnly = false,
}: {
  backend: OnboardingBackend;
  onDone: () => void;
  onSkip: () => void;
  connectOnly?: boolean;
}) {
  const [step, setStep] = useState<Step>(connectOnly ? "token" : "welcome");
  const [token, setToken] = useState("");
  const [showToken, setShowToken] = useState(false);
  const tokenInputID = useId();
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [databases, setDatabases] = useState<ArchiveDatabase[]>([]);
  const [pages, setPages] = useState<ParentPage[]>([]);
  const [query, setQuery] = useState("");
  const [manualTarget, setManualTarget] = useState("");
  const [selection, setSelection] = useState<Selection | null>(null);
  const [databaseName, setDatabaseName] = useState("memo");
  const [preview, setPreview] = useState<ArchiveDatabasePreview | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewError, setPreviewError] = useState("");
  const [schemaConfirmed, setSchemaConfirmed] = useState(false);
  const [result, setResult] = useState<ConnectResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [resolving, setResolving] = useState(false);
  const [error, setError] = useState("");
  const [hotkey, setHotkey] = useState("CmdOrCtrl+Shift+Space");
  const [startAtLogin, setStartAtLogin] = useState(false);

  const selectedDatabase = selection?.kind === "database" ? selection.value : null;
  const selectedPage = selection?.kind === "page" ? selection.value : null;
  const selectedDatabaseID = selectedDatabase?.id ?? "";
  // Go marshals a nil slice as JSON null. Normalize here as a second line of
  // defence so a preview payload can never crash rendering.
  const missingProperties = preview?.missing_properties ?? [];
  const conflicts = preview?.conflicts ?? [];

  useEffect(() => {
    void backend
      .settings()
      .then((settings) => {
        setHotkey(settings.hotkey);
        setStartAtLogin(settings.start_at_login);
      })
      .catch((e) => reportError(e, { action: "load_settings" }, "onboarding"));
  }, [backend]);

  // Never leave the token visible once the user moves on.
  useEffect(() => {
    if (step !== "token") setShowToken(false);
  }, [step]);

  useEffect(() => {
    if (step !== "parent") return;
    const timer = window.setTimeout(() => {
      void loadTargets(query);
    }, 250);
    return () => window.clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, query]);

  useEffect(() => {
    if (step !== "parent" || !selectedDatabaseID) {
      setPreview(null);
      setPreviewError("");
      setPreviewLoading(false);
      setSchemaConfirmed(false);
      return;
    }
    let cancelled = false;
    setPreview(null);
    setPreviewError("");
    setPreviewLoading(true);
    setSchemaConfirmed(false);
    void backend
      .previewArchiveDatabase(token, selectedDatabaseID)
      .then((next) => {
        if (!cancelled) setPreview(next);
      })
      .catch((e) => {
        if (!cancelled) {
          reportError(e, { action: "preview_database", target: selectedDatabaseID }, "onboarding");
          setPreviewError(e instanceof Error ? e.message : String(e));
        }
      })
      .finally(() => {
        if (!cancelled) setPreviewLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [backend, step, token, selectedDatabaseID]);

  const loadTargets = async (search: string) => {
    setBusy(true);
    setError("");
    try {
      const result = await backend.parentPages(token, search);
      setDatabases(result.databases);
      setPages(result.pages);
      setSelection((current) => {
        if (current) {
          if (current.kind === "database") {
            const fresh = result.databases.find((item) => item.id === current.value.id);
            return fresh ? { kind: "database", value: fresh } : current;
          }
          const fresh = result.pages.find((item) => item.id === current.value.id);
          return fresh ? { kind: "page", value: fresh } : current;
        }
        const first = result.databases[0];
        return first ? { kind: "database", value: first } : null;
      });
    } catch (e) {
      reportError(e, { action: "load_targets", query: search }, "onboarding");
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const chooseDatabase = (database: ArchiveDatabase) => {
    setSelection({ kind: "database", value: database });
    setPreview(null);
    setPreviewError("");
    setSchemaConfirmed(false);
  };

  const choosePage = (page: ParentPage) => {
    setSelection({ kind: "page", value: page });
    setPreview(null);
    setPreviewError("");
    setSchemaConfirmed(false);
  };

  const useManualTarget = async () => {
    if (!manualTarget.trim() || resolving) return;
    setResolving(true);
    setError("");
    try {
      const target = await backend.resolveArchiveTarget(token, manualTarget.trim());
      if (target.kind === "database") {
        const database: ArchiveDatabase = {
          id: target.id,
          database_id: "",
          title: target.title,
          url: target.url,
        };
        setDatabases((current) =>
          current.some((item) => item.id === database.id) ? current : [database, ...current],
        );
        chooseDatabase(database);
      } else {
        const page: ParentPage = { id: target.id, title: target.title, url: target.url };
        setPages((current) =>
          current.some((item) => item.id === page.id) ? current : [page, ...current],
        );
        choosePage(page);
      }
      setManualTarget("");
    } catch (e) {
      reportError(e, { action: "resolve_target", target: manualTarget.trim() }, "onboarding");
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setResolving(false);
    }
  };

  const validate = async () => {
    setBusy(true);
    setError("");
    try {
      const info = await backend.validateToken(token);
      setWorkspace(info);
      setStep("parent");
    } catch (e) {
      reportError(e, { action: "validate_token" }, "onboarding");
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const connect = async () => {
    if (!selection) return;
    setBusy(true);
    setError("");
    try {
      const connected = await backend.connect({
        token,
        kind: selection.kind,
        target_id: selection.value.id,
        database_name: selection.kind === "page" ? databaseName.trim() || "memo" : "",
        confirm_schema_changes:
          selection.kind === "database" && missingProperties.length > 0 && schemaConfirmed,
      });
      setResult(connected);
      setStep("done");
    } catch (e) {
      reportError(
        e,
        { action: "connect", kind: selection.kind, target: selection.value.id },
        "onboarding",
      );
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const skip = async () => {
    try {
      await backend.completeOnboarding();
    } catch (e) {
      reportError(e, { action: "complete_onboarding" }, "onboarding");
    } finally {
      onSkip();
    }
  };

  const databaseOptions =
    selectedDatabase && !databases.some((item) => item.id === selectedDatabase.id)
      ? [selectedDatabase, ...databases]
      : databases;
  const pageOptions =
    selectedPage && !pages.some((item) => item.id === selectedPage.id)
      ? [selectedPage, ...pages]
      : pages;

  const needsSchemaConfirmation = Boolean(selectedDatabase && preview && missingProperties.length > 0);
  const hasSchemaConflicts = Boolean(selectedDatabase && preview && conflicts.length > 0);
  const connectDisabled =
    busy ||
    !selection ||
    (selectedDatabase
      ? previewLoading ||
        Boolean(previewError) ||
        !preview ||
        hasSchemaConflicts ||
        (needsSchemaConfirmation && !schemaConfirmed)
      : false);

  return (
    <div className="onboarding">
      <div className="onboarding-card">
        <aside className="onboarding-aside">
          <div className="onboarding-brand">
            <div className="brand-mark">m</div>
            <div>
              <strong>memo</strong>
              <span>个人笔记 · 可选 Notion 归档</span>
            </div>
          </div>
          <div className="onboarding-aside-note">
            <span>本地优先</span>
            <p>所有内容先写入本机 SQLite。Notion 只在你需要时参与归档与同步。</p>
          </div>
          <div className="onboarding-aside-foot">SQLite · 本地存储</div>
        </aside>

        <div className="onboarding-main">
        {step === "welcome" && (
          <>
            <h1>先写在本地，再决定要不要归档。</h1>
            <p className="lead">
              memo 是一本本地的、按时间排列的笔记。所有内容先写进这台电脑的 SQLite；
              连接 Notion 后，它只作为长期归档和跨设备同步的通道。
            </p>
            <ul className="onboarding-points">
              <li>
                <CheckIcon size={16} /> 全局快捷键快速记录，不打断手头的事
              </li>
              <li>
                <CheckIcon size={16} /> 中文全文搜索，1–2 个字也能搜
              </li>
              <li>
                <CheckIcon size={16} /> 旧笔记的正文可以清理，搜索和标签仍然保留
              </li>
            </ul>
            <div className="onboarding-actions">
              <button type="button" className="primary-button" onClick={() => setStep("token")}>
                连接我的 Notion
              </button>
              <button type="button" className="secondary-button" onClick={() => void skip()}>
                暂时不连接
              </button>
            </div>
            <p className="fine-print">Notion 是可选功能。不连接也能完整使用本地笔记与统计。</p>
          </>
        )}

        {step === "token" && (
          <>
            <h1>连接 Notion 归档</h1>
            <p className="lead">
              在 Notion 里创建一个集成，拿到 Personal Access Token，然后把目标数据库或页面共享给这个集成。
              令牌只保存在系统钥匙串，不会写进本地数据库或日志。
            </p>
            <div className="field">
              <label htmlFor={tokenInputID}>Notion 集成令牌</label>
              <div className="field-input">
                <input
                  id={tokenInputID}
                  type={showToken ? "text" : "password"}
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="ntn_…"
                  autoComplete="off"
                  spellCheck={false}
                />
                <button
                  type="button"
                  className="field-suffix"
                  onClick={() => setShowToken((shown) => !shown)}
                  onMouseDown={(e) => e.preventDefault()}
                  aria-label={showToken ? "隐藏令牌" : "显示令牌"}
                  aria-pressed={showToken}
                  title={showToken ? "隐藏令牌" : "显示令牌"}
                >
                  {showToken ? <EyeOffIcon size={16} /> : <EyeIcon size={16} />}
                </button>
              </div>
            </div>
            {workspace && (
              <div className="workspace-preview">
                <span>工作区</span>
                <strong>{workspace.name}</strong>
                <em>文件上限 {workspace.file_limit ? `${Math.round(workspace.file_limit / 1024 / 1024)} MB` : "未知"}</em>
              </div>
            )}
            {error && <div className="inline-error">{error}</div>}
            <div className="onboarding-actions">
              <button type="button" className="secondary-button" onClick={() => (connectOnly ? onSkip() : setStep("welcome"))}>
                返回
              </button>
              <button type="button" className="primary-button" onClick={() => void validate()} disabled={busy || !token.trim()}>
                {busy ? "验证中…" : "验证令牌"}
              </button>
            </div>
            <p className="fine-print">
              优先把目标数据库共享给集成，这样可以立即使用；也可以只共享一个页面，应用会在页面下新建数据库。
            </p>
          </>
        )}

        {step === "parent" && (
          <>
            <h1>选择归档位置</h1>
            <p className="lead">
              优先直接使用已共享给集成的数据库；也可以选择一个页面，让 memo 在页面下新建自己的数据库。
            </p>
            <label className="search-box onboarding-search">
              <SearchIcon size={16} />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="搜索页面或数据库标题"
                aria-label="搜索 Notion 页面或数据库"
              />
            </label>
            <div className="page-tools">
              <span>同时列出集成已共享的数据库和页面。</span>
              <button type="button" className="page-refresh" onClick={() => void loadTargets(query)} disabled={busy}>
                重新搜索
              </button>
            </div>

            <div className="target-group">
              <div className="target-group-title">
                共享数据库 <span>推荐 · 直接使用</span>
              </div>
              <div className="page-list">
                {busy && <div className="muted">正在读取…</div>}
                {!busy && databaseOptions.length === 0 && (
                  <div className="muted page-empty">
                    没有找到已共享的数据库。如果没有合适的页面，也可以直接粘贴数据库链接。
                  </div>
                )}
                {databaseOptions.map((database) => {
                  const isSelected = selectedDatabase?.id === database.id;
                  return (
                    <button
                      type="button"
                      key={database.id}
                      className={`page-row ${isSelected ? "is-selected" : ""}`}
                      onClick={() => chooseDatabase(database)}
                    >
                      <span>{database.title}</span>
                      <span className="target-badge">
                        {isSelected && <CheckIcon size={14} />} 直接使用
                      </span>
                    </button>
                  );
                })}
              </div>
            </div>

            <div className="target-group">
              <div className="target-group-title">
                共享页面 <span>将在页面下新建数据库</span>
              </div>
              <div className="page-list">
                {busy && <div className="muted">正在读取…</div>}
                {!busy && pageOptions.length === 0 && (
                  <div className="muted page-empty">
                    没有找到已共享的页面。把页面共享给集成后，点“重新搜索”。
                  </div>
                )}
                {pageOptions.map((page) => {
                  const isSelected = selectedPage?.id === page.id;
                  return (
                    <button
                      type="button"
                      key={page.id}
                      className={`page-row ${isSelected ? "is-selected" : ""}`}
                      onClick={() => choosePage(page)}
                    >
                      <span>{page.title}</span>
                      <span className="target-badge">
                        {isSelected && <CheckIcon size={14} />} 新建数据库
                      </span>
                    </button>
                  );
                })}
              </div>
            </div>

            {selectedDatabase && (
              <div className="selection-panel">
                <div className="selection-line">
                  <strong>{selectedDatabase.title}</strong>
                  <span className="mini-badge">已共享数据库</span>
                </div>
                {previewLoading && <div className="muted">正在检查数据库属性…</div>}
                {previewError && <div className="inline-error">{previewError}</div>}
                {preview && (
                  <>
                    {conflicts.length > 0 ? (
                      <div className="schema-warning is-error">
                        <strong>无法使用这个数据库</strong>
                        <ul>
                          {conflicts.map((conflict) => (
                            <li key={conflict.name}>
                              属性「{conflict.name}」已有类型 {conflict.existing_type}，memo 需要 {conflict.required_type}
                            </li>
                          ))}
                        </ul>
                        <p>请改用一个共享数据库，或先在 Notion 中调整冲突属性。</p>
                      </div>
                    ) : missingProperties.length > 0 ? (
                      <div className="schema-warning">
                        <strong>连接时会新增 {missingProperties.length} 个属性</strong>
                        <ul>
                          {missingProperties.map((property) => (
                            <li key={property.name}>
                              {property.name} <em>{property.type}</em>
                            </li>
                          ))}
                        </ul>
                        <p>只会新增缺失属性，不会修改或删除已有属性。</p>
                        <label className="checkbox-row">
                          <input
                            type="checkbox"
                            checked={schemaConfirmed}
                            onChange={(e) => setSchemaConfirmed(e.target.checked)}
                          />
                          <span>我确认向这个数据库新增上述属性</span>
                        </label>
                      </div>
                    ) : (
                      <div className="schema-ok">该数据库已包含 memo 所需属性，可直接使用。</div>
                    )}
                    {preview.has_existing_rows && (
                      <p className="fine-print">
                        这个数据库已有记录，连接后会作为 memo 同步到本地。
                      </p>
                    )}
                  </>
                )}
              </div>
            )}

            {selectedPage && (
              <div className="selection-panel">
                <div className="selection-line">
                  <strong>{selectedPage.title}</strong>
                  <span className="mini-badge">将在页面下新建数据库</span>
                </div>
                <label className="field">
                  <span>新数据库名称</span>
                  <input
                    value={databaseName}
                    onChange={(e) => setDatabaseName(e.target.value)}
                    placeholder="memo"
                    aria-label="新数据库名称"
                    spellCheck={false}
                  />
                </label>
                <p className="fine-print">
                  memo 会在该页面下创建数据库「{databaseName.trim() || "memo"}」，并把归档记录写进去。
                </p>
              </div>
            )}

            <div className="manual-page">
              <input
                value={manualTarget}
                onChange={(e) => setManualTarget(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") void useManualTarget();
                }}
                placeholder="粘贴页面或数据库链接/ID"
                aria-label="Notion 链接或 ID"
                spellCheck={false}
              />
              <button
                type="button"
                className="secondary-button"
                onClick={() => void useManualTarget()}
                disabled={resolving || !manualTarget.trim()}
              >
                {resolving ? "读取中…" : "使用此链接"}
              </button>
            </div>
            {error && <div className="inline-error">{error}</div>}
            <div className="onboarding-actions">
              <button type="button" className="secondary-button" onClick={() => setStep("token")}>
                返回
              </button>
              <button type="button" className="primary-button" onClick={() => void connect()} disabled={connectDisabled}>
                {busy
                  ? selectedPage
                    ? "正在创建…"
                    : "正在连接…"
                  : selectedPage
                    ? "创建数据库并连接"
                    : "使用此数据库并连接"}
              </button>
            </div>
          </>
        )}

        {step === "done" && (
          <>
            <div className="done-mark">
              <CheckIcon size={28} />
            </div>
            <h1>归档已经准备好了。</h1>
            <p className="lead">
              {result?.created_database
                ? `已在所选页面下创建数据库「${result?.database_title ?? "memo"}」。`
                : `已连接共享数据库「${result?.database_title ?? "memo"}」。`}
              之后每次编辑都会先写本地，再由同步队列推送。旧正文和附件会按保留期清理，元数据与搜索索引保留。
            </p>
            <div className="done-facts">
              <div><span>数据库</span><strong>{result?.database_title ?? "—"}</strong></div>
              <div>
                <span>数据源</span>
                <strong>{result?.data_source_id ? `${result.data_source_id.slice(0, 12)}…` : "—"}</strong>
              </div>
              <div><span>标题属性</span><strong>{result?.title_property ?? "—"}</strong></div>
              <div><span>文件上限</span><strong>{Math.round((result?.workspace?.file_limit ?? 0) / 1024 / 1024)} MB</strong></div>
            </div>
            <div className="onboarding-preferences">
              <label className="field">
                <span>呼出快捷键</span>
                <input value={hotkey} onChange={(e) => setHotkey(e.target.value)} spellCheck={false} />
              </label>
              <label className="checkbox-row">
                <input type="checkbox" checked={startAtLogin} onChange={(e) => setStartAtLogin(e.target.checked)} />
                <span>开机时启动 memo</span>
              </label>
            </div>
            <p className="fine-print">首次同步会在后台开始；主窗口会显示待推送数量。旧正文按保留策略清理，搜索索引保留。</p>
            {error && <div className="inline-error">{error}</div>}
            <div className="onboarding-actions">
              <button
                type="button"
                className="primary-button"
                disabled={busy}
                onClick={() => {
                  setBusy(true);
                  setError("");
                  void backend
                    .setHotkey(hotkey)
                    .then(() => backend.setStartAtLogin(startAtLogin))
                    .then(() => backend.completeOnboarding())
                    .then(onDone)
                    .catch((e) => {
                      reportError(e, { action: "finish_onboarding" }, "onboarding");
                      setError(e instanceof Error ? e.message : String(e));
                    })
                    .finally(() => setBusy(false));
                }}
              >
                {busy ? "正在保存…" : "开始使用"}
              </button>
            </div>
          </>
        )}
        </div>
      </div>
    </div>
  );
}
