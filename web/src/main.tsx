import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode, Suspense } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App.tsx";
import "./i18n";
// Per-theme typefaces (variable woff2, self-hosted — no CDN fetches):
// Schibsted Grotesk = clean-saas, Onest = glassmorphism,
// JetBrains Mono = cyber-terminal. Wired via --font-sans/--font-mono.
import "@fontsource-variable/schibsted-grotesk/index.css";
import "@fontsource-variable/onest/index.css";
import "@fontsource-variable/jetbrains-mono/index.css";
import "./index.css";

const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			staleTime: 60 * 1000,
			refetchOnWindowFocus: false,
		},
	},
});

// While the active language's catalog chunk loads, ThemeProvider (inside
// App) hasn't run yet, so no theme CSS applies — a null fallback would
// flash an unstyled white page in a dark room. Read the stored theme
// synchronously and paint a matching blank surface; deliberately
// translation-free (the catalog is exactly what we're waiting for).
function bootSurface(): React.ReactElement {
	let dark = true;
	try {
		dark = localStorage.getItem("theme") !== "light";
	} catch {
		/* private mode etc. — dark is the safer default */
	}
	return (
		<div
			style={{ minHeight: "100vh", background: dark ? "#0b0c0f" : "#f9fafb" }}
		/>
	);
}

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("Root element not found");
createRoot(rootElement).render(
	<StrictMode>
		<QueryClientProvider client={queryClient}>
			<BrowserRouter>
				{/* Locale catalogs other than English load lazily; useTranslation
				    suspends app-wide until the active language's chunk arrives,
				    so the boundary must sit above everything that translates. */}
				<Suspense fallback={bootSurface()}>
					<App />
				</Suspense>
			</BrowserRouter>
		</QueryClientProvider>
	</StrictMode>,
);
