import { Fragment } from "react";
import { useTranslation } from "react-i18next";
import { CollapsibleToggle, useCollapsible } from "../CollapsibleToggle";
import { CAP_META, type CapKey } from "../capMeta";
import { OUTPUT_ICON_BADGE, OutputIcon } from "../OutputBadges";
import { OUTPUT_META, type OutputMeta } from "../outputMeta";

/** Pills shown while a strip is collapsed (both metas are ordered most-common first). */
const COLLAPSED_PILLS = 3;

export interface FilterPill {
	key: string;
	label: string;
	className: string;
	active: boolean;
	disabled?: boolean;
	/** Renders the pill as this output's icon badge, the label moving to its tooltip. */
	icon?: OutputMeta;
	onToggle: () => void;
}

/**
 * A filter pill strip shared by both model tables' capability and output
 * cells. Starts collapsed to the first three pills behind an expand toggle;
 * expanded, the rest start on a new line below them. The strip wraps in both
 * states, so a locale with long labels grows the header row downwards instead
 * of spilling into the next column. A filter
 * set on a hidden pill still shows the clear button. Unselected pills go grey
 * once any is set, so the set ones stand out.
 */
export function PillStrip({
	pills,
	label,
	storageKey,
	showClear = false,
	onClear,
}: {
	pills: FilterPill[];
	/** The column, named in the toggle's label so each strip's toggle is told apart. */
	label: string;
	/** Where the expanded/collapsed choice persists. */
	storageKey: string;
	showClear?: boolean;
	onClear?: () => void;
}) {
	const { t } = useTranslation();
	const { collapsed, toggle } = useCollapsible(storageKey, true);
	const anyActive = pills.some((p) => p.active);
	const visible = collapsed ? pills.slice(0, COLLAPSED_PILLS) : pills;
	return (
		<span className="flex flex-wrap items-center gap-1">
			{pills.length > COLLAPSED_PILLS && (
				<CollapsibleToggle
					collapsed={collapsed}
					onToggle={toggle}
					expandTitle={t("common.expandNamed", { name: label })}
					collapseTitle={t("common.collapseNamed", { name: label })}
					ariaLabel={label}
					iconStyle="double"
					size={12}
					className="ui-icon-btn p-0.5 rounded-md shrink-0"
				/>
			)}
			{visible.map((p, i) => (
				<Fragment key={p.key}>
					{i === COLLAPSED_PILLS && (
						<span aria-hidden className="basis-full h-0" />
					)}
					<button
						type="button"
						disabled={p.disabled}
						aria-pressed={p.active}
						data-dimmed={anyActive && !p.active ? "" : undefined}
						{...(p.icon ? { "aria-label": p.label, title: p.label } : {})}
						onClick={p.onToggle}
						className={`${p.icon ? OUTPUT_ICON_BADGE : "ui-badge inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium border"} transition-[color,background-color,border-color,filter] data-dimmed:grayscale data-dimmed:hover:grayscale-0 ${p.className}`}
					>
						{p.icon ? <OutputIcon meta={p.icon} /> : p.label}
					</button>
				</Fragment>
			))}
			{showClear && (
				<button
					type="button"
					onClick={onClear}
					aria-label={t("common.clearFilter")}
					title={t("common.clearFilter")}
					className="ui-badge inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium text-gray-400 hover:text-gray-200"
				>
					✕
				</button>
			)}
		</span>
	);
}

/**
 * The Output column's filter cell: one icon toggle per output modality, text
 * included, collapsed to text/image/audio like the capability strip.
 */
export function OutputFilterIcons({
	metas,
	active,
	onToggle,
}: {
	metas: OutputMeta[];
	active: Set<string>;
	onToggle: (key: string) => void;
}) {
	const { t } = useTranslation();
	return (
		<PillStrip
			label={t("models.table.outputs")}
			storageKey="modelTable.outputPillsCollapsed"
			pills={metas.map((m) => {
				const on = active.has(m.key);
				return {
					key: m.key,
					label: t(m.labelKey),
					className: on ? m.style : m.muted,
					active: on,
					icon: m,
					onToggle: () => onToggle(m.key),
				};
			})}
		/>
	);
}

/**
 * The second header row: one pill per capability and one icon per output
 * modality, each toggling a server-side filter, plus a clear button once any
 * is set. Everything renders unconditionally: filtering is server-side, so a
 * matching model may exist outside the loaded window and every toggle must
 * stay reachable.
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
			<th className="px-4 py-2 align-top">
				<PillStrip
					label={t("models.table.capabilities")}
					storageKey="modelTable.capPillsCollapsed"
					pills={CAP_META.map((m) => {
						const active = capFilter.has(m.key);
						return {
							key: m.key,
							label: t(m.labelKey),
							className: active ? m.style : m.muted,
							active,
							onToggle: () => onToggleCap(m.key),
						};
					})}
					showClear={capFilter.size > 0 || outputFilter.size > 0}
					onClear={onClear}
				/>
			</th>
			<th className="px-2 py-2 align-top">
				<OutputFilterIcons
					metas={OUTPUT_META}
					active={outputFilter}
					onToggle={onToggleOutput}
				/>
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
