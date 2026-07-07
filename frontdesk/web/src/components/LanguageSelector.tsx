import { TranslateIcon } from "@phosphor-icons/react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { LANGUAGE_STORAGE_KEY } from "../i18n";

// Language names are autonyms (each language in its own script), shown
// identically in every UI locale — the industry standard for language pickers,
// so a user stranded in the wrong language can still recognize their own.
// English is last in the base ordering; the active language is pinned to the
// top at render time (see below).
const SUPPORTED_LANGUAGES = [
	{ code: "cs", label: "Čeština" },
	{ code: "de", label: "Deutsch" },
	{ code: "es", label: "Español" },
	{ code: "fr", label: "Français" },
	{ code: "ja", label: "日本語" },
	{ code: "nl", label: "Nederlands" },
	{ code: "pl", label: "Polski" },
	{ code: "ru", label: "Русский" },
	{ code: "sk", label: "Slovenčina" },
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
	const containerRef = useRef<HTMLDivElement>(null);
	const triggerRef = useRef<HTMLButtonElement>(null);
	const listboxRef = useRef<HTMLDivElement>(null);

	// The dropdown opens downward from the header, so pin the active language
	// to the top (nearest the trigger). This is the opposite of the main
	// app's sidebar selector, which opens upward and pins the active language
	// at the bottom nearest its trigger.
	const activeLang = i18n.resolvedLanguage ?? i18n.language;
	const languages = [
		...SUPPORTED_LANGUAGES.filter((l) => l.code === activeLang),
		...SUPPORTED_LANGUAGES.filter((l) => l.code !== activeLang),
	];

	// Close the dropdown when clicking outside it.
	useEffect(() => {
		function handleClickOutside(e: MouseEvent) {
			if (
				containerRef.current &&
				!containerRef.current.contains(e.target as Node)
			) {
				setOpen(false);
			}
		}
		if (open) {
			document.addEventListener("mousedown", handleClickOutside);
			return () =>
				document.removeEventListener("mousedown", handleClickOutside);
		}
	}, [open]);

	// When the dropdown opens, move keyboard focus to the active (top) option
	// so arrow-key navigation starts there immediately.
	useEffect(() => {
		if (open && listboxRef.current) {
			const first =
				listboxRef.current.querySelector<HTMLButtonElement>("[role='option']");
			first?.focus();
		}
	}, [open]);

	if (SUPPORTED_LANGUAGES.length <= 1) return null;

	function handleListboxKeyDown(e: React.KeyboardEvent) {
		const opts = Array.from(
			listboxRef.current?.querySelectorAll<HTMLButtonElement>(
				"[role='option']",
			) ?? [],
		);
		if (opts.length === 0) return;
		const current = opts.indexOf(document.activeElement as HTMLButtonElement);
		switch (e.key) {
			case "ArrowDown":
				e.preventDefault();
				opts[(current + 1) % opts.length]?.focus();
				break;
			case "ArrowUp":
				e.preventDefault();
				opts[(current - 1 + opts.length) % opts.length]?.focus();
				break;
			case "Escape":
				e.preventDefault();
				setOpen(false);
				triggerRef.current?.focus();
				break;
			case "Tab":
				// Let the browser move focus, then close so the open menu
				// never lingers with focus elsewhere.
				setOpen(false);
				break;
		}
	}

	return (
		<div ref={containerRef} className="fd-lang">
			<button
				ref={triggerRef}
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
				<div
					ref={listboxRef}
					className="fd-lang-menu"
					role="listbox"
					onKeyDown={handleListboxKeyDown}
				>
					<div className="fd-lang-menu-scroll">
						{languages.map((lang) => (
							<button
								key={lang.code}
								type="button"
								role="option"
								aria-selected={activeLang === lang.code}
								onClick={() => {
									// Only persist after the lazy catalog load succeeds.
									// If the chunk fails to load (network error, deploy
									// mismatch), i18next rejects and falls back to the
									// current language — saving the failed code would
									// retry and fail on every reload.
									i18n
										.changeLanguage(lang.code)
										.then(() => {
											localStorage.setItem(LANGUAGE_STORAGE_KEY, lang.code);
										})
										.catch(() => {})
										.finally(() => setOpen(false));
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
