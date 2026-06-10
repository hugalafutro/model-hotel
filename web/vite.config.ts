import path from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// https://vite.dev/config/
export default defineConfig({
	plugins: [react()],
	resolve: {
		alias: {
			"@": path.resolve(__dirname, "./src"),
		},
	},
	build: {
		rollupOptions: {
			output: {
				manualChunks(id) {
					if (id.includes("node_modules")) {
						// Syntax highlighting (shiki + its oniguruma regex translator)
						// — only ever loaded on demand via dynamic import; leaving it
						// unassigned keeps it out of the eagerly-loaded vendor chunks.
						if (
							id.includes("/shiki/") ||
							id.includes("/@shikijs/") ||
							id.includes("/oniguruma-to-es/") ||
							id.includes("/oniguruma-parser/") ||
							id.includes("/hast-util-to-html/")
						)
							return undefined;
						// Framework core — changes least often
						if (id.includes("/react-dom/") || id.includes("/react/"))
							return "vendor-react";
						// Routing + data fetching
						if (id.includes("/react-router-dom/") || id.includes("/@tanstack/"))
							return "vendor-router-query";
						// Internationalization
						if (id.includes("/i18next/") || id.includes("/react-i18next/"))
							return "vendor-i18n";
						// Markdown rendering (katex is large)
						if (
							id.includes("/react-markdown/") ||
							id.includes("/remark-") ||
							id.includes("/rehype-") ||
							id.includes("/katex/")
						)
							return "vendor-markdown";
						// Charts
						if (id.includes("/recharts/")) return "vendor-charts";
						// Drag and drop
						if (id.includes("/@dnd-kit/")) return "vendor-dnd";
						// Everything else (lucide, react-colorful, immer, etc.)
						return "vendor-misc";
					}
				},
			},
		},
	},
});
