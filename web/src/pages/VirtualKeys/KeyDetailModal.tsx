import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	BrainSlashIcon,
	CalendarPlus,
	Clock,
	Coins,
	Fingerprint,
	Gauge,
	Key,
	RotateCcw,
	ShieldCheck,
	Tag,
	UserRound,
	Zap,
} from "@/lib/icons";
import { api } from "../../api/client";
import type { VirtualKey } from "../../api/types";
import { ConfirmDeleteButton } from "../../components/ConfirmDeleteButton";
import { CopyablePill } from "../../components/CopyablePill";
import { InfoHint } from "../../components/InfoHint";
import { DetailItem } from "../../components/LogDetailItem";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { useIdentity } from "../../context/IdentityContext";
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

export function KeyDetailModal({
	vk,
	onClose,
	onToast,
	managed,
}: {
	vk: VirtualKey;
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
	// When true this key is managed by the fleet primary: edit and delete are
	// hidden, since local changes are replaced on the next config sync.
	managed?: boolean;
}) {
	const queryClient = useQueryClient();
	const { t } = useTranslation();
	const { isAdmin, me } = useIdentity();
	const [editing, setEditing] = useState(false);
	const [editName, setEditName] = useState(vk.name);
	const [editOwnerId, setEditOwnerId] = useState(vk.owner_user_id ?? "");
	const [editRps, setEditRps] = useState(vk.rate_limit_rps?.toString() ?? "");
	const [editBurst, setEditBurst] = useState(
		vk.rate_limit_burst?.toString() ?? "",
	);
	const [editTpm, setEditTpm] = useState(vk.rate_limit_tpm?.toString() ?? "");
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

	// Roster for the owner select; admin-only, like the assignment itself.
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
	const ownerAccount = editOwnerId
		? (users ?? []).find((u) => u.id === editOwnerId)
		: undefined;
	const ownerCap = ownerAccount?.allowed_providers ?? null;
	// Non-admins cannot reassign a key (the server writes it to them whatever the
	// body says), so their own account cap is the one that will apply.
	const cap = ownerCap ?? (isAdmin ? null : (me?.allowed_providers ?? null));
	const capIsOtherOwner =
		ownerCap !== null && ownerAccount?.username !== me?.username;
	const capNoteId = "vk-detail-provider-cap-note";
	const capNote = capIsOtherOwner
		? t("virtualkeys.modal.form.providerOutsideOwnerAccess")
		: t("virtualkeys.modal.form.providerOutsideAccountAccess");
	const isOutsideCap = (id: string) => cap !== null && !cap.includes(id);
	const outsideCapIds = sortedProviders
		.map((p) => p.id)
		.filter((id) => isOutsideCap(id));
	// An out-of-cap provider is always excluded, on top of whatever the user
	// picked. This is derived rather than seeded into excludedProviders so that
	// providersChanged keeps tracking user intent alone (an untouched picker
	// stays untouched) and so a cap that resolves after edit mode opened still
	// applies.
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
			api.virtualKeys.update(vk.id, {
				name,
				rate_limit_rps,
				rate_limit_burst,
				rate_limit_tpm,
				// Both are omitted-means-preserve on the API; keep them off the
				// wire entirely rather than sending an explicit undefined.
				...(allowed_providers !== undefined ? { allowed_providers } : {}),
				strip_reasoning,
				...(owner_user_id !== undefined ? { owner_user_id } : {}),
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
		// An untouched picker OMITS allowed_providers rather than restating it.
		// The API reads an absent field as "preserve the stored value" and, since
		// the request claims nothing about provider access and keeps the key where
		// it is, does not re-check it against the owner's cap. That is the only
		// spelling that can edit a key whose owner's cap has since narrowed below
		// its stored list, and the only one that does not quietly overwrite the
		// operator's stored intent with the narrowed view this picker is showing.
		//
		// A REASSIGNMENT is the exception, and this condition must keep both of
		// its halves in step with the server's (`req.allowedProvidersPresent ||
		// !sameOwner(...)` in internal/api/virtualkeys.go). Handing the key to a
		// different account re-validates the preserved list against the NEW
		// owner's cap, so omitting the field there gets the stored list refused
		// with no way back inside this modal: the out-of-cap chips are inert, and
		// excluding the rest to force a change trips the empty-list guard below.
		// Restating the narrowed list is also right on the merits, since moving a
		// key into a narrower account is a deliberate claim, not an untouched
		// field.
		let allowedProviders: string[] | null | undefined;
		if (providersChanged || ownerChanged) {
			const allProviderIds = sortedProviders.map((p) => p.id);
			allowedProviders =
				effectiveExcluded.length > 0
					? allProviderIds.filter((id) => !effectiveExcluded.includes(id))
					: // User removed all exclusions → send null (no restriction)
						null;
			if (allowedProviders && allowedProviders.length === 0) {
				setProviderError(t("virtualKeys.create.providerRequired"));
				return;
			}
		}
		updateMutation.mutate({
			name: editName.trim(),
			rate_limit_rps: editRps !== "" ? parseFloat(editRps) : null,
			rate_limit_burst: editBurst !== "" ? parseInt(editBurst, 10) : null,
			rate_limit_tpm: editTpm !== "" ? parseInt(editTpm, 10) : null,
			...(allowedProviders !== undefined
				? { allowed_providers: allowedProviders }
				: {}),
			strip_reasoning: editStripReasoning,
			// Non-admins omit the field entirely; the server preserves the
			// current owner (and would force self anyway).
			...(isAdmin
				? { owner_user_id: editOwnerId !== "" ? editOwnerId : null }
				: {}),
		});
	};

	const handleCancelEdit = () => {
		setEditName(vk.name);
		setEditOwnerId(vk.owner_user_id ?? "");
		setEditRps(vk.rate_limit_rps?.toString() ?? "");
		setEditBurst(vk.rate_limit_burst?.toString() ?? "");
		setEditTpm(vk.rate_limit_tpm?.toString() ?? "");
		setExcludedProviders([]);
		setOriginalExcluded([]);
		setEditStripReasoning(vk.strip_reasoning);
		setEditing(false);
	};

	const startEditing = () => {
		setEditName(vk.name);
		setEditOwnerId(vk.owner_user_id ?? "");
		setEditRps(vk.rate_limit_rps?.toString() ?? "");
		setEditBurst(vk.rate_limit_burst?.toString() ?? "");
		setEditTpm(vk.rate_limit_tpm?.toString() ?? "");
		setEditStripReasoning(vk.strip_reasoning);
		setProviderError("");
		// Compute excluded providers from the VK's allowed_providers.
		// If the key has restrictions but providers haven't loaded yet,
		// we must not proceed — that would silently clear restrictions.
		//
		// The test is the LIST'S PRESENCE, not its length. An EMPTY list is a
		// restriction (deny everything), and it is an ordinary state: deleting the
		// last provider a key was scoped to prunes the stored list to `{}`. Length-
		// gating it let a deny-all key fall to the else-branch below, which clears
		// excludedProviders and paints every provider as allowed, so the first chip
		// touched would widen the key.
		if (vk.allowed_providers && !providers) {
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

	// Moving a key to a different account re-opens the provider question even
	// when the picker was not touched, because the cap that binds the write
	// changes with it. handleSave uses this to stay in step with the server.
	const ownerChanged = editOwnerId !== (vk.owner_user_id ?? "");

	const hasChanges =
		editName !== vk.name ||
		ownerChanged ||
		editRps !== (vk.rate_limit_rps?.toString() ?? "") ||
		editBurst !== (vk.rate_limit_burst?.toString() ?? "") ||
		editTpm !== (vk.rate_limit_tpm?.toString() ?? "") ||
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

						{isAdmin && (
							<div>
								<label
									htmlFor="vk-detail-owner"
									className="block text-sm font-medium text-gray-300 mb-1"
								>
									{t("virtualkeys.modal.form.owner")}
								</label>
								<select
									id="vk-detail-owner"
									value={editOwnerId}
									onChange={(e) => setEditOwnerId(e.target.value)}
									className="ui-input"
									data-testid="vk-detail-owner-select"
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
									min="1"
									value={editBurst}
									onChange={(e) => setEditBurst(e.target.value)}
									className="ui-input"
									placeholder={t("virtualkeys.modal.form.placeholderGlobal")}
								/>
							</div>
						</div>
						<div>
							<label
								htmlFor="vk-detail-tpm"
								className="block text-sm font-medium text-gray-300 mb-1"
							>
								{t("virtualkeys.modal.form.rateLimitTpm")}
							</label>
							<input
								id="vk-detail-tpm"
								type="number"
								min="1"
								value={editTpm}
								onChange={(e) => setEditTpm(e.target.value)}
								className="ui-input"
								placeholder={t("virtualkeys.modal.form.placeholderGlobal")}
							/>
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
												// aria-disabled, not disabled: a disabled button drops
												// out of the tab order, which would put both the title
												// and the note out of reach of a keyboard or a screen
												// reader. toggleProvider is what makes it inert.
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
							icon={Fingerprint}
							label={t("virtualkeys.modal.labels.id")}
						>
							<CopyablePill
								text={vk.id}
								textClassName="text-sm font-mono text-(--text-primary)"
								tooltip={t("virtualkeys.tooltip.id")}
							/>
						</DetailItem>
						<DetailItem
							icon={Tag}
							label={t("virtualkeys.modal.form.name")}
							value={vk.name}
						/>
						<DetailItem
							icon={UserRound}
							label={t("virtualkeys.modal.labels.owner")}
							value={vk.owner_username ?? t("virtualkeys.modal.form.ownerNone")}
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
							icon={BrainSlashIcon}
							label={t("virtualkeys.modal.form.stripReasoning")}
							value={
								vk.strip_reasoning ? t("common.enabled") : t("common.disabled")
							}
						/>
						<DetailItem
							emphasis="stat"
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
							emphasis="stat"
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
							emphasis="stat"
							icon={Gauge}
							label={t("virtualKeys.detail.tpm")}
							labelExtra={<InfoHint tooltip={t("virtualkeys.tooltip.tpm")} />}
							value={
								vk.rate_limit_tpm != null
									? String(vk.rate_limit_tpm)
									: t("common.global")
							}
							mono
						/>
						<DetailItem
							emphasis="stat"
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
							icon={ShieldCheck}
							label={t("virtualkeys.modal.sections.providerAccess")}
							className="col-span-2"
						>
							{sortedProviders.length === 0 ? (
								<p className="text-xs text-gray-500 italic">
									{t("virtualkeys.modal.noProvidersConfigured")}
								</p>
							) : (
								<>
									{outsideCapIds.length > 0 && (
										<p
											id={capNoteId}
											data-testid="vk-detail-readonly-cap-note"
											data-cap-source={capIsOtherOwner ? "owner" : "account"}
											className="text-xs text-gray-500 italic mb-1.5"
										>
											{capNote}
										</p>
									)}
									<div className="flex flex-wrap gap-1.5">
										{sortedProviders.map((provider) => {
											const outsideCap = isOutsideCap(provider.id);
											// Being named by the key is not the same as being
											// granted. A stored list wider than the owner's cap is a
											// deliberate, ordinary state here (the key keeps its
											// stored intent and effectiveAllowedProviders in
											// internal/proxy intersects the two on every request),
											// so a provider the cap excludes reads as denied however
											// the stored list spells it. Both reasons strike the
											// chip through; only the cap adds the note, since that
											// is the one the stored list cannot explain.
											const isAllowed =
												!outsideCap &&
												(!vk.allowed_providers ||
													vk.allowed_providers.includes(provider.id));
											return (
												<span
													key={provider.id}
													data-testid={`vk-detail-provider-${provider.id}`}
													{...(outsideCap
														? {
																"data-outside-cap": "true",
																"aria-describedby": capNoteId,
															}
														: {})}
													title={outsideCap ? capNote : undefined}
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
								</>
							)}
						</DetailItem>
					</div>
				)}
			</div>

			{!managed && (
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
								className="ui-btn ui-btn-primary"
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
							// Same presence test as startEditing's guard, deliberately: an
							// empty allowed_providers is still a restriction, and if this
							// button stayed enabled for one the click would hit that guard
							// and silently do nothing.
							disabled={!!vk.allowed_providers && !providers}
							title={
								vk.allowed_providers && !providers
									? t("virtualkeys.modal.loadingProviders")
									: undefined
							}
						>
							{t("common.edit")}
						</button>
					)}
				</div>
			)}

			{managed && (
				// The page-level ManagedBanner sits behind this full-screen modal, so
				// without the footer actions the modal would read as blank. Restate the
				// read-only intent inline (same copy as the synced Settings sections).
				<p data-testid="managed-note" className="text-xs text-(--text-muted)">
					{t("settings.managed.sectionNote")}
				</p>
			)}
		</Modal>
	);
}
