import { useEffect, useState } from "react";
import type { Dashboard } from "../types";
import type { StatsBackend } from "../backend";
import { currentMonth, formatMonth, humanBytes } from "../lib/format";
import { Heatmap } from "./common";
import { RefreshIcon } from "./Icons";

const compositionLabels: Record<string, string> = {
  paragraph: "段落",
  heading: "标题",
  list: "列表",
  todo: "待办",
  quote: "引用",
  code: "代码",
  attachment: "附件",
};

const compositionColors: Record<string, string> = {
  paragraph: "#22c55e",
  heading: "#0ea5e9",
  list: "#84cc16",
  todo: "#f59e0b",
  quote: "#14b8a6",
  code: "#6366f1",
  attachment: "#ec4899",
};

export function StatsPage({
  backend,
  onError,
  initialMonth,
}: {
  backend: StatsBackend;
  onError: (message: string) => void;
  initialMonth?: string;
}) {
  const [month, setMonth] = useState(initialMonth || currentMonth());
  const [data, setData] = useState<Dashboard | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = async (value: string) => {
    setLoading(true);
    setError("");
    try {
      setData(await backend.dashboard(value));
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setError(message);
      onError(message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load(month);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [month]);

  if (loading && !data) {
    return (
      <div className="timeline-loading">
        <span className="spinner" /> 正在统计…
      </div>
    );
  }
  if (!data) {
    return <div className="inline-error">{error || "统计暂时不可用"}</div>;
  }

  const compositionCount = data.composition.reduce((n, c) => n + c.count, 0);
  const totalComposition = Math.max(1, compositionCount);
  let angle = 0;
  const donut = data.composition
    .filter((c) => c.count > 0)
    .map((c) => {
      const start = angle;
      angle += (c.count / totalComposition) * 360;
      return `${compositionColors[c.type] ?? "#94a3b8"} ${start}deg ${angle}deg`;
    })
    .join(", ");
  const maxDaily = Math.max(1, ...data.daily.map((d) => d.count));
  const maxHour = Math.max(1, ...data.hours.map((h) => h.count));
  const maxTag = Math.max(1, ...data.top_tags.map((t) => t.count));
  const maxBucket = Math.max(1, ...data.word_buckets.map((b) => b.count));
  const tags = Array.from(new Set(data.co_occurrence.flatMap((c) => [c.a, c.b]))).slice(0, 8);
  const pairMap = new Map(data.co_occurrence.map((c) => [`${c.a}|${c.b}`, c.count]));
  const empty = data.totals.notes === 0;

  return (
    <div className="stats-page">
      <div className="stats-head">
        <div>
          <h2>记录统计</h2>
          <p>统计来自本地 SQLite，不上传。即使正文被清理，元数据仍然计入。</p>
        </div>
        <div className="stats-controls">
          <input
            type="month"
            value={month}
            aria-label="选择月份"
            onChange={(e) => setMonth(e.target.value || currentMonth())}
          />
          <button type="button" className="icon-button" onClick={() => void load(month)} aria-label="刷新统计">
            <RefreshIcon size={16} />
          </button>
        </div>
      </div>

      {empty && <div className="stats-empty">这个月还没有记录。换一个月份，或开始写第一条。</div>}

      <div className="stat-totals">
        {[
          ["笔记", data.totals.notes],
          ["字数", data.totals.words],
          ["标签", data.totals.tags],
          ["附件", data.totals.attachments],
          ["活跃天数", data.totals.active_days],
        ].map(([label, value]) => (
          <div className="stat-total" key={String(label)}>
            <strong>{value}</strong>
            <span>{label}</span>
          </div>
        ))}
      </div>

      <section className="stat-card">
        <div className="stat-card-head">
          <h3>每日记录</h3>
          <span>{formatMonth(month)} · 共 {data.totals.notes} 条</span>
        </div>
        <div className="bar-chart daily-chart">
          {data.daily.map((day, index) => (
            <div className="bar-column" key={day.date} title={`${day.date} · ${day.count} 条`}>
              <span className="bar" style={{ height: `${(day.count / maxDaily) * 100}%` }} data-empty={day.count === 0} />
              {(index === 0 || (index + 1) % 5 === 0 || index === data.daily.length - 1) && (
                <em>{Number(day.date.slice(-2))}</em>
              )}
            </div>
          ))}
        </div>
      </section>

      <section className="stat-card">
        <div className="stat-card-head">
          <h3>最近一年</h3>
          <span className="heat-legend">
            少
            <i className="level-0" />
            <i className="level-1" />
            <i className="level-2" />
            <i className="level-3" />
            <i className="level-4" />
            多
          </span>
        </div>
        <Heatmap days={data.year} weeks={53} />
      </section>

      <div className="stat-grid">
        <section className="stat-card">
          <div className="stat-card-head">
            <h3>内容构成</h3>
            <span>按块类型 · 共 {compositionCount} 块</span>
          </div>
          <div className="donut-wrap">
            <div className="donut" style={{ background: donut ? `conic-gradient(${donut})` : undefined }}>
              <div className="donut-hole">
                <strong>{compositionCount}</strong>
                <span>块</span>
              </div>
            </div>
            <ul className="legend">
              {data.composition.map((c) => (
                <li key={c.type}>
                  <i style={{ background: compositionColors[c.type] }} />
                  <span>{compositionLabels[c.type] ?? c.type}</span>
                  <em>{Math.round((c.count / totalComposition) * 100)}%</em>
                </li>
              ))}
            </ul>
          </div>
        </section>

        <section className="stat-card">
          <div className="stat-card-head">
            <h3>字数分布</h3>
            <span>每条笔记</span>
          </div>
          <div className="bucket-list">
            {data.word_buckets.map((bucket) => (
              <div className="bucket-row" key={bucket.label}>
                <span>{bucket.label}</span>
                <div className="bucket-track">
                  <i style={{ width: `${(bucket.count / maxBucket) * 100}%` }} />
                </div>
                <em>{bucket.count}</em>
              </div>
            ))}
          </div>
        </section>

        <section className="stat-card">
          <div className="stat-card-head">
            <h3>标签 Top</h3>
            <span>v1 标签是扁平的</span>
          </div>
          <div className="bucket-list">
            {data.top_tags.slice(0, 8).map((tag) => (
              <div className="bucket-row" key={tag.id ?? tag.name}>
                <span>#{tag.name}</span>
                <div className="bucket-track">
                  <i style={{ width: `${(tag.count / maxTag) * 100}%` }} />
                </div>
                <em>{tag.count}</em>
              </div>
            ))}
            {data.top_tags.length === 0 && <div className="muted">这个月还没有标签。</div>}
          </div>
        </section>

        <section className="stat-card">
          <div className="stat-card-head">
            <h3>标签共现</h3>
            <span>同一条笔记中出现</span>
          </div>
          {tags.length > 0 ? (
            <div className="co-occurrence" style={{ gridTemplateColumns: `80px repeat(${tags.length}, 1fr)` }}>
              <span />
              {tags.map((tag) => (
                <span className="co-head" key={tag} title={tag}>
                  {tag.slice(0, 4)}
                </span>
              ))}
              {tags.map((row) => (
                <div className="co-row" key={row} style={{ display: "contents" }}>
                  <span className="co-label" title={row}>
                    {row.slice(0, 5)}
                  </span>
                  {tags.map((col) => {
                    if (row === col) return <span className="co-cell is-self" key={col} />;
                    const key = row < col ? `${row}|${col}` : `${col}|${row}`;
                    const count = pairMap.get(key) ?? 0;
                    return (
                      <span
                        key={col}
                        className={`co-cell ${count > 0 ? "has-value" : ""}`}
                        style={{ opacity: count === 0 ? 0.25 : Math.min(1, 0.35 + count / 5) }}
                        title={`${row} + ${col} · ${count}`}
                      >
                        {count || ""}
                      </span>
                    );
                  })}
                </div>
              ))}
            </div>
          ) : (
            <div className="muted">还没有足够的标签形成共现。</div>
          )}
        </section>

        <section className="stat-card">
          <div className="stat-card-head">
            <h3>记录时段</h3>
            <span>按本地小时</span>
          </div>
          <div className="hour-chart">
            {data.hours.map((hour) => (
              <div key={hour.hour} title={`${hour.hour}:00 · ${hour.count} 条`}>
                <i style={{ height: `${(hour.count / maxHour) * 100}%` }} />
              </div>
            ))}
          </div>
          <div className="hour-axis">
            <span>0</span>
            <span>6</span>
            <span>12</span>
            <span>18</span>
            <span>23</span>
          </div>
        </section>

        <section className="stat-card">
          <div className="stat-card-head">
            <h3>待办完成</h3>
            <span>{data.todos.done} / {data.todos.total}</span>
          </div>
          <div className="progress large">
            <i style={{ width: `${data.todos.total ? (data.todos.done / data.todos.total) * 100 : 0}%` }} />
          </div>
          <p className="muted">{data.todos.total === 0 ? "这个月没有待办。" : `完成率 ${Math.round((data.todos.done / data.todos.total) * 100)}%`}</p>

          <div className="stat-card-head spaced">
            <h3>附件</h3>
            <span>{humanBytes(data.attachments.bytes)}</span>
          </div>
          <div className="mini-stats">
            <div><strong>{data.attachments.images}</strong><span>图片</span></div>
            <div><strong>{data.attachments.files}</strong><span>文件</span></div>
          </div>
        </section>
      </div>

      <section className="stat-card">
        <div className="stat-card-head">
          <h3>同步与存储健康</h3>
          <span>全部本地笔记</span>
        </div>
        <div className="health-grid">
          <div><strong>{data.health.local_only}</strong><span>仅本地</span></div>
          <div><strong>{data.health.synced}</strong><span>已同步</span></div>
          <div><strong>{data.health.evicted}</strong><span>已清理正文</span></div>
          <div><strong>{data.health.pending}</strong><span>待同步</span></div>
        </div>
      </section>
    </div>
  );
}
