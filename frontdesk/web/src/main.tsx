import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App.tsx";
import "./i18n";
import "./index.css";
import { initTheme } from "./theme";

// Before React mounts, so the login screen comes up in the persisted theme.
initTheme();

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("Root element not found");
createRoot(rootElement).render(
	<StrictMode>
		<App />
	</StrictMode>,
);
