import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import { wailsBackend } from "./wailsBackend";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { installGlobalErrorHandlers, setErrorSink } from "./lib/errorLog";
import "./styles.css";

// Register diagnostics before the first render so even an initial render crash
// is persisted, not only react errors caught after App's effects have run.
setErrorSink((report) => wailsBackend.reportError(report));
installGlobalErrorHandlers();

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <ErrorBoundary source="app">
      <App />
    </ErrorBoundary>
  </React.StrictMode>,
);
