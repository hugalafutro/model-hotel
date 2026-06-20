import { useEffect, useRef, useState } from "react";
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
import { CapBadge } from "../../components/CapBadge";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { CopyablePill } from "../../components/CopyablePill";
import { CopyButton } from "../../components/CopyButton";
import { CAP_META, hasCap } from "../../components/capMeta";
import { DetailItem } from "../../components/LogDetailItem";
import { LangIcon, type LangIconKey } from "../../components/langIcons";
import { Modal } from "../../components/Modal";
import { ShikiCode } from "../../components/ShikiCode";
import { Spinner } from "../../components/Spinner";
import { TerminalPreview } from "../../components/TerminalPreview";
import { formatNumber, formatRelativeTime } from "../../utils/format";
import {
	formatPrice,
	formatPriceInput,
	parseCapabilities,
	proxyModelID,
} from "../../utils/model";
import type { SnippetLang } from "../../utils/shikiHighlighter";
import {
	snippetClaudeCodeModelText,
	snippetCurlModelText,
	snippetHermesModelText,
	snippetJSModelText,
	snippetLibreChatModelText,
	snippetOpenClawModelText,
	snippetOpencodeModelText,
	snippetPowershellModelText,
	snippetPythonModelText,
	snippetZedModelText,
} from "../../utils/snippets";
import { useModelEditor } from "./useModelEditor";

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

function parseParams(raw: string): Record<string, unknown> | null {
	try {
		return JSON.parse(raw);
	} catch {
		return null;
	}
}

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
	onTest?: (id: string) => Promise<{
		success: boolean;
		streaming: boolean;
		ttft_ms: number;
		duration_ms: number;
		response: string;
		error?: string;
	}>;
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
	const inputMods = (() => {
		try {
			const v = JSON.parse(model.input_modalities);
			return Array.isArray(v) ? v : [v];
		} catch {
			return [];
		}
	})();
	const outputMods = (() => {
		try {
			const v = JSON.parse(model.output_modalities);
			return Array.isArray(v) ? v : [v];
		} catch {
			return [];
		}
	})();
	const [cooldown, setCooldown] = useState(0);
	const [discovering, setDiscovering] = useState(false);
	const [testing, setTesting] = useState(false);
	const [testError, setTestError] = useState(false);
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
	const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
	const testErrorTimers = useRef<ReturnType<typeof setTimeout>[]>([]);

	useEffect(() => {
		return () => {
			if (timerRef.current) clearInterval(timerRef.current);
			// eslint-disable-next-line react-hooks/exhaustive-deps -- cleanup reads ref at unmount time
			for (const timer of testErrorTimers.current) clearTimeout(timer);
		};
	}, []);

	const handleDiscover = async () => {
		if (!onDiscover || cooldown > 0 || discovering) return;
		setDiscovering(true);
		try {
			await onDiscover(model.provider_id);
			setCooldown(30);
			timerRef.current = setInterval(() => {
				setCooldown((prev) => {
					if (prev <= 1) {
						if (timerRef.current) clearInterval(timerRef.current);
						return 0;
					}
					return prev - 1;
				});
			}, 1000);
		} finally {
			setDiscovering(false);
		}
	};

	const handleTest = async () => {
		if (!onTest || !onToast || testing) return;
		setTesting(true);
		setTestError(false);
		try {
			const result = await onTest(model.id);
			if (result.success) {
				const content = result.response.replace(/\n/g, " ").slice(0, 80);
				const isStreaming = result.streaming;
				const ttftPart = isStreaming
					? ` | TTFT: ${(result.ttft_ms / 1000).toFixed(1)}s`
					: "";
				onToast(
					t(
						// Reasoning models can succeed with empty content (the budget
						// went to reasoning); omit the "Response:" part in that case.
						content
							? "models.detail.testSuccess"
							: "models.detail.testSuccessNoResponse",
						{
							content,
							ttftPart,
							duration: (result.duration_ms / 1000).toFixed(1),
						},
					),
					"success",
				);
			} else {
				setTestError(true);
				onToast(
					t("models.detail.testFailed", {
						error: result.error || t("common.unknownError"),
					}),
					"error",
				);
				const timer = setTimeout(() => setTestError(false), 3000);
				testErrorTimers.current.push(timer);
			}
		} catch (err) {
			setTestError(true);
			onToast(
				t("models.detail.testFailed", {
					error: err instanceof Error ? err.message : t("common.unknownError"),
				}),
				"error",
			);
			const timer = setTimeout(() => setTestError(false), 3000);
			testErrorTimers.current.push(timer);
		} finally {
			setTesting(false);
		}
	};

	const handleClose = () => {
		if (editing) {
			handleCancelEdit();
		} else {
			onClose();
		}
	};

	const pMid = proxyModelID(model.provider_name, model.model_id);
	const origin = window.location.origin;
	const modelSnippetOpts = { proxyModelId: pMid, origin };
	const zedOpts = {
		proxyModelId: pMid,
		displayName: model.display_name || model.name,
		contextLength: model.context_length,
		maxOutputTokens: model.max_output_tokens,
		capabilities: caps,
		origin,
	};
	const opencodeOpts = {
		proxyModelId: pMid,
		displayName: model.display_name || model.name || pMid,
		contextLength: model.context_length,
		maxOutputTokens: model.max_output_tokens,
		capabilities: caps,
		inputModalities: inputMods,
		outputModalities: outputMods,
		inputPricePerMillion: model.input_price_per_million,
		outputPricePerMillion: model.output_price_per_million,
		origin,
	};

	type SnippetEntry = {
		key: LangIconKey;
		title: string;
		lang: SnippetLang;
		copyText: string;
	};

	const SNIPPET_ENTRIES: SnippetEntry[] = [
		{
			key: "curl",
			title: t("models.detail.snippet.curl"),
			lang: "bash",
			copyText: snippetCurlModelText(modelSnippetOpts),
		},
		{
			key: "powershell",
			title: t("models.detail.snippet.powershell"),
			lang: "powershell",
			copyText: snippetPowershellModelText(modelSnippetOpts),
		},
		{
			key: "javascript",
			title: t("models.detail.snippet.javascript"),
			lang: "javascript",
			copyText: snippetJSModelText(modelSnippetOpts),
		},
		{
			key: "python",
			title: t("models.detail.snippet.python"),
			lang: "python",
			copyText: snippetPythonModelText(modelSnippetOpts),
		},
		{
			key: "claude",
			title: t("models.detail.snippet.claudeCode"),
			lang: "bash",
			copyText: snippetClaudeCodeModelText(modelSnippetOpts),
		},
		{
			key: "openclaw",
			title: t("models.detail.snippet.openClaw"),
			lang: "bash",
			copyText: snippetOpenClawModelText(modelSnippetOpts),
		},
		{
			key: "hermes",
			title: t("models.detail.snippet.hermes"),
			lang: "bash",
			copyText: snippetHermesModelText(modelSnippetOpts),
		},
		{
			key: "librechat",
			title: t("models.detail.snippet.librechat"),
			lang: "yaml",
			copyText: snippetLibreChatModelText(modelSnippetOpts),
		},
		{
			key: "zed",
			title: t("models.detail.snippet.zed"),
			lang: "json",
			copyText: snippetZedModelText(zedOpts),
		},
		{
			key: "opencode",
			title: t("models.detail.snippet.opencode"),
			lang: "json",
			copyText: snippetOpencodeModelText(opencodeOpts),
		},
	];

	const activeSnippet =
		SNIPPET_ENTRIES.find((e) => e.key === snippetTab) ?? SNIPPET_ENTRIES[0];

	return (
		<Modal
			header={
				<div>
					<div className="flex justify-between items-start mb-0">
						<div>
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
				<DetailItem
					icon={Tag}
					label={t("models.detail.displayName")}
					value={model.display_name || model.name || "-"}
					className="col-span-2"
				>
					{editing ? (
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
					) : undefined}
				</DetailItem>
				<DetailItem
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
								<RevertButton
									onClick={() => revertField("max_output_tokens")}
								/>
							)}
						</div>
					) : undefined}
				</DetailItem>
				<DetailItem
					icon={DollarSign}
					label={t("models.detail.inputPrice")}
					value={
						model.input_price_per_million != null
							? `$${formatPrice(model.input_price_per_million)}/1M`
							: "-"
					}
					mono
				>
					{editing ? (
						<div className="flex items-center gap-1">
							<div className="relative w-full">
								<input
									type="number"
									step="0.01"
									min={0}
									max={1000}
									value={editData.input_price_per_million}
									onChange={(e) =>
										setEditData((prev) => ({
											...prev,
											input_price_per_million: e.target.value,
										}))
									}
									className="ui-input text-sm pr-16!"
									placeholder={t("models.detail.placeholder.price")}
								/>
								<span className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] text-gray-400">
									{t("models.detail.perMillionTokens")}
								</span>
							</div>
							{editData.input_price_per_million !==
								formatPriceInput(
									discoveredDefaults.input_price_per_million,
								) && (
								<RevertButton
									onClick={() => revertField("input_price_per_million")}
									className="shrink-0"
								/>
							)}
						</div>
					) : undefined}
				</DetailItem>
				<DetailItem
					icon={Coins}
					label={t("models.detail.outputPrice")}
					value={
						model.output_price_per_million != null
							? `$${formatPrice(model.output_price_per_million)}/1M`
							: "-"
					}
					mono
				>
					{editing ? (
						<div className="flex items-center gap-1">
							<div className="relative w-full">
								<input
									type="number"
									step="0.01"
									min={0}
									max={1000}
									value={editData.output_price_per_million}
									onChange={(e) =>
										setEditData((prev) => ({
											...prev,
											output_price_per_million: e.target.value,
										}))
									}
									className="ui-input text-sm pr-16!"
									placeholder={t("models.detail.placeholder.price")}
								/>
								<span className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] text-gray-400">
									{t("models.detail.perMillionTokens")}
								</span>
							</div>
							{editData.output_price_per_million !==
								formatPriceInput(
									discoveredDefaults.output_price_per_million,
								) && (
								<RevertButton
									onClick={() => revertField("output_price_per_million")}
									className="shrink-0"
								/>
							)}
						</div>
					) : undefined}
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

			{caps && (
				<div className="mb-4">
					<h3 className="text-sm font-medium text-gray-400 mb-2">
						{t("models.detail.capabilities")}
					</h3>
					<div className="flex flex-wrap gap-1">
						{CAP_META.map((m) => (
							<CapBadge key={m.key} caps={caps} capKey={m.key} />
						))}
					</div>
					{!CAP_META.some((m) => hasCap(caps, m.key)) && (
						<p className="text-sm text-gray-500">
							{t("model.noSpecialCapabilities")}
						</p>
					)}
				</div>
			)}

			{params && params.subscription_included !== undefined && (
				<div className="mb-4">
					<h3 className="text-sm font-medium text-gray-400 mb-2">
						{t("models.detail.subscription")}
					</h3>
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

			<div className="mt-4 pt-4">
				<div
					role="tablist"
					aria-label={t("models.detail.snippetFormatPicker")}
					className="flex items-center gap-1 mb-3"
				>
					{SNIPPET_ENTRIES.map((entry) => (
						<button
							key={entry.key}
							type="button"
							role="tab"
							aria-selected={snippetTab === entry.key}
							onClick={() => setSnippetTab(entry.key)}
							className={`ui-tab p-1.5 transition-all ${
								snippetTab === entry.key
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
					title={activeSnippet.title}
					icon={activeSnippet.key}
					copyText={activeSnippet.copyText}
					height={200}
				>
					<ShikiCode
						code={activeSnippet.copyText}
						lang={activeSnippet.lang}
						highlights={[origin, "YOUR_API_KEY", pMid]}
					/>
				</TerminalPreview>
			</div>

			{manageable && (
				<div className="flex items-center justify-between mt-4 pt-4">
					<div className="flex items-center gap-2">
						<button
							type="button"
							onClick={() => onToggle?.(model.id, !model.enabled)}
							className={`ui-btn ${model.enabled ? "ui-btn-primary" : "ui-btn-danger"}`}
						>
							{model.enabled ? t("common.enabled") : t("common.disabled")}
						</button>
						<button
							type="button"
							disabled={testing}
							onClick={handleTest}
							className={`ui-btn ${
								testError
									? "ui-btn-danger bg-red-900/50 text-red-300 border-red-700/50"
									: testing
										? "ui-btn-secondary bg-amber-900/30 text-amber-300/70 border-amber-700/30 cursor-wait"
										: "ui-btn-secondary"
							}`}
						>
							{testing && <Spinner />}
							{testing ? t("models.detail.testing") : t("models.detail.test")}
						</button>
						{!confirmDelete ? (
							<button
								type="button"
								onClick={() => setConfirmDelete(true)}
								className="ui-btn bg-red-900/20 text-red-500/60 border-red-700/30 hover:bg-red-900/40 hover:text-red-400"
							>
								{t("common.delete")}
							</button>
						) : (
							<button
								type="button"
								onClick={() => {
									onDelete?.(model.id);
									onClose();
								}}
								className="ui-btn ui-btn-danger"
							>
								{t("models.detail.confirmDelete")}
							</button>
						)}
					</div>
					<div className="flex items-center gap-2">
						{editing ? (
							<>
								<button
									type="button"
									onClick={handleCancelEdit}
									className="ui-btn ui-btn-secondary"
								>
									{t("common.cancel")}
								</button>
								<button
									type="button"
									onClick={handleSave}
									className="ui-btn ui-btn-primary"
								>
									{t("common.saveChanges")}
								</button>
							</>
						) : (
							<>
								<button
									type="button"
									onClick={() => setEditing(true)}
									className="ui-btn ui-btn-secondary"
								>
									{t("common.edit")}
								</button>
								<button
									type="button"
									disabled={cooldown > 0 || discovering}
									onClick={handleDiscover}
									className={`ui-btn ${
										cooldown > 0 || discovering
											? "bg-(--accent-lighter) text-(--accent)/50 border-(--accent-light) cursor-not-allowed"
											: "bg-(--accent-light) text-(--accent) border-(--accent-lighter) hover:brightness-125"
									}`}
								>
									{discovering ? (
										<>
											<Spinner /> {t("models.detail.updating")}
										</>
									) : cooldown > 0 ? (
										t("models.detail.updateCooldown", { cooldown })
									) : (
										t("models.detail.updateInfo")
									)}
								</button>
							</>
						)}
					</div>
				</div>
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
