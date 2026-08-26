import type { Ref } from "react";
import { useTranslation } from "react-i18next";
import type { Model } from "../../api/types";
import {
	formatDate,
	formatNumber,
	formatRelativeTime,
} from "../../utils/format";
import { parseCapabilities, proxyModelID } from "../../utils/model";
import { CopyablePill } from "../CopyablePill";
import { CAP_META, hasCap } from "../capMeta";
import { OutputBadges } from "../OutputBadges";

/** One model in the virtual table. Cells match the width tables in order. */
export function ModelRow({
	model,
	index,
	measureRef,
	showProviderCol,
	onClick,
}: {
	model: Model;
	index: number;
	/** The virtualizer's measureElement, so the row reports its real height. */
	measureRef: Ref<HTMLTableRowElement>;
	showProviderCol: boolean;
	onClick?: (model: Model) => void;
}) {
	const { t } = useTranslation();
	const caps = parseCapabilities(model.capabilities);
	const isParked = !model.provider_enabled;
	const isActive = model.enabled && !model.disabled_manually;
	const isManuallyDisabled = model.enabled && model.disabled_manually;
	return (
		<tr
			data-index={index}
			ref={measureRef}
			className={`hover:bg-(--surface-hover) ${index % 2 === 1 ? "ui-row-even" : ""} ${onClick ? "cursor-pointer" : ""}`}
			onClick={() => onClick?.(model)}
		>
			<td className="px-4 py-1.5">
				<div className="flex flex-col">
					<span
						className={`text-left text-sm ${isActive ? "font-medium text-white" : "text-gray-500"}`}
					>
						{model.name || proxyModelID(model.provider_name, model.model_id)}
					</span>
					<CopyablePill
						text={proxyModelID(model.provider_name, model.model_id)}
						textClassName="text-[11px] model-id-text font-mono leading-tight"
						tooltip={t("components.modelTable.clickToCopyId")}
					/>
				</div>
			</td>
			<td className="px-4 py-1.5">
				<div className="flex flex-wrap gap-1">
					{CAP_META.filter((m) => hasCap(caps, m.key)).map((m) => (
						<span
							key={m.key}
							className={`ui-badge inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium border ${m.style}`}
						>
							{m.label}
						</span>
					))}
					<OutputBadges outputModalities={model.output_modalities} />
				</div>
			</td>
			{showProviderCol && (
				<td
					className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300 truncate"
					title={model.provider_name}
				>
					{model.provider_name}
				</td>
			)}
			<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-400">
				{formatRelativeTime(model.last_seen_at)}
			</td>
			<td aria-hidden />
			<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
				{formatNumber(model.context_length)}
			</td>
			<td aria-hidden />
			<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
				{formatNumber(model.max_output_tokens)}
			</td>
			<td aria-hidden />
			<td className="px-4 py-1.5 whitespace-nowrap">
				{isParked ? (
					<span
						className="ui-badge ui-badge-neutral px-2 py-px leading-[1.6] text-xs"
						title={t("models.status_parked_hint")}
					>
						<span className="badge-text">{t("models.status_parked")}</span>
					</span>
				) : (
					<span
						className={`ui-badge px-2 py-px leading-[1.6] text-xs ${
							isActive
								? "ui-badge-success"
								: isManuallyDisabled
									? "ui-badge-warning"
									: "ui-badge-error"
						}`}
						{...(!model.enabled && !model.disabled_manually
							? {
									title: t("models.disabledByDiscovery", {
										date: formatDate(model.last_seen_at),
									}),
									"data-testid": "disabled-by-discovery",
								}
							: {})}
					>
						<span className="badge-text">
							{isActive
								? t("common.enabled")
								: isManuallyDisabled
									? t("common.manuallyDisabled")
									: t("common.disabled")}
						</span>
					</span>
				)}
			</td>
		</tr>
	);
}
