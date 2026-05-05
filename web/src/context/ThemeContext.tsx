import { createContext, type ReactNode, useContext, useEffect } from "react";
import { useLocalStorage } from "../hooks/useLocalStorage";

type Theme = "dark" | "light";
type UIStyle = "clean-saas" | "cyber-terminal" | "glassmorphism-lite";

interface AccentPreset {
	name: string;
	color: string;
	lightColor: string;
}

const ACCENT_PRESETS: AccentPreset[] = [
	{ name: "Steel Blue", color: "#546de5", lightColor: "#3b3b98" },
	{ name: "Emerald", color: "#1dd1a1", lightColor: "#10ac84" },
	{ name: "Gold", color: "#b8860b", lightColor: "#996515" },
	{ name: "Forest", color: "#2ed573", lightColor: "#218c74" },
	{ name: "Sky", color: "#2196f3", lightColor: "#1976d2" },
	{ name: "Violet", color: "#a55eea", lightColor: "#8854d0" },
	{ name: "Hot Pink", color: "#e84393", lightColor: "#c2185b" },
	{ name: "Lime", color: "#6b8e23", lightColor: "#556b2f" },
	{ name: "Teal", color: "#00897b", lightColor: "#00695c" },
	{ name: "Fuchsia", color: "#ff6b81", lightColor: "#e84393" },
];

interface ThemeContextType {
	theme: Theme;
	setTheme: (theme: Theme) => void;
	uiStyle: UIStyle;
	setUIStyle: (style: UIStyle) => void;
	accentColor: string;
	setAccentColor: (color: string) => void;
	accentPresets: AccentPreset[];
}

const ThemeContext = createContext<ThemeContextType>({
	theme: "dark",
	setTheme: () => {},
	uiStyle: "clean-saas",
	setUIStyle: () => {},
	accentColor: "#546de5",
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

	const [accentColor, setAccentColor] = useLocalStorage<string>(
		"accentColor",
		"#546de5",
	);

	useEffect(() => {
		document.documentElement.classList.remove("light", "dark");
		document.documentElement.classList.add(theme);
		document.documentElement.setAttribute("data-ui-style", uiStyle);
		applyAccentColor(accentColor, theme);
	}, [theme, uiStyle, accentColor]);

	return (
		<ThemeContext.Provider
			value={{
				theme,
				setTheme,
				uiStyle,
				setUIStyle,
				accentColor,
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
