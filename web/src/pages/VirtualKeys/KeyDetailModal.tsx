import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Brain, Gauge, Key, RotateCcw, ShieldCheck, Zap } from "lucide-react";
import { useState } from "react";
import { api } from "../../api/client";
import type { VirtualKey } from "../../api/types";
import { ConfirmDeleteButton } from "../../components/ConfirmDeleteButton";
import { CopyablePill } from "../../components/CopyablePill";
import { Modal } from "../../components/Modal";
import { formatNumber } from "../../utils/format";

function SectionHeader({
	icon: Icon,
	label,
}: {
	icon: React.ComponentType<{ size?: number; className?: string }>;
	label: string;
}) {
	return (
		<div className="flex items-center gap-2 text-(--accent) mt-4 first:mt-0">
			<Icon size={14} className="shrink-0" />
			<span className="text-xs font-semibold uppercase tracking-wider">
				{label}
			</span>
		</div>
	);
}

function BrainSlashIcon({
	size = 14,
	className = "",
}: {
	size?: number;
	className?: string;
}) {
	return (
		<span
			className={`relative inline-block ${className}`}
			style={{ width: size, height: size }}
		>
			<Brain size={size} />
			<span className="absolute inset-0 flex items-center justify-center pointer-events-none">
				<span className="w-full h-[1.5px] bg-current rotate-45" />
			</span>
		</span>
	);
}

function InfoItem({
	label,
	value,
	mono = false,
}: {
	label: string;
	value: string;
	mono?: boolean;
}) {
	return (
		<div>
			<span className="text-xs text-gray-500 uppercase tracking-wider">
				{label}
			</span>
			<p className={`text-sm text-gray-200 mt-0.5 ${mono ? "font-mono" : ""}`}>
				{value}
			</p>
		</div>
	);
}

export function KeyDetailModal({
	vk,
	onClose,
	onToast,
}: {
	vk: VirtualKey;
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
	const queryClient = useQueryClient();
	const [editing, setEditing] = useState(false);
	const [editName, setEditName] = useState(vk.name);
	const [editRps, setEditRps] = useState(vk.rate_limit_rps?.toString() ?? "");
	const [editBurst, setEditBurst] = useState(
		vk.rate_limit_burst?.toString() ?? "",
	);
	const [excludedProviders, setExcludedProviders] = useState<string[]>([]);
	const [originalExcluded, setOriginalExcluded] = useState<string[]>([]);
	const [providerError, setProviderError] = useState("");
	const [editStripReasoning, setEditStripReasoning] = useState(
		vk.strip_reasoning,
	);

	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});

	const availableProviders = providers ?? [];

	const toggleProvider = (providerId: string) => {
		setExcludedProviders((prev) =>
			prev.includes(providerId)
				? prev.filter((id) => id !== providerId)
				: [...prev, providerId],
		);
	};

	const resetProviders = () => setExcludedProviders([]);

	const deleteMutation = useMutation({
		mutationFn: () => api.virtualKeys.delete(vk.id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["virtualKeys"] });
			onToast("Virtual key deleted", "success");
			onClose();
		},
		onError: (err: Error) => {
			onToast(`Failed to delete: ${err.message}`, "error");
		},
	});

	const updateMutation = useMutation({
		mutationFn: ({
			name,
			rate_limit_rps,
			rate_limit_burst,
			allowed_providers,
			strip_reasoning,
		}: {
			name: string;
			rate_limit_rps?: number | null;
			rate_limit_burst?: number | null;
			allowed_providers?: string[] | null;
			strip_reasoning?: boolean;
		}) =>
			api.virtualKeys.update(vk.id, {
				name,
				rate_limit_rps,
				rate_limit_burst,
				allowed_providers,
				strip_reasoning,
			}),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["virtualKeys"] });
			onToast("Virtual key updated", "success");
			onClose();
		},
		onError: (err: Error) => {
			onToast(`Failed: ${err.message}`, "error");
		},
	});

	const handleSave = () => {
		if (!editName.trim()) return;
		setProviderError("");
		const allProviderIds = availableProviders.map((p) => p.id);
		let allowedProviders: string[] | null;
		if (excludedProviders.length > 0) {
			allowedProviders = allProviderIds.filter(
				(id) => !excludedProviders.includes(id),
			);
		} else if (providersChanged) {
			// User removed all exclusions → send null (no restriction)
			allowedProviders = null;
		} else {
			// No change to providers → preserve original value
			allowedProviders = vk.allowed_providers ?? null;
		}
		if (allowedProviders && allowedProviders.length === 0) {
			setProviderError("At least one provider must remain accessible");
			return;
		}
		updateMutation.mutate({
			name: editName.trim(),
			rate_limit_rps: editRps !== "" ? parseFloat(editRps) : null,
			rate_limit_burst: editBurst !== "" ? parseInt(editBurst, 10) : null,
			allowed_providers: allowedProviders,
			strip_reasoning: editStripReasoning,
		});
	};

	const handleCancelEdit = () => {
		setEditName(vk.name);
		setEditRps(vk.rate_limit_rps?.toString() ?? "");
		setEditBurst(vk.rate_limit_burst?.toString() ?? "");
		setExcludedProviders([]);
		setOriginalExcluded([]);
		setEditStripReasoning(vk.strip_reasoning);
		setEditing(false);
	};

	const startEditing = () => {
		setEditName(vk.name);
		setEditRps(vk.rate_limit_rps?.toString() ?? "");
		setEditBurst(vk.rate_limit_burst?.toString() ?? "");
		setEditStripReasoning(vk.strip_reasoning);
		setProviderError("");
		// Compute excluded providers from the VK's allowed_providers.
		// If the key has restrictions but providers haven't loaded yet,
		// we must not proceed — that would silently clear restrictions.
		if (vk.allowed_providers && vk.allowed_providers.length > 0 && !providers) {
			return;
		}
		if (vk.allowed_providers && providers) {
			const allIds = providers.map((p) => p.id);
			const excluded = allIds.filter(
				(id) => !vk.allowed_providers?.includes(id),
			);
			setExcludedProviders(excluded);
			setOriginalExcluded(excluded);
		} else {
			setExcludedProviders([]);
			setOriginalExcluded([]);
		}
		setEditing(true);
	};

	const providersChanged =
		excludedProviders.length !== originalExcluded.length ||
		excludedProviders.some((id) => !originalExcluded.includes(id));

	const hasChanges =
		editName !== vk.name ||
		editRps !== (vk.rate_limit_rps?.toString() ?? "") ||
		editBurst !== (vk.rate_limit_burst?.toString() ?? "") ||
		providersChanged ||
		editStripReasoning !== vk.strip_reasoning;

	const handleClose = () => {
		if (editing && hasChanges) {
			if (!window.confirm("Discard unsaved changes?")) return;
		}
		onClose();
	};

	return (
		<Modal
			title="Virtual Key Details"
			onClose={handleClose}
			maxWidth="max-w-lg"
			scrollable
		>
			<div className="space-y-2 mb-6">
				{editing ? (
					<>
						<SectionHeader icon={Key} label="Identity" />
						<div>
							<label
								htmlFor="vk-detail-name"
								className="block text-sm font-medium text-gray-300 mb-1"
							>
								Name
							</label>
							<input
								id="vk-detail-name"
								type="text"
								required
								maxLength={100}
								value={editName}
								onChange={(e) => setEditName(e.target.value)}
								className="ui-input"
							/>
						</div>

						<SectionHeader icon={Gauge} label="Rate Limits" />
						<div className="grid grid-cols-2 gap-4">
							<div>
								<label
									htmlFor="vk-detail-rps"
									className="block text-sm font-medium text-gray-300 mb-1"
								>
									Rate Limit RPS (requests/sec)
								</label>
								<input
									id="vk-detail-rps"
									type="number"
									min="0"
									value={editRps}
									onChange={(e) => setEditRps(e.target.value)}
									className="ui-input"
									placeholder="Use global setting"
								/>
							</div>
							<div>
								<label
									htmlFor="vk-detail-burst"
									className="block text-sm font-medium text-gray-300 mb-1"
								>
									Rate Limit Burst (max concurrent)
								</label>
								<input
									id="vk-detail-burst"
									type="number"
									min="0"
									value={editBurst}
									onChange={(e) => setEditBurst(e.target.value)}
									className="ui-input"
									placeholder="Use global setting"
								/>
							</div>
						</div>
						<div>
							<div className="flex items-center justify-between mb-1">
								<span className="text-sm font-medium text-gray-300">
									Provider Access
								</span>
								{excludedProviders.length > 0 && (
									<button
										type="button"
										onClick={resetProviders}
										className="text-gray-500 hover:text-gray-300 transition-colors cursor-pointer"
										aria-label="Restore access to all providers"
										title="Restore access to all providers"
									>
										<RotateCcw size={14} />
									</button>
								)}
							</div>
							<p className="text-xs text-gray-500 mb-2">
								Click a provider to restrict access. All are accessible by
								default.
							</p>
							{availableProviders.length === 0 ? (
								<p className="text-xs text-gray-500 italic">
									No providers available.
								</p>
							) : (
								<div className="flex flex-wrap gap-1.5">
									{availableProviders.map((provider) => {
										const isExcluded = excludedProviders.includes(provider.id);
										return (
											<button
												key={provider.id}
												type="button"
												onClick={() => toggleProvider(provider.id)}
												aria-pressed={isExcluded}
												className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium cursor-pointer transition-colors
													${
														isExcluded
															? "bg-gray-800/60 text-gray-500 border border-gray-700/60 line-through hover:bg-gray-700/60"
															: "bg-(--accent)/20 text-(--accent) border border-(--accent)/40"
													}`}
											>
												{provider.name}
											</button>
										);
									})}
								</div>
							)}
						</div>
						{providerError && (
							<p className="text-xs text-red-400 mt-1">{providerError}</p>
						)}

						<SectionHeader icon={BrainSlashIcon} label="Strip Reasoning" />
						<div>
							<div className="flex items-center gap-3">
								<button
									type="button"
									onClick={() => setEditStripReasoning(!editStripReasoning)}
									aria-pressed={editStripReasoning}
									aria-label={
										editStripReasoning
											? "Disable strip reasoning"
											: "Enable strip reasoning"
									}
									className={`relative inline-flex items-center h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none ${
										editStripReasoning
											? "bg-(--accent) shadow-[var(--glow-accent)]"
											: "bg-gray-600"
									}`}
								>
									<span
										aria-hidden="true"
										className={`pointer-events-none block h-3.5 w-3.5 transform rounded-full bg-white shadow-sm ring-0 transition-transform duration-200 ease-in-out ${
											editStripReasoning ? "translate-x-4" : "translate-x-0"
										}`}
									/>
								</button>
								<span className="text-sm text-gray-200">
									{editStripReasoning ? "Enabled" : "Disabled"}
								</span>
							</div>
							<p className="text-xs text-gray-400 mt-1.5">
								When enabled, reasoning/thinking tokens are removed from
								streaming responses for clients that cannot handle them (e.g.,
								Warp.dev).
							</p>
						</div>
					</>
				) : (
					<>
						{/* Identity Section */}
						<SectionHeader icon={Key} label="Identity" />
						<div className="grid grid-cols-2 gap-4">
							<InfoItem label="Name" value={vk.name} />
							<div>
								<span className="text-xs text-gray-500 uppercase tracking-wider">
									Key
								</span>
								<div className="mt-0.5">
									<CopyablePill
										text={vk.key_preview}
										displayText={vk.key_preview}
										textClassName="text-sm font-mono text-gray-200"
									/>
								</div>
							</div>
						</div>

						{/* Rate Limits Section */}
						<SectionHeader icon={Gauge} label="Rate Limits" />
						<div className="grid grid-cols-2 gap-4">
							<InfoItem
								label="RPS"
								value={
									vk.rate_limit_rps != null
										? String(vk.rate_limit_rps)
										: "Global"
								}
								mono
							/>
							<InfoItem
								label="Burst"
								value={
									vk.rate_limit_burst != null
										? String(vk.rate_limit_burst)
										: "Global"
								}
								mono
							/>
						</div>

						{/* Usage Section */}
						<SectionHeader icon={Zap} label="Usage" />
						<div className="grid grid-cols-2 gap-4">
							<InfoItem
								label="Tokens Consumed"
								value={formatNumber(vk.tokens_used)}
							/>
							<InfoItem
								label="Last Used"
								value={
									vk.last_used_at
										? new Date(vk.last_used_at).toLocaleString()
										: "Never"
								}
							/>
							<InfoItem
								label="Created"
								value={new Date(vk.created_at).toLocaleString()}
							/>
						</div>

						<SectionHeader icon={ShieldCheck} label="Provider Access" />
						<div>
							{availableProviders.length === 0 ? (
								<p className="text-xs text-gray-500 italic">
									No providers configured.
								</p>
							) : (
								<div className="flex flex-wrap gap-1.5">
									{availableProviders.map((provider) => {
										const isAllowed =
											!vk.allowed_providers ||
											vk.allowed_providers.includes(provider.id);
										return (
											<span
												key={provider.id}
												className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium
													${
														isAllowed
															? "bg-(--accent)/20 text-(--accent) border border-(--accent)/40"
															: "bg-gray-800/60 text-gray-500 border border-gray-700/60 line-through"
													}`}
											>
												{provider.name}
											</span>
										);
									})}
								</div>
							)}
						</div>

						<SectionHeader icon={BrainSlashIcon} label="Strip Reasoning" />
						<div>
							<InfoItem
								label="Strip Reasoning"
								value={vk.strip_reasoning ? "Enabled" : "Disabled"}
							/>
						</div>
					</>
				)}
			</div>

			<div className="flex justify-between items-center">
				<ConfirmDeleteButton
					onConfirm={() => deleteMutation.mutate()}
					loading={deleteMutation.isPending}
				/>
				{editing ? (
					<div className="flex space-x-3">
						<button
							type="button"
							onClick={handleCancelEdit}
							className="ui-btn ui-btn-secondary"
						>
							Cancel
						</button>
						<button
							type="button"
							onClick={handleSave}
							disabled={!hasChanges || updateMutation.isPending}
							className="ui-btn ui-btn-primary disabled:opacity-50"
						>
							{updateMutation.isPending ? "Saving..." : "Save Changes"}
						</button>
					</div>
				) : (
					<button
						type="button"
						onClick={startEditing}
						className="ui-btn ui-btn-secondary"
						disabled={
							!!vk.allowed_providers &&
							vk.allowed_providers.length > 0 &&
							!providers
						}
						title={
							vk.allowed_providers &&
							vk.allowed_providers.length > 0 &&
							!providers
								? "Loading providers..."
								: undefined
						}
					>
						Edit
					</button>
				)}
			</div>
		</Modal>
	);
}
