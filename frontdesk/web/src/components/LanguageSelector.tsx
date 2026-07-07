import { TranslateIcon } from "@phosphor-icons/react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import i18next from "i18next";
import { LANGUAGE_STORAGE_KEY } from "../i18n";

// Language names are autonyms (each language in its own script), shown
// identically in every UI locale — the industry standard for language pickers,
// so a user stranded in the wrong language can still recognize their own.
// English is intentionally last so it sits at the bottom of the menu.
const SUPPORTED_LANGUAGES = [
	{ code: "de", label: "Deutsch" },
	{ code: "es", label: "Español" },
	{ code: "fr", label: "Français" },
	{ code: "ja", label: "日本語" },
	{ code: "nl", label: "Nederlands" },
	{ code: "pl", label: "Polski" },
	{ code: "ru", label: "Русский" },
	{ code: "zh", label: "中文" },
	{ code: "en", label: "English" },
] as const;

// LanguageSelector is the header globe button + dropdown that lets the operator
// pick the UI language. The choice is persisted to localStorage (fdLng) so it
// survives reloads; the browser locale is never auto-cached (see i18n/index.ts),
// so an explicit pick always wins on the next visit until changed again.
export function LanguageSelector() {
	const { t, i18n } = useTranslation();
	const [open, setOpen] = useState(false);
	const ref = useRef<HTMLDivElement>(null);
	const scrollRef = useRef<HTMLDivElement>(null);

	// Close the dropdown when clicking outside it.
	useEffect(() => {
		function handleClickOutside(e: MouseEvent) {
			if (ref.current && !ref.current.contains(e.target as Node)) {
				setOpen(false);
			}
		}
		if (open) {
			document.addEventListener("mousedown", handleClickOutside);
			return () =>
				document.removeEventListener("mousedown", handleClickOutside);
		}
	}, [open]);

	// Scroll the active language into view when the dropdown opens.
	useEffect(() => {
		if (open && scrollRef.current) {
			const active = scrollRef.current.querySelector("[aria-selected='true']");
			active?.scrollIntoView({ block: "nearest" });
		}
	}, [open]);

	if (SUPPORTED_LANGUAGES.length <= 1) return null;

	return (
		<div ref={ref} className="fd-lang">
			<button
				type="button"
				className="fd-tab"
				onClick={() => setOpen((v) => !v)}
				title={t("layout.language.label")}
				aria-label={t("layout.language.label")}
				aria-haspopup="listbox"
				aria-expanded={open}
			>
				<TranslateIcon size={16} />
			</button>
			{open && (
				<div className="fd-lang-menu" role="listbox">
					<div ref={scrollRef} className="fd-lang-menu-scroll">
						{SUPPORTED_LANGUAGES.map((lang) => (
							<button
								key={lang.code}
								type="button"
								role="option"
								aria-selected={
									(i18n.resolvedLanguage ?? i18n.language) === lang.code
								}
								onClick={() => {
									i18next.changeLanguage(lang.code);
									// Persist every deliberate choice — including English —
									// so the effective priority is strictly
									// user choice > system locale > English. The browser
									// locale is never auto-cached (caches: [] in
									// i18n/index.ts), so an explicit pick always wins on
									// the next visit until the user changes it again.
									localStorage.setItem(LANGUAGE_STORAGE_KEY, lang.code);
									setOpen(false);
								}}
								className="fd-lang-option"
							>
								{lang.label}
							</button>
						))}
					</div>
				</div>
			)}
		</div>
	);
}
