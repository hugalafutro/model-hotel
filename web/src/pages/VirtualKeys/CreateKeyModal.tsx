import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { BrainSlashIcon, ChevronRight, RotateCcw } from "@/lib/icons";
import { api } from "../../api/client";
import type { VirtualKey } from "../../api/types";
import { CopyablePill } from "../../components/CopyablePill";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { useIdentity } from "../../context/IdentityContext";
import { UsageSnippets } from "./UsageSnippets";

export function CreateKeyModal({
	onClose,
	onToast,
}: {
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
	const queryClient = useQueryClient();
	const { t } = useTranslation();
	const { isAdmin, me } = useIdentity();
	const [name, setName] = useState("");
	const [ownerId, setOwnerId] = useState("");
	const [rateLimitRps, setRateLimitRps] = useState<string>("");
	const [rateLimitBurst, setRateLimitBurst] = useState<string>("");
	const [rateLimitTpm, setRateLimitTpm] = useState<string>("");
	const [excludedProviders, setExcludedProviders] = useState<string[]>([]);
	const [stripReasoning, setStripReasoning] = useState(false);
	const [createdKey, setCreatedKey] = useState<VirtualKey | null>(null);
	const [showExamples, setShowExamples] = useState(false);
	const [providerError, setProviderError] = useState("");

	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});

	// Owner assignment is an admin concern: non-admins always create keys as
	// their own (the server forces it) and cannot read the roster anyway.
	const { data: users } = useQuery({
		queryKey: ["users"],
		queryFn: () => api.users.list(),
		enabled: isAdmin,
	});

	const sortedProviders = (providers ?? [])
		.slice()
		.sort((a, b) => a.name.localeCompare(b.name));

	// A virtual key can never name a provider outside its OWNER's account cap:
	// the API resolves the write against that cap and the proxy intersects it
	// again per request. Mirroring it here only saves the user a round trip into
	// a rejection; it decides nothing.
	const ownerAccount = ownerId
		? (users ?? []).find((u) => u.id === ownerId)
		: undefined;
	const ownerCap = ownerAccount?.allowed_providers ?? null;
	// Non-admins cannot choose an owner (the server writes the key to them), so
	// their own account cap is the one that will apply.
	const cap = ownerCap ?? (isAdmin ? null : (me?.allowed_providers ?? null));
	const capIsOtherOwner =
		ownerCap !== null && ownerAccount?.username !== me?.username;
	const capNoteId = "vk-create-provider-cap-note";
	const capNote = capIsOtherOwner
		? t("virtualkeys.modal.form.providerOutsideOwnerAccess")
		: t("virtualkeys.modal.form.providerOutsideAccountAccess");
	const isOutsideCap = (id: string) => cap !== null && !cap.includes(id);
	const outsideCapIds = sortedProviders
		.map((p) => p.id)
		.filter((id) => isOutsideCap(id));
	// An out-of-cap provider is always excluded, on top of whatever the user
	// picked. Derived rather than held in state so that switching the owner
	// re-applies the new owner's cap without stomping the user's own picks.
	const effectiveExcluded =
		outsideCapIds.length > 0
			? Array.from(new Set([...excludedProviders, ...outsideCapIds]))
			: excludedProviders;

	const toggleProvider = (providerId: string) => {
		// Out-of-cap chips stay focusable (aria-disabled, not disabled) so their
		// explanation is reachable, so the choke point on activating them is here.
		if (isOutsideCap(providerId)) return;
		setExcludedProviders((prev) =>
			prev.includes(providerId)
				? prev.filter((id) => id !== providerId)
				: [...prev, providerId],
		);
	};

	const resetProviders = () => setExcludedProviders([]);

	const createMutation = useMutation({
		mutationFn: ({
			name,
			rate_limit_rps,
			rate_limit_burst,
			rate_limit_tpm,
			allowed_providers,
			strip_reasoning,
			owner_user_id,
		}: {
			name: string;
			rate_limit_rps?: number | null;
			rate_limit_burst?: number | null;
			rate_limit_tpm?: number | null;
			allowed_providers?: string[] | null;
			strip_reasoning?: boolean;
			owner_user_id?: string | null;
		}) =>
			api.virtualKeys.create(
				name,
				rate_limit_rps,
				rate_limit_burst,
				rate_limit_tpm,
				allowed_providers,
				strip_reasoning,
				owner_user_id,
			),
		onSuccess: (vk) => {
			setCreatedKey(vk);
			queryClient.invalidateQueries({ queryKey: ["virtualKeys"] });
			onToast(t("virtualkeys.modal.keyCreated"), "success");
		},
		onError: (err: Error) => {
			onToast(
				t("virtualkeys.modal.keyCreatedFailed", { message: err.message }),
				"error",
			);
		},
	});

	const handleSubmit = (e: React.SubmitEvent) => {
		e.preventDefault();
		if (!name.trim()) return;
		setProviderError("");
		const allProviderIds = sortedProviders.map((p) => p.id);
		const allowedProviders =
			effectiveExcluded.length > 0
				? allProviderIds.filter((id) => !effectiveExcluded.includes(id))
				: null;
		if (allowedProviders && allowedProviders.length === 0) {
			setProviderError(t("virtualKeys.create.providerRequired"));
			return;
		}
		createMutation.mutate({
			name: name.trim(),
			rate_limit_rps: rateLimitRps !== "" ? parseFloat(rateLimitRps) : null,
			rate_limit_burst:
				rateLimitBurst !== "" ? parseInt(rateLimitBurst, 10) : null,
			rate_limit_tpm: rateLimitTpm !== "" ? parseInt(rateLimitTpm, 10) : null,
			allowed_providers: allowedProviders,
			strip_reasoning: stripReasoning,
			owner_user_id: isAdmin && ownerId !== "" ? ownerId : null,
		});
	};

	return (
		<Modal
			title={
				createdKey
					? t("virtualkeys.modal.createdTitle")
					: t("virtualkeys.modal.createTitle")
			}
			closeOnBackdrop={!createdKey}
			onClose={onClose}
			maxWidth={createdKey ? "max-w-2xl" : "max-w-md"}
			scrollable={!!createdKey}
		>
			{createdKey ? (
				<>
					<div className="bg-red-500/10 border-2 border-red-500/40 rounded-[var(--radius-box)] p-3 mb-4">
						<p className="text-red-400 font-semibold text-sm">
							{t("virtualkeys.modal.warningTitle")}
						</p>
						<p className="text-red-400/70 text-xs mt-1">
							{t("virtualkeys.modal.warningText")}
						</p>
					</div>
					<div className="bg-gray-950 rounded-[var(--radius-box)] p-3 mb-4">
						{createdKey.key && (
							<CopyablePill
								text={createdKey.key}
								displayText={createdKey.key}
								textClassName="text-sm text-green-400 font-mono break-all"
								tooltip={t("virtualKeys.create.clickToCopyKey")}
							/>
						)}
					</div>
					{createdKey.key && (
						<div className="mb-4">
							<button
								type="button"
								onClick={() => setShowExamples((v) => !v)}
								aria-expanded={showExamples}
								className="ui-link-accent inline-flex items-center gap-1.5 text-sm font-medium"
							>
								<ChevronRight
									size={14}
									className={`transition-transform ${
										showExamples ? "rotate-90" : ""
									}`}
								/>
								{t("virtualkeys.modal.usageExamples")}
							</button>
							{showExamples && (
								<div className="mt-3">
									<UsageSnippets apiKey={createdKey.key} />
								</div>
							)}
						</div>
					)}
					<div className="flex justify-end">
						<button
							type="button"
							onClick={onClose}
							className="ui-btn ui-btn-secondary"
						>
							{t("common.done")}
						</button>
					</div>
				</>
			) : (
				<form onSubmit={handleSubmit} className="space-y-4">
					<div>
						<label
							htmlFor="vk-name"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							{t("virtualkeys.modal.nameLabel")}
						</label>
						<input
							id="vk-name"
							type="text"
							required
							maxLength={100}
							value={name}
							onChange={(e) => setName(e.target.value)}
							className="ui-input"
							placeholder={t("virtualkeys.modal.form.namePlaceholder")}
						/>
					</div>
					{isAdmin && (
						<div>
							<label
								htmlFor="vk-owner"
								className="block text-sm font-medium text-gray-300 mb-1"
							>
								{t("virtualkeys.modal.form.owner")}
							</label>
							<select
								id="vk-owner"
								value={ownerId}
								onChange={(e) => setOwnerId(e.target.value)}
								className="ui-input"
								data-testid="vk-owner-select"
							>
								<option value="">
									{t("virtualkeys.modal.form.ownerNone")}
								</option>
								{(users ?? []).map((u) => (
									<option key={u.id} value={u.id}>
										{u.username}
									</option>
								))}
							</select>
							<p className="text-xs text-gray-500 mt-1">
								{t("virtualkeys.modal.form.ownerHint")}
							</p>
						</div>
					)}
					<div>
						<label
							htmlFor="vk-rate-limit-rps"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							{t("virtualkeys.modal.rateLimitRpsLabel")}
						</label>
						<input
							id="vk-rate-limit-rps"
							type="number"
							min="0"
							value={rateLimitRps}
							onChange={(e) => setRateLimitRps(e.target.value)}
							className="ui-input"
							placeholder={t("virtualkeys.modal.form.placeholderGlobal")}
						/>
					</div>
					<div>
						<label
							htmlFor="vk-rate-limit-burst"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							{t("virtualkeys.modal.rateLimitBurstLabel")}
						</label>
						<input
							id="vk-rate-limit-burst"
							type="number"
							min="1"
							value={rateLimitBurst}
							onChange={(e) => setRateLimitBurst(e.target.value)}
							className="ui-input"
							placeholder={t("virtualkeys.modal.form.placeholderGlobal")}
						/>
					</div>
					<div>
						<label
							htmlFor="vk-rate-limit-tpm"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							{t("virtualkeys.modal.rateLimitTpmLabel")}
						</label>
						<input
							id="vk-rate-limit-tpm"
							type="number"
							min="1"
							value={rateLimitTpm}
							onChange={(e) => setRateLimitTpm(e.target.value)}
							className="ui-input"
							placeholder={t("virtualkeys.modal.form.placeholderGlobal")}
						/>
					</div>
					<div>
						<div className="flex items-center justify-between mb-1">
							<span className="text-sm font-medium text-gray-300">
								{t("virtualkeys.modal.form.providerAccess")}
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
							{t("virtualkeys.modal.providerInstructionsText")}
						</p>
						{outsideCapIds.length > 0 && (
							<p
								id={capNoteId}
								data-testid="vk-provider-cap-note"
								data-cap-source={capIsOtherOwner ? "owner" : "account"}
								className="text-xs text-gray-500 italic mb-2"
							>
								{capNote}
							</p>
						)}
						{sortedProviders.length === 0 ? (
							<p className="text-xs text-gray-500 italic">
								{t("virtualkeys.modal.form.noProviders")}
							</p>
						) : (
							<div className="flex flex-wrap gap-1.5 max-h-40 overflow-y-auto">
								{sortedProviders.map((provider) => {
									const outsideCap = isOutsideCap(provider.id);
									const isExcluded = effectiveExcluded.includes(provider.id);
									return (
										<button
											key={provider.id}
											type="button"
											data-testid={`vk-provider-option-${provider.id}`}
											// aria-disabled, not disabled: a disabled button drops out
											// of the tab order, which would put both the title and the
											// note out of reach of a keyboard or a screen reader.
											// toggleProvider is what makes it inert.
											{...(outsideCap
												? {
														"data-outside-cap": "true",
														"aria-disabled": true,
														"aria-describedby": capNoteId,
													}
												: {})}
											title={outsideCap ? capNote : undefined}
											onClick={() => toggleProvider(provider.id)}
											aria-pressed={isExcluded}
											className={`inline-flex items-center px-2 py-px leading-[1.6] text-xs font-medium transition-colors ui-badge
												${
													isExcluded
														? "ui-badge-neutral line-through opacity-60"
														: "ui-badge-accent"
												}
												${outsideCap ? "cursor-not-allowed" : isExcluded ? "hover:brightness-125" : ""}`}
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

					<div>
						<div className="flex items-center gap-2 text-(--accent) mb-2">
							<BrainSlashIcon size={12} className="shrink-0" />
							<span className="text-xs font-semibold uppercase tracking-wider">
								{t("virtualkeys.modal.form.stripReasoning")}
							</span>
						</div>
						<div className="flex items-center gap-3">
							<Toggle
								checked={stripReasoning}
								onChange={setStripReasoning}
								size="sm"
								ariaLabel={t("virtualkeys.modal.form.stripReasoning")}
							/>
							<span className="text-sm text-gray-200">
								{stripReasoning ? t("common.enabled") : t("common.disabled")}
							</span>
						</div>
						<p className="text-xs text-gray-400 mt-1.5">
							{t("virtualkeys.modal.stripReasoningDescriptionText")}
						</p>
					</div>

					<div className="flex space-x-3 justify-end pt-2">
						<button
							type="button"
							onClick={onClose}
							className="ui-btn ui-btn-secondary"
						>
							{t("common.cancel")}
						</button>
						<button
							type="submit"
							disabled={createMutation.isPending}
							className="ui-btn ui-btn-primary"
						>
							{createMutation.isPending
								? t("common.creating")
								: t("virtualKeys.create.createKey")}
						</button>
					</div>
				</form>
			)}
		</Modal>
	);
}
