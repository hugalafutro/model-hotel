import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Model } from "../../api/types";
import { CapBadge } from "../../components/CapBadge";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { CopyablePill } from "../../components/CopyablePill";
import { CAP_META, hasCap } from "../../components/capMeta";
import { LangIcon, type LangIconKey } from "../../components/langIcons";
import { Modal } from "../../components/Modal";
import { Spinner } from "../../components/Spinner";
import { TerminalPreview } from "../../components/TerminalPreview";
import { formatNumber, formatRelativeTime } from "../../utils/format";
import {
	formatPrice,
	formatPriceInput,
	parseCapabilities,
	proxyModelID,
} from "../../utils/model";
import {
	snippetClaudeCodeModel,
	snippetClaudeCodeModelText,
	snippetCurlModel,
	snippetCurlModelText,
	snippetHermesModel,
	snippetHermesModelText,
	snippetJSModel,
	snippetJSModelText,
	snippetLibreChatModel,
	snippetLibreChatModelText,
	snippetOpenClawModel,
	snippetOpenClawModelText,
	snippetOpencodeModel,
	snippetOpencodeModelText,
	snippetPowershellModel,
	snippetPowershellModelText,
	snippetPythonModel,
	snippetPythonModelText,
	snippetZedModel,
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
			className={`text-[10px] px-1.5 py-0.5 rounded bg-gray-700 text-gray-400 hover:text-white border border-gray-600 cursor-pointer ${className ?? ""}`}
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
}: {
	model: Model;
	onClose: () => void;
	onToggle: (id: string, enabled: boolean) => void;
	onDiscover: (providerId: string) => Promise<unknown>;
	onTest: (id: string) => Promise<{
		success: boolean;
		streaming: boolean;
		ttft_ms: number;
		duration_ms: number;
		response: string;
		error?: string;
	}>;
	onToast: (msg: string, type?: "success" | "error" | "info") => void;
	onUpdate: (id: string, updates: Partial<Model>) => void;
	onDelete: (id: string) => void;
}) {
	const { t } = useTranslation();
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
	} = useModelEditor({ model, onUpdate });
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
		if (cooldown > 0 || discovering) return;
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
		if (testing) return;
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
					t("models.detail.testSuccess", {
						content,
						ttftPart,
						duration: (result.duration_ms / 1000).toFixed(1),
					}),
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
		content: ReturnType<typeof snippetCurlModel>;
		copyText: string;
	};

	const SNIPPET_ENTRIES: SnippetEntry[] = [
		{
			key: "curl",
			title: t("models.detail.snippet.curl"),
			content: snippetCurlModel(modelSnippetOpts),
			copyText: snippetCurlModelText(modelSnippetOpts),
		},
		{
			key: "powershell",
			title: t("models.detail.snippet.powershell"),
			content: snippetPowershellModel(modelSnippetOpts),
			copyText: snippetPowershellModelText(modelSnippetOpts),
		},
		{
			key: "javascript",
			title: t("models.detail.snippet.javascript"),
			content: snippetJSModel(modelSnippetOpts),
			copyText: snippetJSModelText(modelSnippetOpts),
		},
		{
			key: "python",
			title: t("models.detail.snippet.python"),
			content: snippetPythonModel(modelSnippetOpts),
			copyText: snippetPythonModelText(modelSnippetOpts),
		},
		{
			key: "claude",
			title: t("models.detail.snippet.claudeCode"),
			content: snippetClaudeCodeModel(modelSnippetOpts),
			copyText: snippetClaudeCodeModelText(modelSnippetOpts),
		},
		{
			key: "openclaw",
			title: t("models.detail.snippet.openClaw"),
			content: snippetOpenClawModel(modelSnippetOpts),
			copyText: snippetOpenClawModelText(modelSnippetOpts),
		},
		{
			key: "hermes",
			title: t("models.detail.snippet.hermes"),
			content: snippetHermesModel(modelSnippetOpts),
			copyText: snippetHermesModelText(modelSnippetOpts),
		},
		{
			key: "librechat",
			title: t("models.detail.snippet.librechat"),
			content: snippetLibreChatModel(modelSnippetOpts),
			copyText: snippetLibreChatModelText(modelSnippetOpts),
		},
		{
			key: "zed",
			title: t("models.detail.snippet.zed"),
			content: snippetZedModel(zedOpts),
			copyText: snippetZedModelText(zedOpts),
		},
		{
			key: "opencode",
			title: t("models.detail.snippet.opencode"),
			content: snippetOpencodeModel(opencodeOpts),
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
			scrollable
		>
			{model.description && (
				<div className="max-h-[60px] overflow-y-auto mt-2 mb-4">
					<p className="text-sm text-gray-300 m-0 leading-[20px]">
						{model.description}
					</p>
				</div>
			)}

			<div className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm mb-4">
				<div>
					<span className="text-gray-500">{t("models.detail.provider")}</span>
					<p className="text-gray-200">{model.provider_name}</p>
				</div>
				<div>
					<span className="text-gray-500">
						{t("models.detail.lastDiscovered")}
					</span>
					<p className="text-gray-200">
						{formatRelativeTime(model.last_seen_at)}
					</p>
				</div>
				<div className="col-span-2">
					<span className="text-gray-500">
						{t("models.detail.displayName")}
					</span>
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
					) : (
						<p className="text-gray-200">
							{model.display_name || model.name || "-"}
						</p>
					)}
				</div>
				<div>
					<span className="text-gray-500">
						{t("models.detail.contextLength")}
					</span>
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
					) : (
						<p className="text-gray-200">
							{formatNumber(model.context_length)}
							{t("models.detail.tokens")}
						</p>
					)}
				</div>
				<div>
					<span className="text-gray-500">{t("models.detail.maxOutput")}</span>
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
					) : (
						<p className="text-gray-200">
							{formatNumber(model.max_output_tokens)}
							{t("models.detail.tokens")}
						</p>
					)}
				</div>
				<div>
					<span className="text-gray-500">{t("models.detail.inputPrice")}</span>
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
					) : (
						<p className="text-gray-200">
							{model.input_price_per_million != null
								? `$${formatPrice(model.input_price_per_million)}/1M`
								: "-"}
						</p>
					)}
				</div>
				<div>
					<span className="text-gray-500">
						{t("models.detail.outputPrice")}
					</span>
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
					) : (
						<p className="text-gray-200">
							{model.output_price_per_million != null
								? `$${formatPrice(model.output_price_per_million)}/1M`
								: "-"}
						</p>
					)}
				</div>
				<div>
					<span className="text-gray-500">{t("models.detail.input")}</span>
					<p className="text-gray-200">
						{inputMods.join(", ") || t("models.detail.modality.text")}
					</p>
				</div>
				<div>
					<span className="text-gray-500">{t("models.detail.output")}</span>
					<p className="text-gray-200">
						{outputMods.join(", ") || t("models.detail.modality.text")}
					</p>
				</div>
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
							className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${
								params.subscription_included
									? "bg-green-900/40 text-green-300 border border-green-700/50"
									: "bg-yellow-900/40 text-yellow-300 border border-yellow-700/50"
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
							className={`p-1.5 rounded cursor-pointer transition-all ${
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
					{activeSnippet.content}
				</TerminalPreview>
			</div>

			<div className="flex items-center justify-between mt-4 pt-4">
				<div className="flex items-center gap-2">
					<button
						type="button"
						onClick={() => onToggle(model.id, !model.enabled)}
						className={`px-3 py-1.5 text-xs rounded-full border cursor-pointer transition-all ${
							model.enabled
								? "bg-green-900/50 text-green-400 border-green-700/50 hover:brightness-125 hover:shadow-[var(--glow-box-green)]"
								: "bg-red-900/50 text-red-400 border-red-700/50 hover:brightness-125 hover:shadow-[var(--glow-box-red)]"
						}`}
					>
						{model.enabled ? t("common.enabled") : t("common.disabled")}
					</button>
					<button
						type="button"
						disabled={testing}
						onClick={handleTest}
						className={`px-3 py-1.5 text-xs rounded-full border transition-all flex items-center gap-1.5 ${
							testError
								? "bg-red-900/50 text-red-300 border-red-700/50"
								: testing
									? "bg-amber-900/30 text-amber-300/70 border-amber-700/30 cursor-wait"
									: "bg-amber-900/40 text-amber-300 border-amber-700/50 cursor-pointer hover:brightness-125 hover:shadow-[var(--glow-box-amber)]"
						}`}
					>
						{testing && <Spinner />}
						{testing ? t("models.detail.testing") : t("models.detail.test")}
					</button>
					{!confirmDelete ? (
						<button
							type="button"
							onClick={() => setConfirmDelete(true)}
							className="px-3 py-1.5 text-xs rounded-full border bg-red-900/20 text-red-500/60 border-red-700/30 cursor-pointer hover:bg-red-900/40 hover:text-red-400 transition-all"
						>
							{t("common.delete")}
						</button>
					) : (
						<button
							type="button"
							onClick={() => {
								onDelete(model.id);
								onClose();
							}}
							className="px-3 py-1.5 text-xs rounded-full border bg-red-900/50 text-red-400 border-red-700/50 cursor-pointer hover:brightness-125 hover:shadow-[var(--glow-box-red)] transition-all"
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
								className={`px-3 py-1.5 text-xs rounded-full border transition-all ${
									cooldown > 0 || discovering
										? "bg-(--accent-lighter) text-(--accent)/50 border-(--accent-light) cursor-not-allowed"
										: "bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125"
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
