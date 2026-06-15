import { createContext, type ReactNode, useContext, useEffect } from "react";
import { useLocalStorage } from "../hooks/useLocalStorage";
import { applyFavicon } from "../utils/favicon";

type Theme = "dark" | "light";
export type UIStyle = "clean-saas" | "cyber-terminal" | "glassmorphism-lite";

interface AccentPreset {
	name: string;
	color: string;
	lightColor: string;
}

const ACCENT_PRESETS: AccentPreset[] = [
	{ name: "theme.accent.steelBlue", color: "#546de5", lightColor: "#3b3b98" },
	{ name: "theme.accent.emerald", color: "#1dd1a1", lightColor: "#10ac84" },
	{ name: "theme.accent.gold", color: "#b8860b", lightColor: "#996515" },
	// Deep forest green (CSS forestgreen) — deliberately distinct from the
	// terminal theme's phosphor-green default (#2ed573): swatch colors must
	// never equal a THEME_DEFAULT_ACCENT value, or resetting to the theme
	// default would highlight that swatch as if the user had picked it.
	{ name: "theme.accent.forest", color: "#228b22", lightColor: "#1e7a1e" },
	{ name: "theme.accent.sky", color: "#2196f3", lightColor: "#1976d2" },
	{ name: "theme.accent.violet", color: "#a55eea", lightColor: "#8854d0" },
	{ name: "theme.accent.hotPink", color: "#e84393", lightColor: "#c2185b" },
	{ name: "theme.accent.lime", color: "#6b8e23", lightColor: "#556b2f" },
	{ name: "theme.accent.teal", color: "#00897b", lightColor: "#00695c" },
	{ name: "theme.accent.fuchsia", color: "#ff6b81", lightColor: "#e84393" },
];

// Per-theme default accents (used until the user explicitly picks one):
// phosphor green for the terminal, aqua for glass, warm copper for SaaS —
// anything but the ubiquitous default-indigo.
// eslint-disable-next-line react-refresh/only-export-components -- constant shared with AppearanceSettings' custom-swatch check
export const THEME_DEFAULT_ACCENT: Record<UIStyle, string> = {
	"clean-saas": "#e0823f",
	"cyber-terminal": "#2ed573",
	"glassmorphism-lite": "#35cfc3",
};

interface ThemeContextType {
	theme: Theme;
	setTheme: (theme: Theme) => void;
	uiStyle: UIStyle;
	setUIStyle: (style: UIStyle) => void;
	accentColor: string;
	/** True when the accent comes from an explicit user pick (persisted),
	 * false while the per-theme default applies. */
	accentIsExplicit: boolean;
	setAccentColor: (color: string) => void;
	accentPresets: AccentPreset[];
}

const ThemeContext = createContext<ThemeContextType>({
	theme: "dark",
	setTheme: () => {},
	uiStyle: "clean-saas",
	setUIStyle: () => {},
	accentColor: THEME_DEFAULT_ACCENT["clean-saas"],
	accentIsExplicit: false,
	setAccentColor: () => {},
	accentPresets: ACCENT_PRESETS,
});

function hexToHSL(hex: string): { h: number; s: number; l: number } {
	if (!/^#[0-9a-fA-F]{6}$/.test(hex)) {
		return { h: 0, s: 0, l: 50 };
	}
	const r = parseInt(hex.slice(1, 3), 16) / 255;
	const g = parseInt(hex.slice(3, 5), 16) / 255;
	const b = parseInt(hex.slice(5, 7), 16) / 255;
	const max = Math.max(r, g, b);
	const min = Math.min(r, g, b);
	let h = 0;
	let s = 0;
	const l = (max + min) / 2;
	if (max !== min) {
		const d = max - min;
		s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
		switch (max) {
			case r:
				h = ((g - b) / d + (g < b ? 6 : 0)) / 6;
				break;
			case g:
				h = ((b - r) / d + 2) / 6;
				break;
			case b:
				h = ((r - g) / d + 4) / 6;
				break;
		}
	}
	return { h: h * 360, s: s * 100, l: l * 100 };
}

function hslToRGB(h: number, s: number, l: number): [number, number, number] {
	const sn = s / 100;
	const ln = l / 100;
	const k = (n: number) => (n + h / 30) % 12;
	const a = sn * Math.min(ln, 1 - ln);
	const f = (n: number) =>
		ln - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)));
	return [
		Math.round(f(0) * 255),
		Math.round(f(8) * 255),
		Math.round(f(4) * 255),
	];
}

function applyAccentColor(color: string, theme: Theme) {
	const hsl = hexToHSL(color);
	if (Number.isNaN(hsl.h) || Number.isNaN(hsl.s) || Number.isNaN(hsl.l)) {
		return;
	}
	const root = document.documentElement;

	// Clamp lightness to ensure readability while preserving the color's character
	const minL = theme === "dark" ? 45 : 35;
	const maxL = theme === "dark" ? 80 : 60;
	const baseL = Math.max(minL, Math.min(maxL, hsl.l));

	const hoverL = Math.max(minL, Math.min(maxL, baseL + 5));
	const lightAlpha = theme === "dark" ? 0.2 : 0.15;
	const lighterAlpha = theme === "dark" ? 0.1 : 0.08;

	root.style.setProperty("--accent", `hsl(${hsl.h}, ${hsl.s}%, ${baseL}%)`);
	root.style.setProperty(
		"--accent-hover",
		`hsl(${hsl.h}, ${hsl.s}%, ${hoverL}%)`,
	);
	root.style.setProperty(
		"--accent-light",
		`hsla(${hsl.h}, ${hsl.s}%, ${baseL}%, ${lightAlpha})`,
	);
	root.style.setProperty(
		"--accent-lighter",
		`hsla(${hsl.h}, ${hsl.s}%, ${baseL}%, ${lighterAlpha})`,
	);

	// RGB triplets for rgba(var(--accent-rgb), a) consumers (terminal glows,
	// glass aurora). The alt triplet is the accent hue rotated 150deg — the
	// counter-color that gives the glass aurora its two-tone depth.
	const [r, g, b] = hslToRGB(hsl.h, hsl.s, baseL);
	root.style.setProperty("--accent-rgb", `${r}, ${g}, ${b}`);
	const [ar, ag, ab] = hslToRGB((hsl.h + 150) % 360, hsl.s, baseL);
	root.style.setProperty("--accent-rgb-alt", `${ar}, ${ag}, ${ab}`);
}

export function ThemeProvider({ children }: { children: ReactNode }) {
	const [theme, setTheme] = useLocalStorage<Theme>("theme", "dark", {
		deserialize: (v) => (v === "light" ? "light" : "dark"),
	});

	const [uiStyle, setUIStyle] = useLocalStorage<UIStyle>(
		"uiStyle",
		"clean-saas",
		{
			deserialize: (v) =>
				v === "clean-saas" ||
				v === "cyber-terminal" ||
				v === "glassmorphism-lite"
					? v
					: "clean-saas",
		},
	);

	// Empty string = "never explicitly picked": the theme default applies and
	// switching ui styles re-themes the accent until the user chooses one.
	const [storedAccent, setAccentColor] = useLocalStorage<string>(
		"accentColor",
		"",
	);
	const accentColor = storedAccent || THEME_DEFAULT_ACCENT[uiStyle];
	const accentIsExplicit = storedAccent !== "";

	useEffect(() => {
		document.documentElement.classList.remove("light", "dark");
		document.documentElement.classList.add(theme);
		document.documentElement.setAttribute("data-ui-style", uiStyle);
		applyAccentColor(accentColor, theme);
		applyFavicon(accentColor, uiStyle);
	}, [theme, uiStyle, accentColor]);

	return (
		<ThemeContext.Provider
			value={{
				theme,
				setTheme,
				uiStyle,
				setUIStyle,
				accentColor,
				accentIsExplicit,
				setAccentColor,
				accentPresets: ACCENT_PRESETS,
			}}
		>
			{children}
		</ThemeContext.Provider>
	);
}

// eslint-disable-next-line react-refresh/only-export-components
export function useTheme() {
	return useContext(ThemeContext);
}
