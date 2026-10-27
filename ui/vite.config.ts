import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vite'

// `base` is the URL prefix the extension is mounted at. It MUST match UISpec.Base in main.go:
// the Go side serves the build under /ext/github, and anything Vite emits with a different prefix
// (asset URLs, dynamic imports) would 404 there.
export default defineConfig({
  plugins: [svelte()],
  base: '/ext/github/',
  build: {
    // The output is go:embed-ed, so it lands in cmd/github-ext/ui at build time — see the
    // Containerfile. Keeping it out of the repo means a stale build can never be committed and
    // served in place of the real one.
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2022',
  },
  server: {
    host: '0.0.0.0',
    port: 18500,
    strictPort: true,
    allowedHosts: true,
    // In dev the UI runs on :18500 while the extension serves its API on :8080, so the API paths are
    // proxied rather than configured: the code in the browser uses the SAME absolute URLs it uses in
    // production, and a base-path mistake shows up in dev instead of at deploy time.
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: true },
    },
  },
})
