import { useTranslation } from "react-i18next";
import {
	ArrowDownToLine,
	ArrowUpFromLine,
	Clock,
	Coins,
	DollarSign,
	Hash,
	Layers,
	Server,
	Tag,
} from "@/lib/icons";
import type { Model } from "../../api/types";
import { CopyButton } from "../../components/CopyButton";
import { DetailItem } from "../../components/LogDetailItem";
import { formatNumber, formatRelativeTime } from "../../utils/format";
import { formatPrice, formatPriceInput } from "../../utils/model";
import type { useModelEditor } from "./useModelEditor";

type Editor = ReturnType<typeof useModelEditor>;

/** Small revert button that restores a field to its discovered default value. */
function RevertButton({
	onClick,
	className,
}: {
	onClick: () => void;
	className?: string;
}) {
	const { t } = useTranslation();
	return (
		<button
			type="button"
			onClick={onClick}
			className={`text-[10px] px-1.5 py-0.5 rounded-(--radius-button) bg-gray-700 text-gray-400 hover:text-white border border-gray-600 ${className ?? ""}`}
			title={t("model.revertToDiscoveredValue")}
			aria-label={t("model.revertToDiscoveredValue")}
		>
			↩
		</button>
	);
}

/**
 * The two-column facts grid: provider, discovery time, context and output
 * limits, prices and modalities. In edit mode the editable cells show their
 * inputs, each with a revert button once it differs from the discovered value.
 */
export function ModelStatsGrid({
	model,
	inputMods,
	outputMods,
	editing,
	editData,
	setEditData,
	discoveredDefaults,
	revertField,
}: {
	model: Model;
	inputMods: string[];
	outputMods: string[];
	editing: boolean;
	editData: Editor["editData"];
	setEditData: Editor["setEditData"];
	discoveredDefaults: Editor["discoveredDefaults"];
	revertField: Editor["revertField"];
}) {
	const { t } = useTranslation();
	const priceEditor = (
		field: "input_price_per_million" | "output_price_per_million",
	) => (
		<div className="flex items-center gap-1">
			<div className="relative w-full">
				<input
					type="number"
					step="0.01"
					min={0}
					max={1000}
					value={editData[field]}
					onChange={(e) =>
						setEditData((prev) => ({ ...prev, [field]: e.target.value }))
					}
					className="ui-input text-sm pr-16!"
					placeholder={t("models.detail.placeholder.price")}
				/>
				<span className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] text-gray-400">
					{t("models.detail.perMillionTokens")}
				</span>
			</div>
			{editData[field] !== formatPriceInput(discoveredDefaults[field]) && (
				<RevertButton onClick={() => revertField(field)} className="shrink-0" />
			)}
		</div>
	);
	return (
		<div className="grid grid-cols-2 gap-2 mb-4">
			<DetailItem
				icon={Server}
				label={t("models.detail.provider")}
				value={model.provider_name}
			/>
			<DetailItem
				icon={Clock}
				label={t("models.detail.lastDiscovered")}
				value={formatRelativeTime(model.last_seen_at)}
			/>
			{/* Display name is shown in big letters in the modal header, so the
			    view-mode pill would be redundant; only surface it as an editable
			    field when editing. */}
			{editing && (
				<DetailItem
					icon={Tag}
					label={t("models.detail.displayName")}
					className="col-span-2"
				>
					<div className="flex items-center gap-1">
						<input
							type="text"
							maxLength={128}
							value={editData.display_name}
							onChange={(e) =>
								setEditData((prev) => ({
									...prev,
									display_name: e.target.value,
								}))
							}
							className="ui-input text-sm"
						/>
						{editData.display_name !== discoveredDefaults.display_name && (
							<RevertButton onClick={() => revertField("display_name")} />
						)}
					</div>
				</DetailItem>
			)}
			<DetailItem
				emphasis="stat"
				icon={Layers}
				label={t("models.detail.contextLength")}
				value={`${formatNumber(model.context_length)} ${t("models.detail.tokens")}`}
				mono
				labelExtra={
					model.context_length != null ? (
						<CopyButton
							text={String(model.context_length)}
							title={t("models.detail.copyRawValue")}
						/>
					) : undefined
				}
			>
				{editing ? (
					<div className="flex items-center gap-1">
						<input
							type="number"
							min={256}
							max={2000000}
							value={editData.context_length}
							onChange={(e) =>
								setEditData((prev) => ({
									...prev,
									context_length: e.target.value,
								}))
							}
							className="ui-input text-sm"
							placeholder={t("models.detail.tokens")}
						/>
						{editData.context_length !==
							(discoveredDefaults.context_length?.toString() ?? "") && (
							<RevertButton onClick={() => revertField("context_length")} />
						)}
					</div>
				) : undefined}
			</DetailItem>
			<DetailItem
				emphasis="stat"
				icon={Hash}
				label={t("models.detail.maxOutput")}
				value={`${formatNumber(model.max_output_tokens)} ${t("models.detail.tokens")}`}
				mono
				labelExtra={
					model.max_output_tokens != null ? (
						<CopyButton
							text={String(model.max_output_tokens)}
							title={t("models.detail.copyRawValue")}
						/>
					) : undefined
				}
			>
				{editing ? (
					<div className="flex items-center gap-1">
						<input
							type="number"
							min={1}
							max={128000}
							value={editData.max_output_tokens}
							onChange={(e) =>
								setEditData((prev) => ({
									...prev,
									max_output_tokens: e.target.value,
								}))
							}
							className="ui-input text-sm"
							placeholder={t("models.detail.tokens")}
						/>
						{editData.max_output_tokens !==
							(discoveredDefaults.max_output_tokens?.toString() ?? "") && (
							<RevertButton onClick={() => revertField("max_output_tokens")} />
						)}
					</div>
				) : undefined}
			</DetailItem>
			<DetailItem
				emphasis="stat"
				icon={DollarSign}
				label={t("models.detail.inputPrice")}
				value={
					model.input_price_per_million != null
						? `$${formatPrice(model.input_price_per_million)}/1M`
						: "-"
				}
				mono
			>
				{editing ? priceEditor("input_price_per_million") : undefined}
			</DetailItem>
			<DetailItem
				emphasis="stat"
				icon={Coins}
				label={t("models.detail.outputPrice")}
				value={
					model.output_price_per_million != null
						? `$${formatPrice(model.output_price_per_million)}/1M`
						: "-"
				}
				mono
			>
				{editing ? priceEditor("output_price_per_million") : undefined}
			</DetailItem>
			<DetailItem
				icon={ArrowDownToLine}
				label={t("models.detail.input")}
				value={inputMods.join(", ") || t("models.detail.modality.text")}
			/>
			<DetailItem
				icon={ArrowUpFromLine}
				label={t("models.detail.output")}
				value={outputMods.join(", ") || t("models.detail.modality.text")}
			/>
		</div>
	);
}
