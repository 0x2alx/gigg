import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: false, // keeps dist/.keep, which go:embed needs before the first build
    assetsDir: "assets",
    sourcemap: false,
    // never inline assets as data: URLs: the Content-Security-Policy allows fonts and images from
    // the same origin only, and separate hashed files cache better anyway
    assetsInlineLimit: 0,
  },
});
