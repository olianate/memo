import type { ErrorReport } from "../types";

export type ErrorSink = (report: ErrorReport) => void | Promise<void>;

const MAX_PENDING = 20;
const DEDUPE_MS = 5000;

let sink: ErrorSink | null = null;
let pending: ErrorReport[] = [];
const seen = new Map<string, number>();

function redact(text: string): string {
  return text
    .replace(/\b(?:ntn|secret)_[A-Za-z0-9_-]{6,}/g, "[已隐藏令牌]")
    .replace(/\bBearer\s+[A-Za-z0-9._~+/=-]{6,}/gi, "Bearer [已隐藏]")
    .replace(/\b(token|access_token|api_key|apikey|password|secret)\b\s*[:=]\s*("[^"]*"|'[^']*'|[^\s,;&]+)/gi, "$1=[已隐藏]")
    .replace(/([?&](?:token|access_token|api_key|apikey|secret)=)[^&\s]+/gi, "$1[已隐藏]");
}

function messageOf(error: unknown): string {
  if (error instanceof Error) return error.message || error.name;
  if (typeof error === "string") return error;
  if (error && typeof error === "object") {
    const value = (error as { message?: unknown }).message;
    if (typeof value === "string" && value) return value;
  }
  try {
    const json = JSON.stringify(error);
    if (json && json !== "{}") return json;
  } catch {
    /* fall through */
  }
  return String(error);
}

function stackOf(error: unknown): string | undefined {
  if (error && typeof error === "object") {
    const value = (error as { stack?: unknown }).stack;
    if (typeof value === "string" && value) return value;
  }
  return undefined;
}

function contextOf(context: unknown): string | undefined {
  if (context == null) return undefined;
  let text: string;
  if (typeof context === "string") {
    text = context;
  } else {
    const base = typeof window === "undefined" ? {} : { url: window.location.href };
    try {
      text = JSON.stringify({ ...(context as Record<string, unknown>), ...base });
    } catch {
      text = String(context);
    }
  }
  return redact(text).slice(0, 4000);
}

function deliver(report: ErrorReport): void {
  if (!sink) {
    if (pending.length >= MAX_PENDING) pending.shift();
    pending.push(report);
    return;
  }
  try {
    void Promise.resolve(sink(report)).catch(() => {});
  } catch {
    /* reporting must never break the caller */
  }
}

/**
 * Registers the backend used to persist diagnostics. Reports collected before
 * the UI finishes starting are flushed once a sink is available.
 */
export function setErrorSink(next: ErrorSink | null): void {
  sink = next;
  if (!next || pending.length === 0) return;
  const queued = pending;
  pending = [];
  for (const report of queued) deliver(report);
}

/**
 * Reports an error for local diagnostics. Safe to call from catch blocks,
 * event handlers, and React error boundaries; it never throws.
 */
export function reportError(error: unknown, context?: unknown, source = "frontend"): void {
  const message = redact(messageOf(error) || "未知错误").slice(0, 2000);
  const stack = stackOf(error);
  const detail = contextOf(context);
  const key = `${source}|${message}|${(detail ?? "").slice(0, 160)}`;
  const now = Date.now();
  const last = seen.get(key);
  if (last !== undefined && now - last < DEDUPE_MS) return;
  seen.set(key, now);
  if (seen.size > 100) {
    for (const [oldKey, at] of seen) {
      if (now - at > DEDUPE_MS * 4) seen.delete(oldKey);
    }
  }
  deliver({
    source,
    message,
    stack: stack ? redact(stack).slice(0, 6000) : undefined,
    context: detail,
  });
}

let handlerUsers = 0;
let removeHandlers: (() => void) | null = null;

/**
 * Installs window-level handlers for uncaught errors and rejected promises.
 * Returns an unsubscribe function; handlers are shared by all callers.
 */
export function installGlobalErrorHandlers(): () => void {
  if (handlerUsers === 0 && typeof window !== "undefined") {
    const onError = (event: ErrorEvent) => {
      if (/ResizeObserver loop/i.test(event.message ?? "")) return;
      reportError(
        event.error ?? event.message,
        { kind: "window.error", filename: event.filename, lineno: event.lineno, colno: event.colno },
        "window",
      );
    };
    const onRejection = (event: PromiseRejectionEvent) => {
      reportError(event.reason, { kind: "unhandledrejection" }, "window");
    };
    window.addEventListener("error", onError);
    window.addEventListener("unhandledrejection", onRejection);
    removeHandlers = () => {
      window.removeEventListener("error", onError);
      window.removeEventListener("unhandledrejection", onRejection);
    };
  }
  handlerUsers += 1;
  let disposed = false;
  return () => {
    if (disposed) return;
    disposed = true;
    handlerUsers = Math.max(0, handlerUsers - 1);
    if (handlerUsers === 0) {
      removeHandlers?.();
      removeHandlers = null;
    }
  };
}
