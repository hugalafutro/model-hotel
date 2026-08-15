import { MoonIcon, SunIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { readStoredTheme, setTheme, type Theme } from "../theme";

// ThemeToggle is the header button beside the language picker that flips the
// control plane between the dark and light token sets. The icon shows the
// theme a click switches TO (sun while dark, moon while light), and the label
// says so in words, since the icon alone is ambiguous either way.
export function ThemeToggle() {
	const { t } = useTranslation();
	const [theme, setThemeState] = useState<Theme>(readStoredTheme);
	const next: Theme = theme === "dark" ? "light" : "dark";
	const label =
		next === "light" ? t("layout.theme.toLight") : t("layout.theme.toDark");

	return (
		<button
			type="button"
			className="fd-tab"
			onClick={() => {
				setTheme(next);
				setThemeState(next);
			}}
			title={label}
			aria-label={label}
			data-testid="theme-toggle"
		>
			{next === "light" ? <SunIcon size={16} /> : <MoonIcon size={16} />}
		</button>
	);
}
