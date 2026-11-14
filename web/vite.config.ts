import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: false, // keeps dist/.keep, which go:embed needs before the first build
    assetsDir: "assets",
    sourcemap: false,
  },
});
