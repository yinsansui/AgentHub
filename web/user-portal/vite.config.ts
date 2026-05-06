import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const proxyTarget = process.env.AGENTHUB_USER_PORTAL_PROXY_TARGET ?? "http://127.0.0.1:3000";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5174,
    strictPort: false,
    proxy: {
      "/health": proxyTarget,
      "/workspaces": proxyTarget,
      "/sessions": proxyTarget
    }
  }
});
