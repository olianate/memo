import type { SearchResult } from "../types";
import { formatDate } from "../lib/format";
import { EmptyState, StatusBadge, TagChip } from "./common";

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
          <article key={memo.id} className="search-result" onClick={() => onOpen(result)} tabIndex={0} onKeyDown={(e) => {
            if (e.key === "Enter" && e.target === e.currentTarget) onOpen(result);
          }}>
            <div className="memo-meta">
              <time>{formatDate(memo.created_at)}</time>
              <StatusBadge memo={memo} />
            </div>
            {(memo.tags ?? []).length > 0 && (
              <div className="memo-tags">
                {(memo.tags ?? []).map((tag) => (
                  <TagChip key={tag.id} name={tag.name} />
                ))}
              </div>
            )}
            <p className="search-snippet">
              {result.before}
              {result.match && <mark>{result.match}</mark>}
              {result.after}
            </p>
          </article>
        );
      })}
    </div>
  );
}
