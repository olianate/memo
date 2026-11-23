import React from "react";
import { reportError } from "../lib/errorLog";

type Props = {
  children: React.ReactNode;
  /** Identifies which part of the UI failed in the local error log. */
  source?: string;
};

type State = {
  error: Error | null;
  componentStack: string;
};

/**
 * Last-resort UI for render failures. Without a boundary React unmounts the
 * whole tree, which is what produced the blank white onboarding screen.
 */
export class ErrorBoundary extends React.Component<Props, State> {
  state: State = { error: null, componentStack: "" };

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo): void {
    const componentStack = info.componentStack ?? "";
    this.setState({ componentStack });
    reportError(error, { componentStack }, this.props.source ?? "react");
  }

  private retry = (): void => {
    this.setState({ error: null, componentStack: "" });
  };

  render(): React.ReactNode {
    const { error, componentStack } = this.state;
    if (!error) return this.props.children;

    return (
      <div className="fatal-error" role="alert">
        <div className="fatal-error-card">
          <div className="onboarding-brand">
            <div className="brand-mark">m</div>
            <div>
              <strong>memo</strong>
              <span>界面恢复</span>
            </div>
          </div>
          <h1>界面遇到了一个错误</h1>
          <p className="lead">
            错误已记录到本地日志。你可以先重试；如果仍然出现，请到设置 → 错误日志 中复制详情反馈。
          </p>
          <div className="fatal-error-message">{error.message || error.name || "未知错误"}</div>
          {(error.stack || componentStack) && (
            <details className="fatal-error-details">
              <summary>技术细节</summary>
              <pre>{[error.stack, componentStack].filter(Boolean).join("\n\n")}</pre>
            </details>
          )}
          <div className="onboarding-actions">
            <button type="button" className="primary-button" onClick={this.retry}>
              重试
            </button>
            <button type="button" className="secondary-button" onClick={() => window.location.reload()}>
              重新加载
            </button>
          </div>
        </div>
      </div>
    );
  }
}
