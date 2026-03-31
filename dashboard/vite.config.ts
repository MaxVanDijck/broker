import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/broker.v1": "http://localhost:8080",
      "/agent": "http://localhost:8080",
      "/api": "http://localhost:8080",
      "/healthz": "http://localhost:8080",
    },
  },
});
