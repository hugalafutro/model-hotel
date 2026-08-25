import path from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Front Desk SPA build. Deliberately simpler than the main web/ config: the
// control-plane UI is small, so there is no manual vendor chunking or bundle
// analysis here. Output is the default dist/, which the Makefile copies into
// internal/frontdesk/webui/ for go:embed.
export default defineConfig({
	plugins: [react()],
	resolve: {
		alias: {
			"@": path.resolve(__dirname, "./src"),
			// One prefix alias for the whole cross-app module: "@web-shared/quota"
			// resolves to web-shared/quota/index.ts, "@web-shared/alerts/composers"
			// to that file. A string alias matches the bare id and anything under
			// it, so a module added to web-shared/ needs no config change here.
			"@web-shared": path.resolve(__dirname, "../../web-shared"),
		},
	},
	server: {
		// The shared modules live outside this project root, so the dev server has
		// to be allowed to serve them alongside the app's own files.
		fs: { allow: [__dirname, path.resolve(__dirname, "../../web-shared")] },
		// `pnpm dev` proxies the API to a locally running frontdesk binary so the
		// SPA and its REST/SSE backend share an origin during development.
		proxy: {
			"/api": { target: "http://localhost:8090", changeOrigin: true },
			"/traefik": { target: "http://localhost:8090", changeOrigin: true },
		},
	},
});
