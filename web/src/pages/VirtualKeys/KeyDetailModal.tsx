import { useTranslation } from "react-i18next";
import {
	BrainSlashIcon,
	CalendarPlus,
	Clock,
	Coins,
	Fingerprint,
	Gauge,
	Key,
	ShieldCheck,
	Tag,
	UserRound,
	Zap,
} from "@/lib/icons";
import type { VirtualKey } from "../../api/types";
import { ConfirmDeleteButton } from "../../components/ConfirmDeleteButton";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { CopyablePill } from "../../components/CopyablePill";
import { InfoHint } from "../../components/InfoHint";
import { DetailItem } from "../../components/LogDetailItem";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { formatNumber } from "../../utils/format";
import { ProviderAccessPicker } from "./ProviderAccessPicker";
import { RateLimitField } from "./RateLimitField";
import { SectionHeader } from "./SectionHeader";
import { useKeyEdit } from "./useKeyEdit";

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
	const { t } = useTranslation();
	/** A per-key limit, or "Global" when the key inherits the setting. */
	const limitText = (n: number | null | undefined) =>
		n != null ? String(n) : t("common.global");
	const {
		editing,
		editName,
		setEditName,
		editOwnerId,
		setEditOwnerId,
		editRps,
		setEditRps,
		editBurst,
		setEditBurst,
		editTpm,
		setEditTpm,
		providerError,
		confirmFields,
		setConfirmFields,
		editStripReasoning,
		setEditStripReasoning,
		cap,
		deleteMutation,
		updateMutation,
		handleSave,
		handleCancelEdit,
		startEditing,
		hasChanges,
		handleClose,
		isAdmin,
		providers,
		users,
	} = useKeyEdit({ vk, onClose, onToast });
	// The read-only chips below read the same cap the edit picker enforces.
	const {
		sortedProviders,
		outsideCapIds,
		isOutsideCap,
		capIsOtherOwner,
		capNote,
		capNoteId,
	} = cap;

	return (
		<>
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
								<RateLimitField
									id="vk-detail-rps"
									labelKey="virtualkeys.modal.form.rateLimitRps"
									field="rps"
									value={editRps}
									onChange={setEditRps}
								/>
								<RateLimitField
									id="vk-detail-burst"
									labelKey="virtualkeys.modal.form.rateLimitBurst"
									field="burst"
									value={editBurst}
									onChange={setEditBurst}
								/>
							</div>
							<RateLimitField
								id="vk-detail-tpm"
								labelKey="virtualkeys.modal.form.rateLimitTpm"
								field="tpm"
								value={editTpm}
								onChange={setEditTpm}
							/>
							<ProviderAccessPicker
								cap={cap}
								headingKey="virtualkeys.modal.sections.providerAccess"
								instructionsKey="virtualkeys.modal.form.providerInstructions"
								providerError={providerError}
							/>

							<SectionHeader
								icon={BrainSlashIcon}
								label={t("virtualkeys.modal.form.stripReasoning")}
								spacing="mt-4 first:mt-0 mb-2"
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
								value={
									vk.owner_username ?? t("virtualkeys.modal.form.ownerNone")
								}
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
									vk.strip_reasoning
										? t("common.enabled")
										: t("common.disabled")
								}
							/>
							<DetailItem
								emphasis="stat"
								icon={Gauge}
								label={t("virtualKeys.detail.rps")}
								labelExtra={<InfoHint tooltip={t("virtualkeys.tooltip.rps")} />}
								value={limitText(vk.rate_limit_rps)}
								mono
							/>
							<DetailItem
								emphasis="stat"
								icon={Zap}
								label={t("virtualKeys.detail.burst")}
								labelExtra={
									<InfoHint tooltip={t("virtualkeys.tooltip.burst")} />
								}
								value={limitText(vk.rate_limit_burst)}
								mono
							/>
							<DetailItem
								emphasis="stat"
								icon={Gauge}
								label={t("virtualKeys.detail.tpm")}
								labelExtra={<InfoHint tooltip={t("virtualkeys.tooltip.tpm")} />}
								value={limitText(vk.rate_limit_tpm)}
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
			{confirmFields && (
				<ConfirmDialog
					title={t("delete_confirm.unsaved_changes")}
					fields={confirmFields}
					onConfirm={() => {
						setConfirmFields(null);
						onClose();
					}}
					onCancel={() => setConfirmFields(null)}
				/>
			)}
		</>
	);
}
