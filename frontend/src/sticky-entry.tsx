import React from "react";
import ReactDOM from "react-dom/client";
import StickyApp from "./sticky";
import { wailsBackend } from "./wailsBackend";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { installGlobalErrorHandlers, setErrorSink } from "./lib/errorLog";
import "./styles.css";

// Register diagnostics before the first render so an initial render crash is
// persisted even if StickyApp's effects never run.
setErrorSink((report) => wailsBackend.reportError(report));
installGlobalErrorHandlers();

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <ErrorBoundary source="sticky">
      <StickyApp />
    </ErrorBoundary>
  </React.StrictMode>,
);
