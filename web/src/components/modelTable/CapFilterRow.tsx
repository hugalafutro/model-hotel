import { useTranslation } from "react-i18next";
import { CAP_META, type CapKey } from "../capMeta";
import { OUTPUT_META } from "../outputMeta";

/**
 * The second header row: one pill per capability and per output modality,
 * each toggling a server-side filter, plus a clear button once any is set.
 * All pills render unconditionally: filtering is server-side, so a matching
 * model may exist outside the loaded window and every pill must stay
 * reachable.
 */
export function CapFilterRow({
	capFilter,
	outputFilter,
	onToggleCap,
	onToggleOutput,
	onClear,
	showProviderCol,
}: {
	capFilter: Set<CapKey>;
	outputFilter: Set<string>;
	onToggleCap: (key: CapKey) => void;
	onToggleOutput: (key: string) => void;
	onClear: () => void;
	showProviderCol: boolean;
}) {
	const { t } = useTranslation();
	return (
		<tr className="ui-table-row-filter">
			<th className="px-4 py-2" />
			<th className="px-4 py-2">
				<span className="flex flex-wrap gap-1">
					{[
						...CAP_META.map((m) => ({
							pill: m,
							active: capFilter.has(m.key),
							onToggle: () => onToggleCap(m.key),
						})),
						...OUTPUT_META.map((m) => ({
							pill: m,
							active: outputFilter.has(m.key),
							onToggle: () => onToggleOutput(m.key),
						})),
					].map(({ pill, active, onToggle }) => (
						<button
							key={pill.labelKey}
							type="button"
							aria-pressed={active}
							onClick={onToggle}
							className={`ui-badge inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium border transition-colors ${
								active ? pill.style : pill.muted
							}`}
						>
							{t(pill.labelKey)}
						</button>
					))}
					{(capFilter.size > 0 || outputFilter.size > 0) && (
						<button
							type="button"
							onClick={onClear}
							className="ui-badge inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium text-gray-400 hover:text-gray-200"
						>
							✕
						</button>
					)}
				</span>
			</th>
			{showProviderCol && <th className="px-4 py-2" />}
			<th className="px-4 py-2" />
			<th aria-hidden />
			<th className="px-4 py-2" />
			<th aria-hidden />
			<th className="px-4 py-2" />
			<th aria-hidden />
			<th className="px-4 py-2" />
		</tr>
	);
}
