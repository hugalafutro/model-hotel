import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api/client";
import type { Model, ModelCapabilities } from "../api/types";
import { CapBadge } from "../components/CapBadge";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { CopyablePill } from "../components/CopyablePill";
import { CAP_META, type CapKey, hasCap } from "../components/capMeta";
import type { SortState } from "../components/DataTable";
import {
	EmptyRow,
	PaginationBar,
	Row,
	SortableHeader,
} from "../components/DataTable";
import { FilterInput } from "../components/FilterInput";
import { ProviderFilter } from "../components/ProviderFilter";
import { Spinner } from "../components/Spinner";
import { useToast } from "../context/ToastContext";
import {
	formatNumber,
	formatRelativeTime,
	formatTokens,
} from "../utils/format";
import {
	formatPrice,
	formatPriceInput,
	parseCapabilities,
	proxyModelID,
} from "../utils/model";

function parseParams(raw: string): Record<string, unknown> | null {
	try {
		return JSON.parse(raw);
	} catch {
		return null;
	}
}

type SortField =
	| "name"
	| "capabilities"
	| "provider"
	| "discovered"
	| "context"
	| "output"
	| "status";

function ModelDetailModal({
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
		ttft_ms: number;
		duration_ms: number;
		response: string;
		error?: string;
	}>;
	onToast: (msg: string, type?: "success" | "error" | "info") => void;
	onUpdate: (id: string, updates: Partial<Model>) => void;
	onDelete: (id: string) => void;
}) {
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
	const [snippetTab, setSnippetTab] = useState<"curl" | "zed">("curl");
	const [editing, setEditing] = useState(false);
	const [editVersion, setEditVersion] = useState("");
	const [confirmFields, setConfirmFields] = useState<string[] | null>(null);
	const [confirmDelete, setConfirmDelete] = useState(false);
	const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

	const [editData, setEditData] = useState({
		display_name: model.display_name || "",
		context_length: model.context_length?.toString() || "",
		max_output_tokens: model.max_output_tokens?.toString() || "",
		input_price_per_million: formatPriceInput(model.input_price_per_million),
		output_price_per_million: formatPriceInput(model.output_price_per_million),
	});

	const discoveredDefaults = useMemo(
		() => ({
			display_name: model.name || "",
			context_length: model.context_length,
			max_output_tokens: model.max_output_tokens,
			input_price_per_million: model.input_price_per_million,
			output_price_per_million: model.output_price_per_million,
		}),
		[model],
	);

	useEffect(() => {
		return () => {
			if (timerRef.current) clearInterval(timerRef.current);
		};
	}, []);

	// Re-sync editData when model changes while editing (e.g. after save round-trips)
	// Uses a version string derived from model.id so the compiler doesn't
	// complain about refs or setState-in-effect.
	const currentEditVersion = editing ? model.id : "";
	if (editing && currentEditVersion !== editVersion) {
		setEditVersion(currentEditVersion);
		setEditData({
			display_name: model.display_name || "",
			context_length: model.context_length?.toString() || "",
			max_output_tokens: model.max_output_tokens?.toString() || "",
			input_price_per_million: formatPriceInput(model.input_price_per_million),
			output_price_per_million: formatPriceInput(
				model.output_price_per_million,
			),
		});
	}

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
				onToast(
					`Success | Response: ${content} | TTFT: ${(result.ttft_ms / 1000).toFixed(1)}s | Duration: ${(result.duration_ms / 1000).toFixed(1)}s`,
					"success",
				);
			} else {
				setTestError(true);
				onToast(`Test failed: ${result.error || "Unknown error"}`, "error");
				setTimeout(() => setTestError(false), 3000);
			}
		} catch (err) {
			setTestError(true);
			onToast(
				`Test failed: ${err instanceof Error ? err.message : "Unknown error"}`,
				"error",
			);
			setTimeout(() => setTestError(false), 3000);
		} finally {
			setTesting(false);
		}
	};

	const getFieldLabel = (key: string): string => {
		const labels: Record<string, string> = {
			display_name: "Display Name",
			context_length: "Context Length",
			max_output_tokens: "Max Output Tokens",
			input_price_per_million: "Input Price",
			output_price_per_million: "Output Price",
		};
		return labels[key] || key;
	};

	const getChangedFields = (): string[] => {
		const fields: string[] = [];
		if (editData.display_name !== (model.display_name || ""))
			fields.push("display_name");
		const cl =
			editData.context_length === "" ? null : Number(editData.context_length);
		if (cl !== model.context_length) fields.push("context_length");
		const mot =
			editData.max_output_tokens === ""
				? null
				: Number(editData.max_output_tokens);
		if (mot !== model.max_output_tokens) fields.push("max_output_tokens");
		const ipm =
			editData.input_price_per_million === ""
				? null
				: Number(editData.input_price_per_million);
		if (
			ipm !==
			(model.input_price_per_million != null
				? Math.round(model.input_price_per_million * 10000) / 10000
				: null)
		)
			fields.push("input_price_per_million");
		const opm =
			editData.output_price_per_million === ""
				? null
				: Number(editData.output_price_per_million);
		if (
			opm !==
			(model.output_price_per_million != null
				? Math.round(model.output_price_per_million * 10000) / 10000
				: null)
		)
			fields.push("output_price_per_million");
		return fields;
	};

	const handleCancelEdit = () => {
		const changed = getChangedFields();
		if (changed.length > 0) {
			setConfirmFields(changed.map(getFieldLabel));
		} else {
			setEditing(false);
		}
	};

	const handleSave = () => {
		const changed = getChangedFields();
		if (changed.length === 0) {
			setEditing(false);
			return;
		}
		const updates: Record<string, unknown> = {};
		if (changed.includes("display_name"))
			updates.display_name = editData.display_name;
		if (changed.includes("context_length"))
			updates.context_length =
				editData.context_length === "" ? null : Number(editData.context_length);
		if (changed.includes("max_output_tokens"))
			updates.max_output_tokens =
				editData.max_output_tokens === ""
					? null
					: Number(editData.max_output_tokens);
		if (changed.includes("input_price_per_million"))
			updates.input_price_per_million =
				editData.input_price_per_million === ""
					? null
					: Number(editData.input_price_per_million);
		if (changed.includes("output_price_per_million"))
			updates.output_price_per_million =
				editData.output_price_per_million === ""
					? null
					: Number(editData.output_price_per_million);
		if (Object.keys(updates).length > 0) {
			onUpdate(model.id, updates as Partial<Model>);
		}
		setEditing(false);
	};

	const revertField = (key: keyof typeof discoveredDefaults) => {
		if (key === "display_name") {
			setEditData((prev) => ({
				...prev,
				display_name: discoveredDefaults.display_name,
			}));
		} else if (key === "context_length") {
			setEditData((prev) => ({
				...prev,
				context_length: discoveredDefaults.context_length?.toString() ?? "",
			}));
		} else if (key === "max_output_tokens") {
			setEditData((prev) => ({
				...prev,
				max_output_tokens:
					discoveredDefaults.max_output_tokens?.toString() ?? "",
			}));
		} else if (key === "input_price_per_million") {
			setEditData((prev) => ({
				...prev,
				input_price_per_million: formatPriceInput(
					discoveredDefaults.input_price_per_million,
				),
			}));
		} else if (key === "output_price_per_million") {
			setEditData((prev) => ({
				...prev,
				output_price_per_million: formatPriceInput(
					discoveredDefaults.output_price_per_million,
				),
			}));
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

	const curlCmd = `curl -X POST ${window.location.origin}/v1/chat/completions \\\n  -H "Authorization: Bearer API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model":"${pMid}","messages":[{"role":"user","content":"Hello"}]}'`;

	const zedJson = JSON.stringify(
		{
			name: pMid,
			display_name: model.name,
			max_tokens: model.max_output_tokens,
			context_length: model.context_length,
			capabilities: {
				tools: hasCap(caps, "tool_calling"),
				images: hasCap(caps, "vision"),
				parallel_tool_calls: hasCap(caps, "parallel_tool_calls"),
				prompt_cache_key: false,
			},
		},
		null,
		2,
	);

	const snippetContent = snippetTab === "curl" ? curlCmd : zedJson;

	return (
		<div
			role="dialog"
			aria-modal="true"
			className="fixed inset-0 flex items-center justify-center z-50"
			onKeyDown={(e) => {
				if (e.key === "Escape") handleClose();
			}}
		>
			<button
				type="button"
				className="absolute inset-0 bg-black/60 cursor-default"
				onClick={handleClose}
				aria-label="Close dialog"
			/>
			<div className="relative ui-card p-6 w-full max-w-lg max-h-[85vh] overflow-y-auto">
				<div className="flex justify-between items-start mb-4">
					<div>
						<h2 className="text-xl font-bold text-white">
							{model.display_name || model.name || pMid}
						</h2>
						<CopyablePill
							text={pMid}
							textClassName="text-sm text-gray-500 font-mono leading-tight mt-1"
							tooltip="Click to copy model ID"
						/>
					</div>
					<button
						type="button"
						onClick={handleClose}
						className="absolute top-4 right-4 text-(--text-secondary) hover:text-(--text-primary) transition-all cursor-default leading-none p-1 hover:drop-shadow-[0_0_8px_var(--accent)]"
						aria-label="Close"
					>
						<X size={20} />
					</button>
				</div>

				{model.description && (
					<p className="text-sm text-gray-300 mb-4">{model.description}</p>
				)}

				<div className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm mb-4">
					<div>
						<span className="text-gray-500">Provider</span>
						<p className="text-gray-200">{model.provider_name}</p>
					</div>
					<div>
						<span className="text-gray-500">Last Discovered</span>
						<p className="text-gray-200">
							{formatRelativeTime(model.last_seen_at)}
						</p>
					</div>
					<div>
						<span className="text-gray-500">Display Name</span>
						{editing ? (
							<div className="flex items-center gap-1">
								<input
									type="text"
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
									<button
										type="button"
										onClick={() => revertField("display_name")}
										className="text-[10px] px-1.5 py-0.5 rounded bg-gray-700 text-gray-400 hover:text-white border border-gray-600 cursor-pointer"
										title="Revert to discovered value"
									>
										↩
									</button>
								)}
							</div>
						) : (
							<p className="text-gray-200">
								{model.display_name || model.name || "-"}
							</p>
						)}
					</div>
					<div>
						<span className="text-gray-500">Context Length</span>
						{editing ? (
							<div className="flex items-center gap-1">
								<input
									type="number"
									value={editData.context_length}
									onChange={(e) =>
										setEditData((prev) => ({
											...prev,
											context_length: e.target.value,
										}))
									}
									className="ui-input text-sm"
									placeholder="tokens"
								/>
								{editData.context_length !==
									(discoveredDefaults.context_length?.toString() ?? "") && (
									<button
										type="button"
										onClick={() => revertField("context_length")}
										className="text-[10px] px-1.5 py-0.5 rounded bg-gray-700 text-gray-400 hover:text-white border border-gray-600 cursor-pointer"
										title="Revert to discovered value"
									>
										↩
									</button>
								)}
							</div>
						) : (
							<p className="text-gray-200">
								{formatNumber(model.context_length)} tokens
							</p>
						)}
					</div>
					<div>
						<span className="text-gray-500">Max Output</span>
						{editing ? (
							<div className="flex items-center gap-1">
								<input
									type="number"
									value={editData.max_output_tokens}
									onChange={(e) =>
										setEditData((prev) => ({
											...prev,
											max_output_tokens: e.target.value,
										}))
									}
									className="ui-input text-sm"
									placeholder="tokens"
								/>
								{editData.max_output_tokens !==
									(discoveredDefaults.max_output_tokens?.toString() ?? "") && (
									<button
										type="button"
										onClick={() => revertField("max_output_tokens")}
										className="text-[10px] px-1.5 py-0.5 rounded bg-gray-700 text-gray-400 hover:text-white border border-gray-600 cursor-pointer"
										title="Revert to discovered value"
									>
										↩
									</button>
								)}
							</div>
						) : (
							<p className="text-gray-200">
								{formatNumber(model.max_output_tokens)} tokens
							</p>
						)}
					</div>
					<div>
						<span className="text-gray-500">Input Price</span>
						{editing ? (
							<div className="flex items-center gap-1">
								<div className="relative w-full">
									<input
										type="number"
										step="0.01"
										value={editData.input_price_per_million}
										onChange={(e) =>
											setEditData((prev) => ({
												...prev,
												input_price_per_million: e.target.value,
											}))
										}
										className="ui-input text-sm pr-16!"
										placeholder="0.00"
									/>
									<span className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] text-gray-400">
										/1M tok
									</span>
								</div>
								{editData.input_price_per_million !==
									formatPriceInput(
										discoveredDefaults.input_price_per_million,
									) && (
									<button
										type="button"
										onClick={() => revertField("input_price_per_million")}
										className="text-[10px] px-1.5 py-0.5 rounded bg-gray-700 text-gray-400 hover:text-white border border-gray-600 cursor-pointer shrink-0"
										title="Revert to discovered value"
									>
										↩
									</button>
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
						<span className="text-gray-500">Output Price</span>
						{editing ? (
							<div className="flex items-center gap-1">
								<div className="relative w-full">
									<input
										type="number"
										step="0.01"
										value={editData.output_price_per_million}
										onChange={(e) =>
											setEditData((prev) => ({
												...prev,
												output_price_per_million: e.target.value,
											}))
										}
										className="ui-input text-sm pr-16!"
										placeholder="0.00"
									/>
									<span className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] text-gray-400">
										/1M tok
									</span>
								</div>
								{editData.output_price_per_million !==
									formatPriceInput(
										discoveredDefaults.output_price_per_million,
									) && (
									<button
										type="button"
										onClick={() => revertField("output_price_per_million")}
										className="text-[10px] px-1.5 py-0.5 rounded bg-gray-700 text-gray-400 hover:text-white border border-gray-600 cursor-pointer shrink-0"
										title="Revert to discovered value"
									>
										↩
									</button>
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
						<span className="text-gray-500">Input</span>
						<p className="text-gray-200">{inputMods.join(", ") || "text"}</p>
					</div>
					<div>
						<span className="text-gray-500">Output</span>
						<p className="text-gray-200">{outputMods.join(", ") || "text"}</p>
					</div>
				</div>

				{caps && (
					<div className="mb-4">
						<h3 className="text-sm font-medium text-gray-400 mb-2">
							Capabilities
						</h3>
						<div className="flex flex-wrap gap-1">
							{CAP_META.map((m) => (
								<CapBadge key={m.key} caps={caps} capKey={m.key} />
							))}
						</div>
						{!CAP_META.some((m) => hasCap(caps, m.key)) && (
							<p className="text-sm text-gray-500">
								No special capabilities detected
							</p>
						)}
					</div>
				)}

				{params && params.subscription_included !== undefined && (
					<div className="mb-4">
						<h3 className="text-sm font-medium text-gray-400 mb-2">
							Subscription
						</h3>
						<div className="flex items-center gap-2">
							<span
								className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${
									params.subscription_included
										? "bg-green-900/40 text-green-300 border border-green-700/50"
										: "bg-yellow-900/40 text-yellow-300 border border-yellow-700/50"
								}`}
							>
								{params.subscription_included ? "Included" : "Not included"}
							</span>
							{params.subscription_note ? (
								<span className="text-sm text-gray-500">
									{String(params.subscription_note)}
								</span>
							) : null}
						</div>
					</div>
				)}

				{editing && (
					<div className="flex gap-3 justify-end mb-4 pt-2">
						<button
							type="button"
							onClick={handleCancelEdit}
							className="px-3 py-1.5 text-xs rounded-full border bg-gray-900/40 text-gray-300 border-gray-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(156,163,175,0.15)] transition-all"
						>
							Cancel
						</button>
						<button
							type="button"
							onClick={handleSave}
							className="px-3 py-1.5 text-xs rounded-full border bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125 transition-all"
						>
							Save Changes
						</button>
					</div>
				)}

				<div className="mt-4 pt-4">
					<div className="flex items-center justify-between mb-3">
						<div className="flex items-center gap-1">
							{(["curl", "zed"] as const).map((tab) => (
								<button
									key={tab}
									type="button"
									onClick={() => setSnippetTab(tab)}
									className={`px-2.5 py-1 rounded text-[11px] font-medium uppercase tracking-wider cursor-pointer transition-all ${
										snippetTab === tab
											? "bg-slate-700/60 text-slate-200 border border-slate-600/50"
											: "text-slate-500 hover:text-slate-400 border border-transparent"
									}`}
								>
									{tab === "curl" ? "cURL" : "ZED"}
								</button>
							))}
						</div>
						<button
							type="button"
							onClick={() => {
								navigator.clipboard.writeText(snippetContent);
								onToast("Copied to clipboard", "info");
							}}
							className="px-1.5 py-0.5 rounded text-[10px] font-medium border bg-slate-700/40 text-slate-300 border-slate-600/40 hover:brightness-125 transition-all cursor-pointer"
						>
							Copy
						</button>
					</div>
					<pre className="bg-gray-950 rounded-lg p-3 text-[11px] text-gray-300 font-mono overflow-x-auto overflow-y-auto h-30 leading-relaxed whitespace-pre-wrap break-all">
						{snippetContent}
					</pre>
				</div>

				<div className="flex items-center justify-between mt-4 pt-4">
					<div className="flex items-center gap-2">
						<button
							type="button"
							onClick={() => onToggle(model.id, !model.enabled)}
							className={`px-3 py-1.5 text-xs rounded-full border cursor-pointer transition-all ${
								model.enabled
									? "bg-green-900/50 text-green-400 border-green-700/50 hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(34,197,94,0.2)]"
									: "bg-red-900/50 text-red-400 border-red-700/50 hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)]"
							}`}
						>
							{model.enabled ? "Enabled" : "Disabled"}
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
										: "bg-amber-900/40 text-amber-300 border-amber-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(245,158,11,0.2)]"
							}`}
						>
							{testing && <Spinner />}
							{testing ? "Testing…" : "Test"}
						</button>
						{!confirmDelete ? (
							<button
								type="button"
								onClick={() => setConfirmDelete(true)}
								className="px-3 py-1.5 text-xs rounded-full border bg-red-900/20 text-red-500/60 border-red-700/30 cursor-pointer hover:bg-red-900/40 hover:text-red-400 transition-all"
							>
								Delete
							</button>
						) : (
							<button
								type="button"
								onClick={() => {
									onDelete(model.id);
									onClose();
								}}
								className="px-3 py-1.5 text-xs rounded-full border bg-red-900/50 text-red-400 border-red-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(239,68,68,0.2)] transition-all"
							>
								Confirm delete
							</button>
						)}
					</div>
					<div className="flex items-center gap-2">
						{!editing && (
							<button
								type="button"
								onClick={() => setEditing(true)}
								className="px-3 py-1.5 text-xs rounded-full border bg-gray-900/40 text-gray-300 border-gray-700/50 cursor-pointer hover:brightness-125 hover:shadow-[0_0_8px_2px_rgba(156,163,175,0.15)] transition-all"
							>
								Edit
							</button>
						)}
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
							{discovering
								? "Updating…"
								: cooldown > 0
									? `Update (${cooldown}s)`
									: "Update info"}
						</button>
					</div>
				</div>
			</div>
			{confirmFields && (
				<ConfirmDialog
					title="Unsaved Changes"
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
		</div>
	);
}

function matchesAllCaps(
	caps: ModelCapabilities | null,
	keys: Set<CapKey>,
): boolean {
	if (keys.size === 0) return true;
	for (const k of keys) {
		if (!hasCap(caps, k)) return false;
	}
	return true;
}

export function Models() {
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const [searchQuery, setSearchQuery] = useState("");
	const [selectedProviders, setSelectedProviders] = useState<Set<string>>(
		new Set(),
	);
	const [detailModel, setDetailModel] = useState<Model | null>(null);
	const [sort, setSort] = useState<SortState<SortField>>({
		field: "name",
		dir: "asc",
	});
	const [capFilter, setCapFilter] = useState<Set<CapKey>>(new Set());
	const [statusFilter, setStatusFilter] = useState<Set<"enabled" | "disabled">>(
		new Set(),
	);
	const showAllStatus = statusFilter.size === 0 || statusFilter.size === 2;
	const [pageSize, setPageSize] = useState(20);
	const [currentPage, setCurrentPage] = useState(1);

	const { data: models, isLoading } = useQuery({
		queryKey: ["models"],
		queryFn: () => api.models.list(),
	});

	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});

	const toggleMutation = useMutation({
		mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
			api.models.update(id, { enabled }),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["models"] });
		},
		onError: (err: Error) => {
			toast(`Failed to update model: ${err.message}`, "error");
		},
	});

	const updateMutation = useMutation({
		mutationFn: ({ id, data }: { id: string; data: Record<string, unknown> }) =>
			api.models.update(id, data as Parameters<typeof api.models.update>[1]),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["models"] });
			toast("Model updated", "success");
		},
		onError: (err: Error) => {
			toast(`Failed to update model: ${err.message}`, "error");
		},
	});

	const handleToggleModel = useCallback(
		(id: string, enabled: boolean) => {
			toggleMutation.mutate(
				{ id, enabled },
				{
					onSuccess: () => {
						toast(
							enabled ? "Model enabled" : "Model disabled",
							enabled ? "success" : "error",
						);
						setDetailModel((prev) => (prev ? { ...prev, enabled } : null));
					},
				},
			);
		},
		[toggleMutation, toast],
	);

	const handleUpdateModel = useCallback(
		(id: string, updates: Partial<Model>) => {
			updateMutation.mutate(
				{ id, data: updates },
				{
					onSuccess: () => {
						setDetailModel((prev) => (prev ? { ...prev, ...updates } : null));
					},
				},
			);
		},
		[updateMutation],
	);

	const handleDiscover = useCallback(
		async (providerId: string) => {
			toast("Discovering models…", "info");
			const result = await api.providers.discover(providerId);
			queryClient.invalidateQueries({ queryKey: ["models"] });
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			toast(`Discovered ${result?.discovered ?? "new"} models`, "success");
			return result;
		},
		[queryClient, toast],
	);

	const handleTest = useCallback(async (id: string) => {
		return api.models.test(id);
	}, []);

	const deleteMutation = useMutation({
		mutationFn: (id: string) => api.models.delete(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["models"] });
			toast("Model deleted successfully", "success");
		},
		onError: (err: Error) => {
			toast(`Failed to delete model: ${err.message}`, "error");
		},
	});

	const toggleCapFilter = useCallback((key: CapKey) => {
		setCapFilter((prev) => {
			const next = new Set(prev);
			if (next.has(key)) next.delete(key);
			else next.add(key);
			return next;
		});
		setCurrentPage(1);
	}, []);

	const handleSort = (field: SortField) => {
		setSort((prev) => ({
			field,
			dir: prev.field === field && prev.dir === "asc" ? "desc" : "asc",
		}));
	};

	const { sortedAndFiltered, pillAvailability, existingCaps } = useMemo(() => {
		const baseFiltered =
			models?.filter(
				(model) =>
					proxyModelID(model.provider_name, model.model_id)
						.toLowerCase()
						.includes(searchQuery.toLowerCase()) ||
					model.name?.toLowerCase().includes(searchQuery.toLowerCase()) ||
					model.display_name?.toLowerCase().includes(searchQuery.toLowerCase()),
			) || [];

		const capsInData = new Set<CapKey>();
		for (const m of baseFiltered) {
			const c = parseCapabilities(m.capabilities);
			for (const meta of CAP_META) {
				if (hasCap(c, meta.key)) capsInData.add(meta.key);
			}
		}

		let filtered = baseFiltered;

		if (selectedProviders.size > 0) {
			filtered = filtered.filter((m) => selectedProviders.has(m.provider_id));
		}

		if (capFilter.size > 0) {
			filtered = filtered.filter((m) =>
				matchesAllCaps(parseCapabilities(m.capabilities), capFilter),
			);
		}

		if (!showAllStatus) {
			filtered = filtered.filter((m) =>
				statusFilter.has("enabled") ? m.enabled : !m.enabled,
			);
		}

		const availability = new Map<CapKey, boolean>();
		for (const m of CAP_META) {
			const testFilter = new Set(capFilter);
			testFilter.add(m.key);
			const hasMatch = baseFiltered.some((model) =>
				matchesAllCaps(parseCapabilities(model.capabilities), testFilter),
			);
			availability.set(m.key, hasMatch);
		}

		const dir = sort.dir === "asc" ? 1 : -1;
		filtered.sort((a, b) => {
			switch (sort.field) {
				case "name":
					return (
						dir *
						(a.name || proxyModelID(a.provider_name, a.model_id)).localeCompare(
							b.name || proxyModelID(b.provider_name, b.model_id),
						)
					);
				case "provider":
					return dir * a.provider_name.localeCompare(b.provider_name);
				case "discovered":
					return (
						dir *
						(new Date(a.last_seen_at).getTime() -
							new Date(b.last_seen_at).getTime())
					);
				case "context":
					return dir * ((a.context_length ?? 0) - (b.context_length ?? 0));
				case "output":
					return (
						dir * ((a.max_output_tokens ?? 0) - (b.max_output_tokens ?? 0))
					);
				case "capabilities": {
					const capsA = Object.values(parseCapabilities(a.capabilities)).filter(
						Boolean,
					).length;
					const capsB = Object.values(parseCapabilities(b.capabilities)).filter(
						Boolean,
					).length;
					return dir * (capsA - capsB);
				}
				case "status":
					return dir * (a.enabled === b.enabled ? 0 : a.enabled ? -1 : 1);
				default:
					return 0;
			}
		});

		return {
			sortedAndFiltered: filtered,
			pillAvailability: availability,
			existingCaps: capsInData,
		};
	}, [
		models,
		searchQuery,
		sort,
		capFilter,
		selectedProviders,
		statusFilter,
		showAllStatus,
	]);

	const totalPages = Math.ceil(sortedAndFiltered.length / pageSize);
	const paginatedModels = sortedAndFiltered.slice(
		(currentPage - 1) * pageSize,
		currentPage * pageSize,
	);

	if (isLoading) {
		return (
			<div className="flex items-center justify-center h-64">
				<div className="animate-spin rounded-full h-12 w-12 border-b-2 border-(--accent)"></div>
			</div>
		);
	}

	const totalEnabled = models?.filter((m) => m.enabled).length ?? 0;
	const totalDisabled = (models?.length ?? 0) - totalEnabled;
	const allSameState = totalEnabled === 0 || totalDisabled === 0;

	return (
		<div className="space-y-4">
			<div>
				<div className="flex items-center gap-3">
					<Bot size={28} strokeWidth={2} className="text-(--accent)" />
					<h1 className="text-3xl font-bold text-white">
						{models?.length ?? 0} Models
					</h1>
					{!allSameState && (
						<span className="inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-xs font-medium bg-gray-700/60 border border-gray-600/50">
							<span className="text-green-400">{totalEnabled} enabled</span>
							<span className="text-gray-600">/</span>
							<span className="text-red-400">{totalDisabled} disabled</span>
						</span>
					)}
				</div>
				<p className="text-gray-400">Discovered models from your providers</p>
			</div>

			<div className="flex items-center gap-4">
				<div className="flex items-center gap-2 shrink-0">
					<FilterInput
						value={searchQuery}
						onChange={(v) => {
							setSearchQuery(v);
							setCurrentPage(1);
						}}
						placeholder="Search models…"
						className="w-[320px]"
						autoFocus
					/>
				</div>
				<div className="flex-1 flex justify-end">
					{models && models.length > 0 && (
						<PaginationBar
							page={currentPage}
							totalPages={totalPages}
							totalItems={sortedAndFiltered.length}
							pageSize={pageSize}
							onPageChange={setCurrentPage}
							onPageSizeChange={(s) => {
								setPageSize(s);
								setCurrentPage(1);
							}}
							label="models"
						/>
					)}
				</div>
			</div>

			<div className="ui-card overflow-hidden">
				<table className="min-w-full table-fixed ui-table">
					<colgroup>
						<col className="w-[32%]" />
						<col className="w-[26%]" />
						<col className="w-[17%]" />
						<col className="w-[6%]" />
						<col className="w-[4%]" />
						<col className="w-[4%]" />
						<col className="w-[11%]" />
					</colgroup>
					<thead>
						<tr>
							<SortableHeader
								label="Model"
								field="name"
								sort={sort}
								onSort={handleSort}
								tooltip="Model name and ID"
							/>
							<th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap ui-table-header-text select-none hover:text-gray-200">
								Capabilities
							</th>
							<th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider ui-table-header-text select-none hover:text-gray-200">
								Provider
							</th>
							<SortableHeader
								label="Discovered"
								field="discovered"
								sort={sort}
								onSort={handleSort}
								tooltip="When the model was last seen/discovered"
							/>
							<SortableHeader
								label="Ctx"
								field="context"
								sort={sort}
								onSort={handleSort}
								tooltip="Maximum context length in tokens"
							/>
							<SortableHeader
								label="Max Out"
								field="output"
								sort={sort}
								onSort={handleSort}
								tooltip="Maximum output tokens"
							/>
							<th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider ui-table-header-text select-none hover:text-gray-200">
								Status
							</th>
						</tr>
						<tr className="ui-table-row-filter">
							<th className="px-4 py-2"></th>
							<th className="px-4 py-2">
								<span className="flex flex-wrap gap-1">
									{CAP_META.filter((m) => existingCaps.has(m.key)).map((m) => {
										const isActive = capFilter.has(m.key);
										const isAvailable = pillAvailability.get(m.key) ?? false;
										const isDisabled = !isActive && !isAvailable;
										return (
											<button
												key={m.key}
												type="button"
												disabled={isDisabled}
												onClick={() => toggleCapFilter(m.key)}
												className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${isActive ? m.style : isDisabled ? m.disabled : m.muted}`}
											>
												{m.label}
											</button>
										);
									})}
									{capFilter.size > 0 && (
										<button
											type="button"
											onClick={() => {
												setCapFilter(new Set());
												setCurrentPage(1);
											}}
											className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium text-gray-400 hover:text-gray-200"
										>
											✕
										</button>
									)}
								</span>
							</th>
							<th className="px-4 py-2">
								<ProviderFilter
									providers={providers}
									selected={selectedProviders}
									onChange={(next) => {
										setSelectedProviders(next);
										setCurrentPage(1);
									}}
								/>
							</th>
							<th className="px-4 py-2"></th>
							<th className="px-4 py-2"></th>
							<th className="px-4 py-2"></th>
							<th className="px-4 py-2">
								<span className="flex flex-wrap items-center gap-1">
									<button
										type="button"
										onClick={() => {
											setStatusFilter((prev) => {
												const next = new Set(prev);
												if (next.has("enabled")) {
													next.delete("enabled");
												} else {
													next.add("enabled");
												}
												return next;
											});
											setCurrentPage(1);
										}}
										className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${
											statusFilter.has("enabled")
												? "bg-green-900/40 text-green-300 border-green-700/50 shadow-[0_0_6px_1px_rgba(34,197,94,0.35)]"
												: "bg-green-900/15 text-green-500/60 border-green-700/25 hover:bg-green-900/25 hover:text-green-400"
										}`}
									>
										Enabled
									</button>
									<button
										type="button"
										onClick={() => {
											setStatusFilter((prev) => {
												const next = new Set(prev);
												if (next.has("disabled")) {
													next.delete("disabled");
												} else {
													next.add("disabled");
												}
												return next;
											});
											setCurrentPage(1);
										}}
										className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${
											statusFilter.has("disabled")
												? "bg-red-900/40 text-red-300 border-red-700/50 shadow-[0_0_6px_1px_rgba(239,68,68,0.35)]"
												: "bg-red-900/15 text-red-500/60 border-red-700/25 hover:bg-red-900/25 hover:text-red-400"
										}`}
									>
										Disabled
									</button>
									{statusFilter.size === 2 && (
										<button
											type="button"
											onClick={() => {
												setStatusFilter(new Set());
												setCurrentPage(1);
											}}
											className="inline-flex items-center justify-center w-4 h-4 rounded text-gray-400 hover:text-gray-200 transition-colors"
										>
											<X size={10} />
										</button>
									)}
								</span>
							</th>
						</tr>
					</thead>
					<tbody>
						{paginatedModels.length > 0 ? (
							paginatedModels.map((model, idx) => {
								const caps = parseCapabilities(model.capabilities);
								return (
									<Row key={model.id} index={idx}>
										<td className="px-4 py-1.5">
											<div className="flex flex-col">
												<button
													type="button"
													onClick={() => setDetailModel(model)}
													title="View model details"
													className="text-left text-sm font-medium text-white hover:text-gray-200 cursor-pointer transition-colors"
												>
													{model.name ||
														proxyModelID(model.provider_name, model.model_id)}
												</button>
												<CopyablePill
													text={proxyModelID(
														model.provider_name,
														model.model_id,
													)}
													textClassName="text-[11px] text-gray-500 font-mono leading-tight"
													tooltip="Click to copy model ID"
												/>
											</div>
										</td>
										<td className="px-4 py-1.5">
											<div className="flex flex-wrap gap-1">
												{CAP_META.map((m) => (
													<CapBadge key={m.key} caps={caps} capKey={m.key} />
												))}
											</div>
										</td>
										<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
											{model.provider_name}
										</td>
										<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-400">
											{formatRelativeTime(model.last_seen_at)}
										</td>
										<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
											{formatTokens(model.context_length)}
										</td>
										<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
											{formatTokens(model.max_output_tokens)}
										</td>
										<td className="px-4 py-1.5 whitespace-nowrap">
											<span
												className={`px-2 py-0.5 text-xs rounded-full ${model.enabled ? "bg-green-900/50 text-green-400" : "bg-red-900/50 text-red-400"}`}
											>
												{model.enabled ? "Enabled" : "Disabled"}
											</span>
										</td>
									</Row>
								);
							})
						) : (
							<EmptyRow
								colSpan={7}
								message={
									searchQuery ||
									selectedProviders.size > 0 ||
									capFilter.size > 0
										? "No models match your filters"
										: "No models discovered yet. Add a provider and discover models."
								}
							/>
						)}
					</tbody>
				</table>
			</div>

			{detailModel && (
				<ModelDetailModal
					model={detailModel}
					onClose={() => setDetailModel(null)}
					onToggle={handleToggleModel}
					onDiscover={handleDiscover}
					onTest={handleTest}
					onToast={toast}
					onUpdate={handleUpdateModel}
					onDelete={(id) => deleteMutation.mutate(id)}
				/>
			)}
		</div>
	);
}
