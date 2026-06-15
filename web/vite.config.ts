import path from "node:path";
import { codecovVitePlugin } from "@codecov/vite-plugin";
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
	plugins: [
		react(),
		// Uploads bundle-size stats to Codecov. Gated on CODECOV_TOKEN so it only
		// runs in CI (where the secret exists); local + Docker builds stay silent.
		codecovVitePlugin({
			enableBundleAnalysis: process.env.CODECOV_TOKEN !== undefined,
			bundleName: "model-hotel-web",
			uploadToken: process.env.CODECOV_TOKEN,
		}),
	],
	resolve: {
		alias: {
			"@": path.resolve(__dirname, "./src"),
		},
	},
	build: {
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
							test: vendor("/react-router-dom/", "/@tanstack/"),
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
