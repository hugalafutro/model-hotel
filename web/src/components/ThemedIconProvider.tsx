import { IconContext, type IconWeight } from "@phosphor-icons/react";
import { type ReactNode, useMemo } from "react";
import { type UIStyle, useTheme } from "@/context/ThemeContext";

// Per-theme icon weight. This is the single place to restyle every icon for a
// theme at once - change a value here and that theme's whole icon set shifts
// weight (thin | light | regular | bold | fill | duotone). The map is exhaustive
// over UIStyle, so adding a theme is a compile error until it gets a weight.
// Individual icons can still override locally via their own `weight` prop.
const THEME_ICON_WEIGHT: Record<UIStyle, IconWeight> = {
	"clean-saas": "duotone",
	"cyber-terminal": "bold",
	"glassmorphism-lite": "light",
};

// ThemedIconProvider sets the default Phosphor icon weight for the active theme.
// Mounted inside ThemeProvider so it can read the current uiStyle. The context
// value is memoized on the weight so unrelated ThemeProvider re-renders (accent,
// dark/light) don't push a new object and re-render every icon in the tree.
export function ThemedIconProvider({ children }: { children: ReactNode }) {
	const { uiStyle } = useTheme();
	const value = useMemo(
		() => ({ weight: THEME_ICON_WEIGHT[uiStyle] }),
		[uiStyle],
	);
	return <IconContext.Provider value={value}>{children}</IconContext.Provider>;
}
