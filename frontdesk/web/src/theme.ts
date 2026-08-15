// Front Desk colour theme: dark is the built-in default, light is the
// :root[data-theme="light"] token block in index.css. The choice is persisted
// under its own localStorage key, separate from the main dashboard's, so the
// two apps' themes are independent (same split as fdLng for the language).
export const THEME_STORAGE_KEY = "fdTheme";

export type Theme = "dark" | "light";

function isTheme(value: unknown): value is Theme {
	return value === "dark" || value === "light";
}

// readStoredTheme returns the persisted choice, or dark when nothing valid is
// stored (first visit, cleared storage, or a value another build never wrote).
export function readStoredTheme(): Theme {
	try {
		const stored = localStorage.getItem(THEME_STORAGE_KEY);
		return isTheme(stored) ? stored : "dark";
	} catch {
		return "dark";
	}
}

// applyTheme stamps the choice on <html>. Dark removes the attribute rather
// than writing data-theme="dark", so the stylesheet's default block is the
// dark theme and only light needs a selector.
export function applyTheme(theme: Theme): void {
	if (theme === "light") {
		document.documentElement.setAttribute("data-theme", "light");
	} else {
		document.documentElement.removeAttribute("data-theme");
	}
}

// setTheme applies and persists in one step. Storage can be unavailable
// (private mode, quota); the theme still applies for this page load.
export function setTheme(theme: Theme): void {
	applyTheme(theme);
	try {
		localStorage.setItem(THEME_STORAGE_KEY, theme);
	} catch {
		/* ignore */
	}
}

// initTheme runs before the first render so the login screen and the shell
// paint in the persisted theme with no dark flash.
export function initTheme(): void {
	applyTheme(readStoredTheme());
}
