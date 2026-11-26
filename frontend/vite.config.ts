import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";
import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [
    react(),
    wails("./bindings"),
    {
      // Production keeps script-src 'self'. Vite/React Refresh injects an inline
      // preamble in dev only, so relax that one directive for `wails3 dev`.
      name: "dev-csp",
      apply: "serve",
      transformIndexHtml(html: string) {
        return html.replace("script-src 'self'", "script-src 'self' 'unsafe-inline'");
      },
    },
    {
      // main.go embeds frontend/dist. Vite empties that folder on every build,
      // so put the tracked placeholder back for a clean working tree.
      name: "embed-placeholder",
      apply: "build",
      closeBundle() {
        writeFileSync(fileURLToPath(new URL("./dist/.gitkeep", import.meta.url)), "");
      },
    },
  ],
  build: {
    rollupOptions: {
      input: {
        main: fileURLToPath(new URL("./index.html", import.meta.url)),
        sticky: fileURLToPath(new URL("./sticky.html", import.meta.url)),
      },
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: false,
    server: {
      deps: {
        // The package ships extensionless internal ESM imports, which Node
        // refuses to resolve when Vitest externalizes the dependency.
        inline: ["@atomic-editor/editor"],
      },
    },
  },
});
