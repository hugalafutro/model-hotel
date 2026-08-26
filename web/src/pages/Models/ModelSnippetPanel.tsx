import { useTranslation } from "react-i18next";
import { LangIcon, type LangIconKey } from "../../components/langIcons";
import { ShikiCode } from "../../components/ShikiCode";
import { TerminalPreview } from "../../components/TerminalPreview";
import type { SnippetEntry } from "./modelSnippets";

/** The snippet tabs and the highlighted, copyable snippet for the active one. */
export function ModelSnippetPanel({
	entries,
	activeKey,
	onSelect,
	highlights,
}: {
	entries: SnippetEntry[];
	activeKey: LangIconKey;
	onSelect: (key: LangIconKey) => void;
	highlights: string[];
}) {
	const { t } = useTranslation();
	const active = entries.find((e) => e.key === activeKey) ?? entries[0];
	return (
		<div className="mt-4 pt-4">
			<div
				role="tablist"
				aria-label={t("models.detail.snippetFormatPicker")}
				className="flex items-center gap-1 mb-3"
			>
				{entries.map((entry) => (
					<button
						key={entry.key}
						type="button"
						role="tab"
						aria-selected={activeKey === entry.key}
						onClick={() => onSelect(entry.key)}
						className={`ui-tab p-1.5 transition-all ${
							activeKey === entry.key
								? "bg-slate-700/30 border border-slate-600/30"
								: "text-slate-500 hover:text-slate-400 border border-transparent"
						}`}
						title={entry.title}
						aria-label={entry.title}
					>
						<LangIcon name={entry.key} size={16} />
					</button>
				))}
			</div>
			<TerminalPreview
				variant="code"
				title={active.title}
				icon={active.key}
				copyText={active.copyText}
				height={200}
			>
				<ShikiCode
					code={active.copyText}
					lang={active.lang}
					highlights={highlights}
				/>
			</TerminalPreview>
		</div>
	);
}
