import { IconContext, type IconWeight } from "@phosphor-icons/react";
import type { ReactNode } from "react";
import { type UIStyle, useTheme } from "@/context/ThemeContext";

// Per-theme icon weight. This is the single place to restyle every icon for a
// theme at once - change a value here and that theme's whole icon set shifts
// weight (thin | light | regular | bold | fill | duotone). Individual icons can
// still override locally via their own `weight` prop.
const THEME_ICON_WEIGHT: Record<UIStyle, IconWeight> = {
	"clean-saas": "duotone",
	"cyber-terminal": "bold",
	"glassmorphism-lite": "light",
};

const DEFAULT_WEIGHT: IconWeight = "regular";

// ThemedIconProvider sets the default Phosphor icon weight for the active theme.
// Mounted inside ThemeProvider so it can read the current uiStyle.
export function ThemedIconProvider({ children }: { children: ReactNode }) {
	const { uiStyle } = useTheme();
	const weight = THEME_ICON_WEIGHT[uiStyle] ?? DEFAULT_WEIGHT;
	return (
		<IconContext.Provider value={{ weight }}>{children}</IconContext.Provider>
	);
}
