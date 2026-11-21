import React from "react";
import ReactDOM from "react-dom/client";
import StickyApp from "./sticky";
import "./styles.css";

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <StickyApp />
  </React.StrictMode>,
);
