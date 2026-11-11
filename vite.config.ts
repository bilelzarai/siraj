import { defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";

// Builds web/assets into web/dist with a manifest the Go server reads (web/embed.go).
// In development `make dev` runs this as a dev server on :5173 and the base layout
// loads the assets from there instead (VITE_DEV_SERVER).
export default defineConfig({
  root: "web",
  base: "/static/",
  plugins: [tailwindcss()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    manifest: true,
    rollupOptions: {
      // Relative to `root` (web/), so the dev server and the build agree on the entry.
      // main.ts imports app.css and admin.ts imports admin.css, so each entry carries
      // its own stylesheet; the manifest lists them.
      input: { main: "assets/js/main.ts", admin: "assets/js/admin.ts" },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    origin: "http://localhost:5173",
    cors: true,
  },
});
