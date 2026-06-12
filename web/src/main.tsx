import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode, Suspense } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App.tsx";
import "./i18n";
import "./index.css";

const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			staleTime: 60 * 1000,
			refetchOnWindowFocus: false,
		},
	},
});

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("Root element not found");
createRoot(rootElement).render(
	<StrictMode>
		<QueryClientProvider client={queryClient}>
			<BrowserRouter>
				{/* Locale catalogs other than English load lazily; useTranslation
				    suspends app-wide until the active language's chunk arrives,
				    so the boundary must sit above everything that translates.
				    The fallback is deliberately translation-free. */}
				<Suspense fallback={null}>
					<App />
				</Suspense>
			</BrowserRouter>
		</QueryClientProvider>
	</StrictMode>,
);
