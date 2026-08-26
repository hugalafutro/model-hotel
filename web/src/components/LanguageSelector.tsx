import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Languages } from "@/lib/icons";
import i18next, { LANGUAGE_STORAGE_KEY } from "../i18n";
import { CountryFlag } from "./CountryFlag";

// Language names are autonyms (each language in its own script), shown
// identically in every UI locale — the industry standard for language pickers,
// so a user stranded in the wrong language can still recognize their own.
// English is intentionally last so it sits at the bottom of the upward-opening
// menu (nearest the trigger) in every locale.
const SUPPORTED_LANGUAGES = [
	{ code: "af", label: "Afrikaans" },
	{ code: "ar", label: "العربية" },
	{ code: "ca", label: "Català" },
	{ code: "cs", label: "Čeština" },
	{ code: "da", label: "Dansk" },
	{ code: "de", label: "Deutsch" },
	{ code: "el", label: "Ελληνικά" },
	{ code: "es", label: "Español" },
	{ code: "fi", label: "Suomi" },
	{ code: "fr", label: "Français" },
	{ code: "he", label: "עברית" },
	{ code: "hu", label: "Magyar" },
	{ code: "it", label: "Italiano" },
	{ code: "ja", label: "日本語" },
	{ code: "ko", label: "한국어" },
	{ code: "nl", label: "Nederlands" },
	{ code: "no", label: "Norsk" },
	{ code: "pl", label: "Polski" },
	{ code: "pt", label: "Português" },
	{ code: "ro", label: "Română" },
	{ code: "ru", label: "Русский" },
	{ code: "sk", label: "Slovenčina" },
	{ code: "sr", label: "Српски" },
	{ code: "sv", label: "Svenska" },
	{ code: "tr", label: "Türkçe" },
	{ code: "uk", label: "Українська" },
	{ code: "vi", label: "Tiếng Việt" },
	{ code: "zh", label: "中文" },
	{ code: "en", label: "English" },
] as const;

export function LanguageSelector() {
	const { t, i18n } = useTranslation();
	const [open, setOpen] = useState(false);
	const ref = useRef<HTMLDivElement>(null);
	const scrollRef = useRef<HTMLDivElement>(null);

	// Set document direction for RTL languages
	useEffect(() => {
		const rtlLanguages = new Set(["ar", "he"]);
		const lang = i18n.resolvedLanguage as string;
		document.documentElement.dir = rtlLanguages.has(lang) ? "rtl" : "ltr";
	}, [i18n.resolvedLanguage]);

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

	// Scroll the active language into view when dropdown opens
	useEffect(() => {
		if (open && scrollRef.current) {
			const active = scrollRef.current.querySelector("[aria-selected='true']");
			active?.scrollIntoView({ block: "nearest" });
		}
	}, [open]);

	if (SUPPORTED_LANGUAGES.length <= 1) return null;

	return (
		<div ref={ref} className="relative">
			<button
				type="button"
				onClick={() => setOpen((v) => !v)}
				className="sidebar-footer-link text-gray-400 hover:text-white ui-btn hover:bg-white/5"
				title={t("layout.language.label")}
				aria-label={t("layout.language.label")}
				data-testid="language-trigger"
			>
				<Languages size={14} strokeWidth={2} />
			</button>
			{open && (
				// Outer wrapper owns the rounding + border and clips its overflow so
				// the inner scrollbar stays inside the rounded corners instead of
				// painting over them. The scroll lives on the inner element.
				<div className="ui-popover absolute bottom-full left-1/2 -translate-x-1/2 mb-1 min-w-[120px] bg-gray-800 border border-gray-700 rounded-(--radius-card) shadow-lg z-50 overflow-hidden">
					<div
						ref={scrollRef}
						className="py-1 max-h-[50vh] overflow-y-auto overscroll-contain"
						role="listbox"
					>
						{SUPPORTED_LANGUAGES.map((lang) => (
							<button
								key={lang.code}
								type="button"
								role="option"
								aria-selected={
									(i18n.resolvedLanguage ?? i18n.language) === lang.code
								}
								data-testid={`language-option-${lang.code}`}
								id={`language-option-${lang.code}`}
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
								className={`w-full text-left px-3 py-1.5 text-xs transition-colors flex items-center gap-1.5 ${
									(i18n.resolvedLanguage ?? i18n.language) === lang.code
										? "text-white bg-white/10"
										: "text-gray-400 hover:text-white hover:bg-white/5"
								}`}
							>
								<CountryFlag code={lang.code} />
								{lang.label}
							</button>
						))}
					</div>
				</div>
			)}
		</div>
	);
}
