import type { SearchResult } from "../types";
import { formatTime } from "../lib/format";
import { EmptyState } from "./common";

function searchState(result: SearchResult): string {
  const memo = result.memo;
  if (memo.deleted) return "回收站";
  if (memo.local_state === "evicted") return "已清理正文";
  if (memo.sync_error) return "同步失败";
  return memo.notion_synced_at ? "已同步" : "仅本地";
}

export function SearchPanel({
  results,
  query,
  loading,
  onOpen,
}: {
  results: SearchResult[];
  query: string;
  loading: boolean;
  onOpen: (result: SearchResult) => void;
}) {
  if (loading) {
    return (
      <div className="timeline-loading">
        <span className="spinner" />
        正在搜索…
      </div>
    );
  }
  if (results.length === 0) {
    return <EmptyState title="没有找到匹配的笔记" hint={`试着换一个关键词，或清除「${query}」重新搜索。`} />;
  }
  return (
    <div className="search-results">
      <div className="search-summary">
        找到 <strong>{results.length}</strong> 条与「{query}」相关的笔记
      </div>
      {results.map((result) => {
        const memo = result.memo;
        return (
          <article
            key={memo.id}
            className="search-result"
            onClick={() => onOpen(result)}
            tabIndex={0}
            onKeyDown={(e) => {
              if (e.key === "Enter" && e.target === e.currentTarget) onOpen(result);
            }}
          >
            <div className="search-time">
              <time>{formatTime(memo.created_at)}</time>
              <span>{memo.created_at.slice(5, 10)}</span>
            </div>
            <div className="search-main">
              <p className="search-snippet">
                {result.before}
                {result.match && <mark>{result.match}</mark>}
                {result.after}
              </p>
              {(memo.tags ?? []).length > 0 && (
                <div className="entry-tags">
                  {(memo.tags ?? []).map((tag) => (
                    <span key={tag.id} className="tag-chip">
                      <span className="hash" aria-hidden="true">
                        #
                      </span>
                      {tag.name}
                    </span>
                  ))}
                </div>
              )}
              <div className="entry-foot">
                <span className="entry-status">{searchState(result)}</span>
              </div>
            </div>
          </article>
        );
      })}
    </div>
  );
}
