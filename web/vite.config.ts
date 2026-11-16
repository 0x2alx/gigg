import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

// dist/.keep (tracked) lets go:embed compile before the first web build; the build empties dist (so
// no stale assets get embedded) and this writes it back as part of the bundle
const keepDist: Plugin = {
  name: "keep-dist",
  generateBundle() {
    this.emitFile({ type: "asset", fileName: ".keep", source: "" });
  },
};

export default defineConfig({
  plugins: [react(), keepDist],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    assetsDir: "assets",
    sourcemap: false,
    // never inline assets as data: URLs: the Content-Security-Policy allows fonts and images from
    // the same origin only, and separate hashed files cache better anyway
    assetsInlineLimit: 0,
  },
});
