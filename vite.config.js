import { defineConfig } from 'vite'
import { resolve } from 'node:path'

// Two bundled entries, and one file copied verbatim.
//
// The stylesheet is imported from app.js rather than listed as its own input —
// the bundler then records it under the app entry's "css" array, which is how
// the server finds the built filename in one lookup.
//
// boot.js is NOT bundled. A built entry is an ES module, and a module script is
// deferred by definition; the theme boot has to run before first paint or it
// reintroduces exactly the flash it exists to prevent. So it sits in the public
// directory, is copied unchanged, and stays a classic blocking script.
export default defineConfig({
  root: resolve(import.meta.dirname, 'web/src'),
  publicDir: resolve(import.meta.dirname, 'web/public'),
  base: '/static/dist/',            // must match where the static handler serves
  build: {
    outDir: '../../static/dist',    // inside the embedded tree: one binary still
    emptyOutDir: true,              // required, outDir is outside root
    manifest: true,
    rollupOptions: {
      // Absolute — with `root` set, a relative path resolves against root again.
      input: {
        app:    resolve(import.meta.dirname, 'web/src/js/app.js'),
        alpine: resolve(import.meta.dirname, 'web/src/js/alpine.js'),
      },
    },
  },
  server: { port: 5173, strictPort: true, cors: true },
})
