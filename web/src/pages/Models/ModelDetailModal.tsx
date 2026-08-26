import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Pin, RefreshCw, Sparkles } from "@/lib/icons";
import type { Model } from "../../api/types";
import { CapBadge } from "../../components/CapBadge";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { CopyablePill } from "../../components/CopyablePill";
import { CAP_META, hasCap } from "../../components/capMeta";
import { DetailSectionHeader } from "../../components/DetailSectionHeader";
import type { LangIconKey } from "../../components/langIcons";
import { Modal } from "../../components/Modal";
import { OutputBadges } from "../../components/OutputBadges";
import {
	formatPriceInput,
	nonTextOutputs,
	parseCapabilities,
	proxyModelID,
} from "../../utils/model";
import { ModelActionsFooter } from "./ModelActionsFooter";
import { ModelSnippetPanel } from "./ModelSnippetPanel";
import { ModelStatsGrid } from "./ModelStatsGrid";
import { modelModalities, parseParams } from "./modelDetailParse";
import { modelSnippetEntries } from "./modelSnippets";
import { type ModelTestResult, useModelActions } from "./useModelActions";
import { useModelEditor } from "./useModelEditor";

export function ModelDetailModal({
	model,
	onClose,
	onToggle,
	onDiscover,
	onTest,
	onToast,
	onUpdate,
	onDelete,
	zIndex,
}: {
	model: Model;
	onClose: () => void;
	/** Management callbacks. When omitted (read-only viewers like the
	 * Dashboard and Arena), the action footer and editing are hidden. */
	onToggle?: (id: string, enabled: boolean) => void;
	onDiscover?: (providerId: string) => Promise<unknown>;
	onTest?: (id: string) => Promise<ModelTestResult>;
	onToast?: (msg: string, type?: "success" | "error" | "info") => void;
	onUpdate?: (id: string, updates: Partial<Model>) => void;
	onDelete?: (id: string) => void;
	/** Forwarded to Modal for callers that open it above another modal */
	zIndex?: string;
}) {
	const { t } = useTranslation();
	const manageable = Boolean(
		onToggle && onDiscover && onTest && onToast && onUpdate && onDelete,
	);
	const caps = parseCapabilities(model.capabilities);
	const params = parseParams(model.params);
	const { inputMods, outputMods } = modelModalities(model);
	const [snippetTab, setSnippetTab] = useState<LangIconKey>("curl");
	const {
		editing,
		setEditing,
		editData,
		setEditData,
		confirmFields,
		setConfirmFields,
		discoveredDefaults,
		handleCancelEdit,
		handleSave,
		revertField,
	} = useModelEditor({ model, onUpdate: onUpdate ?? (() => {}) });
	const [confirmDelete, setConfirmDelete] = useState(false);
	const {
		cooldown,
		discovering,
		testing,
		testError,
		handleDiscover,
		handleTest,
	} = useModelActions({ model, onDiscover, onTest, onToast });

	const handleClose = () => {
		if (editing) {
			handleCancelEdit();
		} else {
			onClose();
		}
	};

	const pMid = proxyModelID(model.provider_name, model.model_id);
	const origin = window.location.origin;
	const snippets = modelSnippetEntries({
		t,
		model,
		proxyModelId: pMid,
		caps,
		inputMods,
		outputMods,
		origin,
	});

	return (
		<Modal
			header={
				<div>
					<div className="flex justify-between items-start mb-0">
						<div className="min-w-0">
							<h2 className="text-xl font-bold text-white">
								{model.display_name || model.name || pMid}
							</h2>
							<CopyablePill
								text={pMid}
								textClassName="text-sm text-gray-500 font-mono leading-tight"
								tooltip={t("model.clickToCopyModelId")}
							/>
						</div>
					</div>
				</div>
			}
			onClose={handleClose}
			maxWidth="max-w-lg"
			zIndex={zIndex}
			scrollable
		>
			{model.description && (
				<div className="max-h-[60px] overflow-y-auto mt-2 mb-4">
					<p className="text-sm text-gray-300 m-0 leading-[20px]">
						{model.description}
					</p>
				</div>
			)}

			<ModelStatsGrid
				model={model}
				inputMods={inputMods}
				outputMods={outputMods}
				editing={editing}
				editData={editData}
				setEditData={setEditData}
				discoveredDefaults={discoveredDefaults}
				revertField={revertField}
			/>

			{/* Editing a price pins it server-side; while pinned, discovery stops
			    refreshing this model's prices from live/catalog/models.dev.
			    Unpinning nulls the prices so the next scan re-derives them. */}
			{model.price_customized && (
				<div
					data-testid="price-pin-banner"
					className="mb-4 flex items-center gap-2 text-xs text-gray-500"
				>
					<Pin className="h-3.5 w-3.5 shrink-0" />
					<span>{t("models.detail.pricePinned")}</span>
					{manageable && !editing && (
						<button
							type="button"
							className="ui-link-accent"
							data-testid="price-pin-reset"
							onClick={() =>
								onUpdate?.(model.id, {
									price_customized: false,
									input_price_per_million: null,
									input_price_per_million_cache_hit: null,
									output_price_per_million: null,
								} as Partial<Model>)
							}
						>
							{t("models.detail.resetPricesToSource")}
						</button>
					)}
				</div>
			)}

			{caps && (
				<div className="mb-4">
					<DetailSectionHeader icon={Sparkles}>
						{t("models.detail.capabilities")}
					</DetailSectionHeader>
					<div className="flex flex-wrap gap-1">
						{CAP_META.map((m) => (
							<CapBadge key={m.key} caps={caps} capKey={m.key} />
						))}
						<OutputBadges outputModalities={model.output_modalities} />
					</div>
					{!CAP_META.some((m) => hasCap(caps, m.key)) &&
						nonTextOutputs(model).length === 0 && (
							<p className="text-sm text-gray-500">
								{t("model.noSpecialCapabilities")}
							</p>
						)}
				</div>
			)}

			{params && params.subscription_included !== undefined && (
				<div className="mb-4">
					<DetailSectionHeader icon={RefreshCw}>
						{t("models.detail.subscription")}
					</DetailSectionHeader>
					<div className="flex items-center gap-2">
						<span
							className={`ui-badge inline-flex items-center px-2 py-px leading-[1.6] text-xs font-medium ${
								params.subscription_included
									? "ui-badge-success"
									: "ui-badge-warning"
							}`}
						>
							{params.subscription_included
								? t("model.subscription.included")
								: t("model.subscription.notIncluded")}
						</span>
						{params.subscription_note ? (
							<span className="text-sm text-gray-500">
								{String(params.subscription_note)}
							</span>
						) : null}
					</div>
				</div>
			)}

			<ModelSnippetPanel
				entries={snippets}
				activeKey={snippetTab}
				onSelect={setSnippetTab}
				highlights={[origin, "YOUR_API_KEY", pMid]}
			/>

			{manageable && (
				<ModelActionsFooter
					model={model}
					editing={editing}
					testing={testing}
					testError={testError}
					cooldown={cooldown}
					discovering={discovering}
					confirmDelete={confirmDelete}
					onToggle={() => onToggle?.(model.id, !model.enabled)}
					onTest={handleTest}
					onArmDelete={() => setConfirmDelete(true)}
					onDelete={() => {
						onDelete?.(model.id);
						onClose();
					}}
					onEdit={() => setEditing(true)}
					onCancelEdit={handleCancelEdit}
					onSave={handleSave}
					onDiscover={handleDiscover}
				/>
			)}

			{confirmFields && (
				<ConfirmDialog
					title={t("delete_confirm.unsaved_changes")}
					fields={confirmFields}
					onConfirm={() => {
						setConfirmFields(null);
						setEditing(false);
						setEditData({
							display_name: model.display_name || "",
							context_length: model.context_length?.toString() || "",
							max_output_tokens: model.max_output_tokens?.toString() || "",
							input_price_per_million: formatPriceInput(
								model.input_price_per_million,
							),
							output_price_per_million: formatPriceInput(
								model.output_price_per_million,
							),
						});
					}}
					onCancel={() => setConfirmFields(null)}
				/>
			)}
		</Modal>
	);
}
