import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Brain,
	CalendarPlus,
	Clock,
	Coins,
	Gauge,
	Key,
	RotateCcw,
	ShieldCheck,
	Tag,
	Zap,
} from "@/lib/icons";
import { api } from "../../api/client";
import type { VirtualKey } from "../../api/types";
import { ConfirmDeleteButton } from "../../components/ConfirmDeleteButton";
import { InfoHint } from "../../components/InfoHint";
import { DetailItem } from "../../components/LogDetailItem";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { formatNumber } from "../../utils/format";

function SectionHeader({
	icon: Icon,
	label,
	className,
}: {
	icon: React.ComponentType<{ size?: number; className?: string }>;
	label: string;
	className?: string;
}) {
	return (
		<div
			className={`flex items-center gap-2 text-(--accent) mt-4 first:mt-0 ${className ?? ""}`}
		>
			<Icon size={12} className="shrink-0" />
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
	const { t } = useTranslation();
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

	const sortedProviders = (providers ?? [])
		.slice()
		.sort((a, b) => a.name.localeCompare(b.name));

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
			onToast(t("virtualkeys.deleted"), "success");
			onClose();
		},
		onError: (err: Error) => {
			onToast(t("virtualkeys.deleteFailed", { message: err.message }), "error");
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
			onToast(t("virtualkeys.modal.keyUpdated"), "success");
			onClose();
		},
		onError: (err: Error) => {
			onToast(
				t("virtualkeys.modal.keyUpdateFailed", { message: err.message }),
				"error",
			);
		},
	});

	const handleSave = () => {
		if (!editName.trim()) return;
		setProviderError("");
		const allProviderIds = sortedProviders.map((p) => p.id);
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
			setProviderError(t("virtualKeys.create.providerRequired"));
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
			if (!window.confirm(t("virtualkeys.modal.discardChanges"))) return;
		}
		onClose();
	};

	return (
		<Modal
			title={t("virtualkeys.modal.detailTitle")}
			onClose={handleClose}
			maxWidth="max-w-lg"
			scrollable
		>
			<div className="space-y-2 mb-6">
				{editing ? (
					<>
						<SectionHeader
							icon={Key}
							label={t("virtualkeys.modal.sections.identity")}
						/>
						<div>
							<label
								htmlFor="vk-detail-name"
								className="block text-sm font-medium text-gray-300 mb-1"
							>
								{t("virtualkeys.modal.form.name")}
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

						<SectionHeader
							icon={Gauge}
							label={t("virtualkeys.modal.sections.rateLimits")}
						/>
						<div className="grid grid-cols-2 gap-4">
							<div>
								<label
									htmlFor="vk-detail-rps"
									className="block text-sm font-medium text-gray-300 mb-1"
								>
									{t("virtualkeys.modal.form.rateLimitRps")}
								</label>
								<input
									id="vk-detail-rps"
									type="number"
									min="0"
									value={editRps}
									onChange={(e) => setEditRps(e.target.value)}
									className="ui-input"
									placeholder={t("virtualkeys.modal.form.placeholderGlobal")}
								/>
							</div>
							<div>
								<label
									htmlFor="vk-detail-burst"
									className="block text-sm font-medium text-gray-300 mb-1"
								>
									{t("virtualkeys.modal.form.rateLimitBurst")}
								</label>
								<input
									id="vk-detail-burst"
									type="number"
									min="0"
									value={editBurst}
									onChange={(e) => setEditBurst(e.target.value)}
									className="ui-input"
									placeholder={t("virtualkeys.modal.form.placeholderGlobal")}
								/>
							</div>
						</div>
						<div>
							<div className="flex items-center justify-between mb-1">
								<span className="text-sm font-medium text-gray-300">
									{t("virtualkeys.modal.sections.providerAccess")}
								</span>
								{excludedProviders.length > 0 && (
									<button
										type="button"
										onClick={resetProviders}
										className="text-gray-500 hover:text-gray-300 transition-colors"
										aria-label={t("virtualkeys.modal.form.restoreAccess")}
										title={t("virtualkeys.modal.form.restoreAccess")}
									>
										<RotateCcw size={14} />
									</button>
								)}
							</div>
							<p className="text-xs text-gray-500 mb-2">
								{t("virtualkeys.modal.form.providerInstructions")}
							</p>
							{sortedProviders.length === 0 ? (
								<p className="text-xs text-gray-500 italic">
									{t("virtualkeys.modal.form.noProviders")}
								</p>
							) : (
								<div className="flex flex-wrap gap-1.5 max-h-40 overflow-y-auto">
									{sortedProviders.map((provider) => {
										const isExcluded = excludedProviders.includes(provider.id);
										return (
											<button
												key={provider.id}
												type="button"
												onClick={() => toggleProvider(provider.id)}
												aria-pressed={isExcluded}
												className={`inline-flex items-center px-2 py-px leading-[1.6] text-xs font-medium transition-colors ui-badge
													${
														isExcluded
															? "ui-badge-neutral line-through opacity-60 hover:brightness-125"
															: "ui-badge-accent"
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

						<SectionHeader
							icon={BrainSlashIcon}
							label={t("virtualkeys.modal.form.stripReasoning")}
							className="mb-2"
						/>
						<div>
							<div className="flex items-center gap-3">
								<Toggle
									checked={editStripReasoning}
									onChange={setEditStripReasoning}
									size="sm"
									ariaLabel={t("virtualkeys.modal.form.stripReasoning")}
								/>
								<span className="text-sm text-gray-200">
									{editStripReasoning
										? t("common.enabled")
										: t("common.disabled")}
								</span>
							</div>
							<p className="text-xs text-gray-400 mt-1.5">
								{t("virtualkeys.modal.form.stripReasoningDescription")}
							</p>
						</div>
					</>
				) : (
					<div className="grid grid-cols-2 gap-2">
						<DetailItem
							icon={Tag}
							label={t("virtualkeys.modal.form.name")}
							value={vk.name}
						/>
						<DetailItem icon={Key} label={t("virtualkeys.modal.labels.key")}>
							<div
								className="text-sm font-mono text-(--text-primary) truncate select-none"
								title={t("virtualkeys.tooltip.keyHashed")}
							>
								{vk.key_preview}
							</div>
						</DetailItem>
						<DetailItem
							icon={Gauge}
							label={t("virtualKeys.detail.rps")}
							labelExtra={<InfoHint tooltip={t("virtualkeys.tooltip.rps")} />}
							value={
								vk.rate_limit_rps != null
									? String(vk.rate_limit_rps)
									: t("common.global")
							}
							mono
						/>
						<DetailItem
							icon={Zap}
							label={t("virtualKeys.detail.burst")}
							labelExtra={<InfoHint tooltip={t("virtualkeys.tooltip.burst")} />}
							value={
								vk.rate_limit_burst != null
									? String(vk.rate_limit_burst)
									: t("common.global")
							}
							mono
						/>
						<DetailItem
							icon={Coins}
							label={t("virtualkeys.modal.labels.tokensConsumed")}
							value={formatNumber(vk.tokens_used)}
							mono
						/>
						<DetailItem
							icon={Clock}
							label={t("virtualkeys.modal.labels.lastUsed")}
							value={
								vk.last_used_at
									? new Date(vk.last_used_at).toLocaleString()
									: t("common.never")
							}
						/>
						<DetailItem
							icon={CalendarPlus}
							label={t("virtualkeys.modal.labels.created")}
							value={new Date(vk.created_at).toLocaleString()}
						/>
						<DetailItem
							icon={BrainSlashIcon}
							label={t("virtualkeys.modal.form.stripReasoning")}
							value={
								vk.strip_reasoning ? t("common.enabled") : t("common.disabled")
							}
						/>
						<DetailItem
							icon={ShieldCheck}
							label={t("virtualkeys.modal.sections.providerAccess")}
							className="col-span-2"
						>
							{sortedProviders.length === 0 ? (
								<p className="text-xs text-gray-500 italic">
									{t("virtualkeys.modal.noProvidersConfigured")}
								</p>
							) : (
								<div className="flex flex-wrap gap-1.5">
									{sortedProviders.map((provider) => {
										const isAllowed =
											!vk.allowed_providers ||
											vk.allowed_providers.includes(provider.id);
										return (
											<span
												key={provider.id}
												className={`inline-flex items-center px-2 py-px leading-[1.6] text-xs font-medium ui-badge
													${
														isAllowed
															? "ui-badge-accent"
															: "ui-badge-neutral line-through opacity-60"
													}`}
											>
												{provider.name}
											</span>
										);
									})}
								</div>
							)}
						</DetailItem>
					</div>
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
							{t("common.cancel")}
						</button>
						<button
							type="button"
							onClick={handleSave}
							disabled={!hasChanges || updateMutation.isPending}
							className="ui-btn ui-btn-primary disabled:opacity-50"
						>
							{updateMutation.isPending
								? t("common.saving")
								: t("common.saveChanges")}
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
								? t("virtualkeys.modal.loadingProviders")
								: undefined
						}
					>
						{t("common.edit")}
					</button>
				)}
			</div>
		</Modal>
	);
}
