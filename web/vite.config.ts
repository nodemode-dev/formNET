import { defineConfig } from "vite";

// The client is embedded into the Go binary from web/dist and served at /.
// In development, vite proxies the API to a running `formnet serve`.
export default defineConfig({
  build: { outDir: "dist", emptyOutDir: true, sourcemap: false, target: "es2022" },
  server: {
    port: 5176,
    proxy: {
      "/api": { target: "http://127.0.0.1:8760", changeOrigin: false },
      "/ca.pem": { target: "http://127.0.0.1:8760", changeOrigin: false },
      "/healthz": { target: "http://127.0.0.1:8760", changeOrigin: false },
    },
  },
});
