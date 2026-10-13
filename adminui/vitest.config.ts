import { defineConfig } from "vitest/config";
import path from "path";

export default defineConfig({
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
      "brotli-wasm": path.resolve(__dirname, "node_modules/brotli-wasm/index.node.js")
    }
  },
  test: {
    environment: "node"
  }
});
