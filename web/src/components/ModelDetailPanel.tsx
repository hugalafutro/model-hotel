import {
	ChevronsDownUp,
	ChevronsUpDown,
	RotateCcw,
	Settings,
	X,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { GenerationParams, Model } from "../api/types";
import { formatPrice, parseCapabilities, proxyModelID } from "../utils/model";
import {
	getParamIncompatibility,
	isParamDisabled,
	isParamHidden,
} from "../utils/paramCompat";
import { ApplyRecommendedButton } from "./ApplyRecommendedButton";
import { CapBadge } from "./CapBadge";
import { CopyablePill } from "./CopyablePill";
import { CAP_META } from "./capMeta";
import { Modal } from "./Modal";
import { ParamSlider } from "./ParamSlider";
import { ReasoningEffortSelect } from "./ReasoningEffortSelect";

interface ModelDetailPanelProps {
	model: Model;
	params?: GenerationParams;
	onParamsChange?: (params: GenerationParams) => void;
	/** Optional close callback - when provided, shows an X button in the header */
	onClose?: () => void;
	/** Whether to show the collapse/expand toggle (default: true) */
	collapsible?: boolean;
	/** Tint style for the panel - "accent" applies accent border tint, "blue" applies blue border tint */
	tint?: "accent" | "blue" | "default";
	/** When true, the panel border pulses with an accent glow (e.g. while the model is generating) */
	pulseBorder?: boolean;
	/** When true, skip the outer card wrapper - used when panel is inside a Modal that already provides a card */
	embedded?: boolean;
}

export function ModelDetailPanel({
	model,
	params,
	onParamsChange,
	onClose,
	collapsible = true,
	tint = "default",
	pulseBorder = false,
	embedded = false,
}: ModelDetailPanelProps) {
	const { t } = useTranslation();
	const incompatReason = (prov: string, param: keyof GenerationParams) => {
		const key = getParamIncompatibility(prov, param);
		return key ? t(key) : undefined;
	};
	const caps = parseCapabilities(model.capabilities);
	const [open, setOpen] = useState(false);
	const [collapsed, setCollapsed] = useState(false);

	const editable = params !== undefined && onParamsChange !== undefined;
	const provider = model.provider_name;

	const hasCustom = editable
		? params.temperature !== undefined ||
			params.max_tokens !== undefined ||
			params.top_p !== undefined ||
			params.min_p !== undefined ||
			params.top_k !== undefined ||
			params.frequency_penalty !== undefined ||
			params.presence_penalty !== undefined ||
			params.reasoning_effort !== undefined
		: false;

	const tintClass =
		tint === "accent"
			? "ui-card-tint-accent"
			: tint === "blue"
				? "ui-card-tint-blue"
				: "";

	const pulseClass = pulseBorder
		? "animate-[pulse-border_2s_ease-in-out_infinite]"
		: "";

	return (
		<div
			className={`${embedded ? "" : "ui-card p-3 "}text-xs relative max-h-full ${collapsed && collapsible ? "overflow-hidden" : "overflow-y-auto"} ${tintClass} ${pulseClass}`}
		>
			{/* Header with collapse arrow + cog */}
			<div className="flex items-center justify-between">
				<div className="min-w-0">
					<h3
						className="text-sm font-semibold text-(--accent) leading-tight truncate"
						title={model.display_name || model.model_id}
					>
						{model.display_name || model.model_id}
					</h3>
				</div>
				<div className="flex items-center gap-0.5 shrink-0 ml-2">
					{!collapsed && hasCustom && (
						<button
							type="button"
							onClick={() => onParamsChange?.({})}
							className="p-1.5 rounded-md transition-all shrink-0 text-red-500/80 hover:text-red-500 hover:drop-shadow-[var(--glow-red)]"
							title={t("components.modelDetailPanel.resetParameters")}
						>
							<RotateCcw size={14} />
						</button>
					)}
					{!collapsed && editable && (
						<button
							type="button"
							onClick={() => setOpen((s) => !s)}
							className={`p-1.5 rounded-md transition-all shrink-0 ${
								open || hasCustom
									? "text-(--accent) drop-shadow-[var(--glow-accent)]"
									: "text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)]"
							}`}
							title={t("components.modelDetailPanel.generationParameters")}
						>
							<Settings size={14} />
						</button>
					)}
					{onClose && !embedded && (
						<button
							type="button"
							onClick={onClose}
							className="p-1.5 rounded-md text-(--text-tertiary) hover:text-(--text-primary) transition-colors"
							title={t("components.modal.close")}
						>
							<X size={14} />
						</button>
					)}
					{collapsible && (
						<button
							type="button"
							onClick={() => setCollapsed((c) => !c)}
							className="p-1.5 rounded-md transition-all text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)]"
							title={
								collapsed
									? t("components.modelDetailPanel.expandModelDetails")
									: t("components.modelDetailPanel.collapseModelDetails")
							}
						>
							{collapsed ? (
								<ChevronsUpDown size={14} />
							) : (
								<ChevronsDownUp size={14} />
							)}
						</button>
					)}
				</div>
			</div>
			{!collapsed && <div className="mt-2" />}
			{!collapsed && model.description && (
				<div className="max-h-[48px] overflow-y-auto">
					<p className="text-(--text-secondary) leading-[16px] m-0 text-[11px] text-justify">
						{model.description}
					</p>
				</div>
			)}

			<div
				className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
					collapsed && collapsible ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
				}`}
			>
				<div className="overflow-hidden px-2 py-2">
					<div className="space-y-3 pt-3">
						{editable && (
							<div
								className={`overflow-hidden transition-all duration-300 ease-in-out ${
									open
										? "max-h-125 opacity-100 pt-2 mt-1"
										: "max-h-0 opacity-0 pt-0 mt-0"
								}`}
							>
								<div className="space-y-2">
									{!isParamHidden(provider, "temperature") && (
										<ParamSlider
											label={t("components.modelDetailPanel.temperature")}
											value={params?.temperature}
											min={0}
											max={2}
											step={0.01}
											disabled={isParamDisabled(provider, "temperature")}
											disabledReason={incompatReason(provider, "temperature")}
											onChange={(v) =>
												onParamsChange?.({
													...(params as GenerationParams),
													temperature: v,
												})
											}
										/>
									)}
									{!isParamHidden(provider, "max_tokens") && (
										<ParamSlider
											label={t("components.modelDetailPanel.maxTokens")}
											value={params?.max_tokens}
											min={1}
											max={32768}
											step={1}
											disabled={isParamDisabled(provider, "max_tokens")}
											disabledReason={incompatReason(provider, "max_tokens")}
											onChange={(v) =>
												onParamsChange?.({
													...(params as GenerationParams),
													max_tokens:
														v === undefined ? undefined : Math.round(v),
												})
											}
										/>
									)}
									{!isParamHidden(provider, "top_p") && (
										<ParamSlider
											label={t("components.modelDetailPanel.topP")}
											value={params?.top_p}
											min={0}
											max={1}
											step={0.01}
											disabled={isParamDisabled(provider, "top_p")}
											disabledReason={incompatReason(provider, "top_p")}
											onChange={(v) =>
												onParamsChange?.({
													...(params as GenerationParams),
													top_p: v,
												})
											}
										/>
									)}
									{!isParamHidden(provider, "min_p") && (
										<ParamSlider
											label={t("components.modelDetailPanel.minP")}
											value={params?.min_p}
											min={0}
											max={1}
											step={0.01}
											disabled={isParamDisabled(provider, "min_p")}
											disabledReason={incompatReason(provider, "min_p")}
											onChange={(v) =>
												onParamsChange?.({
													...(params as GenerationParams),
													min_p: v,
												})
											}
										/>
									)}
									{!isParamHidden(provider, "top_k") && (
										<ParamSlider
											label={t("components.modelDetailPanel.topK")}
											value={params?.top_k}
											min={1}
											max={100}
											step={1}
											disabled={isParamDisabled(provider, "top_k")}
											disabledReason={incompatReason(provider, "top_k")}
											onChange={(v) =>
												onParamsChange?.({
													...(params as GenerationParams),
													top_k: v === undefined ? undefined : Math.round(v),
												})
											}
										/>
									)}
									{!isParamHidden(provider, "frequency_penalty") && (
										<ParamSlider
											label={t("components.modelDetailPanel.freqPenalty")}
											value={params?.frequency_penalty}
											min={-2}
											max={2}
											step={0.01}
											disabled={isParamDisabled(provider, "frequency_penalty")}
											disabledReason={incompatReason(
												provider,
												"frequency_penalty",
											)}
											onChange={(v) =>
												onParamsChange?.({
													...(params as GenerationParams),
													frequency_penalty: v,
												})
											}
										/>
									)}
									{!isParamHidden(provider, "presence_penalty") && (
										<ParamSlider
											label={t("components.modelDetailPanel.presPenalty")}
											value={params?.presence_penalty}
											min={-2}
											max={2}
											step={0.01}
											disabled={isParamDisabled(provider, "presence_penalty")}
											disabledReason={incompatReason(
												provider,
												"presence_penalty",
											)}
											onChange={(v) =>
												onParamsChange?.({
													...(params as GenerationParams),
													presence_penalty: v,
												})
											}
										/>
									)}
								</div>
								{caps.reasoning &&
									!isParamHidden(provider, "reasoning_effort") && (
										<ReasoningEffortSelect
											value={params?.reasoning_effort}
											onChange={(v) =>
												onParamsChange?.({
													...(params as GenerationParams),
													reasoning_effort: v,
												})
											}
										/>
									)}
								{/* Recommended settings button */}
								<ApplyRecommendedButton
									modelId={proxyModelID(model.provider_name, model.model_id)}
									providerName={model.provider_name}
									onApply={(recommended) =>
										onParamsChange?.({
											...(params as GenerationParams),
											...recommended,
										})
									}
								/>
							</div>
						)}

						<div className="space-y-2">
							<div>
								<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
									{t("components.modelDetailPanel.provider")}
								</span>
								<div className="text-(--text-primary) font-medium">
									{model.provider_name}
								</div>
							</div>
							<div>
								<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
									{t("components.modelDetailPanel.modelId")}
								</span>
								<div
									className="text-(--text-primary) font-medium truncate"
									title={model.model_id}
								>
									{model.model_id}
								</div>
							</div>
							<div className="grid grid-cols-2 gap-2">
								<div>
									<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
										{t("components.modelDetailPanel.context")}
									</span>
									<div className="text-(--text-primary) font-medium">
										{model.context_length?.toLocaleString() ?? "-"}
									</div>
								</div>
								<div>
									<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
										{t("components.modelDetailPanel.maxOut")}
									</span>
									<div className="text-(--text-primary) font-medium">
										{model.max_output_tokens?.toLocaleString() ?? "-"}
									</div>
								</div>
							</div>
							<div className="grid grid-cols-2 gap-2">
								<div>
									<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
										{t("components.modelDetailPanel.inputPricePerMillion")}
									</span>
									<div className="text-(--text-primary) font-medium">
										${formatPrice(model.input_price_per_million)}
									</div>
								</div>
								<div>
									<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
										{t("components.modelDetailPanel.outputPricePerMillion")}
									</span>
									<div className="text-(--text-primary) font-medium">
										${formatPrice(model.output_price_per_million)}
									</div>
								</div>
							</div>
						</div>

						{CAP_META.some((m) => caps[m.key]) && (
							<div>
								<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
									{t("components.modelDetailPanel.capabilities")}
								</span>
								<div className="flex flex-wrap gap-1 mt-1">
									{CAP_META.filter((m) => caps[m.key]).map((m) => (
										<CapBadge key={m.key} caps={caps} capKey={m.key} />
									))}
								</div>
							</div>
						)}

						<div>
							<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
								{t("components.modelDetailPanel.proxyId")}
							</span>
							<CopyablePill
								text={proxyModelID(model.provider_name, model.model_id)}
								textClassName="text-[10px] text-(--text-secondary) break-all whitespace-normal font-mono"
							/>
						</div>
					</div>
				</div>
			</div>
		</div>
	);
}

interface ModelDetailModalProps {
	model: Model;
	onClose: () => void;
	/** Whether the panel can be collapsed (default: false) */
	collapsible?: boolean;
}

export function ModelDetailModal({
	model,
	onClose,
	collapsible = false,
}: ModelDetailModalProps) {
	return (
		<Modal onClose={onClose} maxWidth="max-w-sm" zIndex="z-60" scrollable>
			<ModelDetailPanel
				model={model}
				onClose={onClose}
				collapsible={collapsible}
				embedded
			/>
		</Modal>
	);
}
