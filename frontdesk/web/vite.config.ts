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
		alias: { "@": path.resolve(__dirname, "./src") },
	},
	server: {
		// `pnpm dev` proxies the API to a locally running frontdesk binary so the
		// SPA and its REST/SSE backend share an origin during development.
		proxy: {
			"/api": { target: "http://localhost:8090", changeOrigin: true },
			"/traefik": { target: "http://localhost:8090", changeOrigin: true },
		},
	},
});
