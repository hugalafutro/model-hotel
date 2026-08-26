import path from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Module-id matcher for a vendor group. Restricting to node_modules keeps
// app code in the entry chunk, mirroring the old manualChunks behavior.
const vendor =
	(...needles: string[]) =>
	(id: string) =>
		id.includes("node_modules") && needles.some((n) => id.includes(n));

// Syntax highlighting (shiki + its oniguruma regex translator) — only ever
// loaded on demand via dynamic import; keeping it out of every group leaves
// it lazily chunked instead of riding in an eagerly-loaded vendor chunk.
const SHIKI_LAZY = [
	"/shiki/",
	"/@shikijs/",
	"/oniguruma-to-es/",
	"/oniguruma-parser/",
	"/hast-util-to-html/",
];

// https://vite.dev/config/
export default defineConfig({
	plugins: [react()],
	resolve: {
		alias: {
			"@": path.resolve(__dirname, "./src"),
			// One prefix alias for the whole cross-app module: "@web-shared/quota"
			// resolves to web-shared/quota/index.ts, "@web-shared/alerts/composers"
			// to that file. A string alias matches the bare id and anything under
			// it, so a module added to web-shared/ needs no config change here.
			"@web-shared": path.resolve(__dirname, "../web-shared"),
		},
	},
	server: {
		// The shared modules live outside this project root, so the dev server has
		// to be allowed to serve them alongside the app's own files.
		fs: { allow: [__dirname, path.resolve(__dirname, "../web-shared")] },
	},
	build: {
		// Fonts are never inlined. Vite's default turns any asset under 4 kB into
		// a base64 data: URI, which put the two smallest woff2 faces straight into
		// the CSS; the CSP is default-src 'self' with no font-src data:, so the
		// browser blocked them and fell back to the next face. Emitting them as
		// files keeps every font behind the same 'self' rule as the rest of the
		// bundle. Images keep the default: img-src allows data:.
		assetsInlineLimit: (file) => (file.endsWith(".woff2") ? false : undefined),
		// The two chunks over Vite's default 500 kB warning line are both benign:
		// the per-language Shiki grammars (cpp is the largest at ~640 kB raw but
		// ~50 kB gzip) are lazy-loaded on demand via SHIKI_LAZY below and never
		// hit initial load, and vendor-react (~530 kB raw / ~140 kB gzip) is the
		// framework core, already isolated into its own long-cache chunk. Raise
		// the limit so the build log stays clean without masking a real future
		// regression in the eager bundles.
		chunkSizeWarningLimit: 700,
		// rolldown-vite ignores rollupOptions.output.manualChunks; vendor
		// splitting must go through rolldown's codeSplitting groups. Groups
		// are matched top-down (equal priority → smaller index wins), so the
		// order below mirrors the old manualChunks if/else chain.
		rolldownOptions: {
			output: {
				codeSplitting: {
					groups: [
						// Framework core — changes least often
						{ name: "vendor-react", test: vendor("/react-dom/", "/react/") },
						// Routing + data fetching
						{
							name: "vendor-router-query",
							test: vendor("/react-router/", "/@tanstack/"),
						},
						// Internationalization
						{
							name: "vendor-i18n",
							test: vendor("/i18next/", "/react-i18next/"),
						},
						// Markdown rendering (katex is large)
						{
							name: "vendor-markdown",
							test: vendor(
								"/react-markdown/",
								"/remark-",
								"/rehype-",
								"/katex/",
							),
						},
						// Charts
						{ name: "vendor-charts", test: vendor("/recharts/") },
						// Drag and drop
						{ name: "vendor-dnd", test: vendor("/@dnd-kit/") },
						// Everything else (lucide, react-colorful, immer, etc.)
						{
							name: "vendor-misc",
							test: (id: string) =>
								id.includes("node_modules") &&
								!SHIKI_LAZY.some((n) => id.includes(n)),
						},
					],
				},
			},
		},
	},
});
