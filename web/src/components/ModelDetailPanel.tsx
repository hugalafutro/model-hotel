import {
	ChevronsDownUp,
	ChevronsUpDown,
	RotateCcw,
	Settings,
	X,
} from "lucide-react";
import { useState } from "react";
import type { GenerationParams, Model } from "../api/types";
import { useToast } from "../context/ToastContext";
import { formatPrice, parseCapabilities, proxyModelID } from "../utils/model";
import { getParamIncompatibility, isParamDisabled } from "../utils/paramCompat";
import { ApplyRecommendedButton } from "./ApplyRecommendedButton";
import { CapBadge } from "./CapBadge";
import { CAP_META } from "./capMeta";
import { Modal } from "./Modal";
import { ParamSlider } from "./ParamSlider";

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
	const caps = parseCapabilities(model.capabilities);
	const [open, setOpen] = useState(false);
	const [collapsed, setCollapsed] = useState(false);

	const editable = params !== undefined && onParamsChange !== undefined;
	const { toast } = useToast();
	const provider = model.provider_name;
	const proxyId = proxyModelID(model.provider_name, model.model_id);

	const hasCustom = editable
		? params.temperature !== undefined ||
			params.max_tokens !== undefined ||
			params.top_p !== undefined ||
			params.min_p !== undefined ||
			params.top_k !== undefined ||
			params.frequency_penalty !== undefined ||
			params.presence_penalty !== undefined
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
			className={`${embedded ? "" : "ui-card p-3 "}text-xs relative overflow-y-auto max-h-full ${tintClass} ${pulseClass}`}
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
							className="p-1.5 rounded-md transition-all cursor-pointer shrink-0 text-red-500/80 hover:text-red-500 hover:drop-shadow-[var(--glow-red)]"
							title="Reset parameters"
						>
							<RotateCcw size={14} />
						</button>
					)}
					{!collapsed && editable && (
						<button
							type="button"
							onClick={() => setOpen((s) => !s)}
							className={`p-1.5 rounded-md transition-all cursor-pointer shrink-0 ${
								open || hasCustom
									? "text-(--accent) drop-shadow-[var(--glow-accent)]"
									: "text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)]"
							}`}
							title="Generation parameters"
						>
							<Settings size={14} />
						</button>
					)}
					{onClose && !embedded && (
						<button
							type="button"
							onClick={onClose}
							className="p-1.5 rounded-md cursor-pointer text-(--text-tertiary) hover:text-(--text-primary) transition-colors"
							title="Close"
						>
							<X size={14} />
						</button>
					)}
					{collapsible && (
						<button
							type="button"
							onClick={() => setCollapsed((c) => !c)}
							className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)]"
							title={
								collapsed ? "Expand model details" : "Collapse model details"
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
			{!collapsed && model.description && (
				<p
					className="text-(--text-secondary) mt-1 line-clamp-10 text-[11px] text-justify"
					title={model.description}
				>
					{model.description}
				</p>
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
									<ParamSlider
										label="Temperature"
										value={params?.temperature}
										min={0}
										max={2}
										step={0.01}
										disabled={isParamDisabled(provider, "temperature")}
										disabledReason={
											getParamIncompatibility(provider, "temperature") ??
											undefined
										}
										onChange={(v) =>
											onParamsChange?.({
												...(params as GenerationParams),
												temperature: v,
											})
										}
									/>
									<ParamSlider
										label="Max Tokens"
										value={params?.max_tokens}
										min={1}
										max={32768}
										step={1}
										disabled={isParamDisabled(provider, "max_tokens")}
										disabledReason={
											getParamIncompatibility(provider, "max_tokens") ??
											undefined
										}
										onChange={(v) =>
											onParamsChange?.({
												...(params as GenerationParams),
												max_tokens: v === undefined ? undefined : Math.round(v),
											})
										}
									/>
									<ParamSlider
										label="Top P"
										value={params?.top_p}
										min={0}
										max={1}
										step={0.01}
										disabled={isParamDisabled(provider, "top_p")}
										disabledReason={
											getParamIncompatibility(provider, "top_p") ?? undefined
										}
										onChange={(v) =>
											onParamsChange?.({
												...(params as GenerationParams),
												top_p: v,
											})
										}
									/>
									<ParamSlider
										label="Min P"
										value={params?.min_p}
										min={0}
										max={1}
										step={0.01}
										disabled={isParamDisabled(provider, "min_p")}
										disabledReason={
											getParamIncompatibility(provider, "min_p") ?? undefined
										}
										onChange={(v) =>
											onParamsChange?.({
												...(params as GenerationParams),
												min_p: v,
											})
										}
									/>
									<ParamSlider
										label="Top K"
										value={params?.top_k}
										min={1}
										max={100}
										step={1}
										disabled={isParamDisabled(provider, "top_k")}
										disabledReason={
											getParamIncompatibility(provider, "top_k") ?? undefined
										}
										onChange={(v) =>
											onParamsChange?.({
												...(params as GenerationParams),
												top_k: v === undefined ? undefined : Math.round(v),
											})
										}
									/>
									<ParamSlider
										label="Freq Penalty"
										value={params?.frequency_penalty}
										min={-2}
										max={2}
										step={0.01}
										disabled={isParamDisabled(provider, "frequency_penalty")}
										disabledReason={
											getParamIncompatibility(provider, "frequency_penalty") ??
											undefined
										}
										onChange={(v) =>
											onParamsChange?.({
												...(params as GenerationParams),
												frequency_penalty: v,
											})
										}
									/>
									<ParamSlider
										label="Pres Penalty"
										value={params?.presence_penalty}
										min={-2}
										max={2}
										step={0.01}
										disabled={isParamDisabled(provider, "presence_penalty")}
										disabledReason={
											getParamIncompatibility(provider, "presence_penalty") ??
											undefined
										}
										onChange={(v) =>
											onParamsChange?.({
												...(params as GenerationParams),
												presence_penalty: v,
											})
										}
									/>
								</div>
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
									Provider
								</span>
								<div className="text-(--text-primary) font-medium">
									{model.provider_name}
								</div>
							</div>
							<div>
								<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
									Model ID
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
										Context
									</span>
									<div className="text-(--text-primary) font-medium">
										{model.context_length?.toLocaleString() ?? "-"}
									</div>
								</div>
								<div>
									<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
										Max Out
									</span>
									<div className="text-(--text-primary) font-medium">
										{model.max_output_tokens?.toLocaleString() ?? "-"}
									</div>
								</div>
							</div>
							<div className="grid grid-cols-2 gap-2">
								<div>
									<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
										In $/1M
									</span>
									<div className="text-(--text-primary) font-medium">
										${formatPrice(model.input_price_per_million)}
									</div>
								</div>
								<div>
									<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
										Out $/1M
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
									Capabilities
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
								Proxy ID
							</span>
							<button
								type="button"
								onClick={() => {
									navigator.clipboard
										.writeText(proxyId)
										.then(() => toast("Copied!", "info"))
										.catch(() => toast("Copy failed", "error"));
								}}
								className="block mt-0.5 p-1.5 rounded bg-(--surface-input) text-[10px] text-(--text-secondary) break-all text-left cursor-pointer hover:bg-gray-700 transition-colors w-full"
								title="Click to copy"
							>
								{proxyId}
							</button>
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
