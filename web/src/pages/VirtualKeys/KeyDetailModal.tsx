import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	BrainSlashIcon,
	CalendarPlus,
	Clock,
	DollarSign,
	Fingerprint,
	Gauge,
	Key,
	KeyRound,
	RotateCcw,
	ShieldCheck,
	UserRound,
	Zap,
} from "@/lib/icons";
import type { VirtualKey } from "../../api/types";
import {
	BUDGET_AMOUNT_BOUNDS,
	BUDGET_PERIODS,
	budgetText,
} from "../../components/budget";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { CopyablePill } from "../../components/CopyablePill";
import { DeleteConfirmModal } from "../../components/DeleteConfirmModal";
import { InfoHint } from "../../components/InfoHint";
import { DetailItem } from "../../components/LogDetailItem";
import { Modal, ModalIcon } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { formatDateTime, formatNumber, formatTokens } from "../../utils/format";
import { ProviderAccessPicker } from "./ProviderAccessPicker";
import { RATE_LIMIT_BOUNDS } from "./rateLimitBounds";
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
	const [confirmDelete, setConfirmDelete] = useState(false);
	const nameId = useId();
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
		editBudgetUsd,
		setEditBudgetUsd,
		editBudgetPeriod,
		setEditBudgetPeriod,
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
		requestClose,
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
		excludedProviders,
		resetProviders,
	} = cap;
	// Only the providers the key can reach are listed: the ones its stored list
	// names, or with no stored list the ones the owner's cap allows. With 200
	// providers and a key scoped to three, the other 197 are noise.
	const listedProviders = sortedProviders.filter((p) =>
		vk.allowed_providers
			? vk.allowed_providers.includes(p.id)
			: !isOutsideCap(p.id),
	);
	// The cap note explains a struck chip, or an unrestricted key the cap left
	// nothing. Above accent chips alone it would read as if those were the
	// blocked ones, and a stored empty list is the key's own doing.
	const showCapNote = vk.allowed_providers
		? listedProviders.some((p) => isOutsideCap(p.id))
		: listedProviders.length === 0 && outsideCapIds.length > 0;

	return (
		<>
			<Modal
				header={
					<div className="flex flex-wrap items-center gap-3 mb-4">
						<ModalIcon icon={KeyRound} />
						{editing ? (
							// The title becomes the name field: the dialog keeps its
							// shape and its name, only the values turn editable.
							<>
								<h2 id={nameId} className="sr-only">
									{vk.name}
								</h2>
								<label
									htmlFor="vk-detail-name"
									className="ui-label after:content-[':']"
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
									className="ui-input flex-1 min-w-0 text-lg font-semibold"
								/>
							</>
						) : (
							<h2
								id={nameId}
								className="ui-modal-title min-w-0 truncate"
								title={vk.name}
							>
								{vk.name}
							</h2>
						)}
						<span
							className="px-2 py-px leading-[1.6] text-xs font-medium whitespace-nowrap ui-badge ui-badge-purple"
							title={formatNumber(vk.tokens_used)}
						>
							<span className="badge-text">
								{t("providers.card_tokens", {
									tokens: formatTokens(vk.tokens_used),
								})}
							</span>
						</span>
					</div>
				}
				labelledBy={nameId}
				onCloseRequest={requestClose}
				onClose={onClose}
				maxWidth="max-w-2xl"
				scrollable
				footer={
					!managed ? (
						<div className="flex justify-between items-center">
							<button
								type="button"
								onClick={() => setConfirmDelete(true)}
								className="ui-btn ui-btn-danger"
							>
								{t("virtualKeys.detail.deleteKey")}
							</button>
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
					) : undefined
				}
			>
				{/* One tile grid for viewing and editing: Edit turns the values it
				   can change into their controls in place, so the dialog reads as
				   the same card made editable rather than a different form. */}
				<div className="grid grid-cols-1 sm:grid-cols-2 gap-2 mb-6">
					<DetailItem
						icon={UserRound}
						label={t("virtualkeys.modal.labels.owner")}
						labelExtra={
							editing && isAdmin ? (
								<InfoHint tooltip={t("virtualkeys.modal.form.ownerHint")} />
							) : undefined
						}
						value={vk.owner_username ?? t("virtualkeys.modal.form.ownerNone")}
					>
						{editing && isAdmin ? (
							<select
								id="vk-detail-owner"
								value={editOwnerId}
								onChange={(e) => setEditOwnerId(e.target.value)}
								className="ui-input"
								aria-label={t("virtualkeys.modal.form.owner")}
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
						) : undefined}
					</DetailItem>
					{/* ID, created, last used and the key are not editable: while
					   editing they would be the only tiles without a control. */}
					{!editing && (
						<>
							<DetailItem
								icon={Fingerprint}
								label={t("virtualkeys.modal.labels.id")}
								labelExtra={
									<InfoHint tooltip={t("virtualkeys.tooltip.idMeaning")} />
								}
							>
								<CopyablePill
									text={vk.id}
									textClassName="text-sm font-mono text-(--text-primary)"
									tooltip={t("virtualkeys.tooltip.id")}
								/>
							</DetailItem>
							<DetailItem
								icon={CalendarPlus}
								label={t("virtualkeys.modal.labels.created")}
								value={formatDateTime(vk.created_at)}
							/>
							<DetailItem
								icon={Clock}
								label={t("virtualkeys.modal.labels.lastUsed")}
								value={
									vk.last_used_at
										? formatDateTime(vk.last_used_at)
										: t("common.never")
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
						</>
					)}
					<DetailItem
						icon={BrainSlashIcon}
						label={t("virtualkeys.modal.form.stripReasoning")}
						labelExtra={
							// While editing the description sits under the toggle instead.
							editing ? undefined : (
								<InfoHint
									tooltip={t(
										"virtualkeys.modal.form.stripReasoningDescription",
									)}
								/>
							)
						}
						value={
							vk.strip_reasoning ? t("common.enabled") : t("common.disabled")
						}
					>
						{editing ? (
							<>
								<div className="flex items-center gap-3">
									<Toggle
										checked={editStripReasoning}
										onChange={setEditStripReasoning}
										size="sm"
										ariaLabel={t("virtualkeys.modal.form.stripReasoning")}
									/>
									<span className="text-sm text-(--text-primary)">
										{editStripReasoning
											? t("common.enabled")
											: t("common.disabled")}
									</span>
								</div>
								<p className="ui-hint mt-1">
									{t("virtualkeys.modal.form.stripReasoningDescription")}
								</p>
							</>
						) : undefined}
					</DetailItem>
					<DetailItem
						emphasis="stat"
						icon={DollarSign}
						label={t("budget.label")}
						labelExtra={<InfoHint tooltip={t("budget.tooltip")} />}
						value={budgetText(t, vk)}
						mono
						className="sm:col-span-2"
					>
						{editing ? (
							<div className="flex gap-2">
								<input
									id="vk-detail-budget"
									type="number"
									{...BUDGET_AMOUNT_BOUNDS}
									value={editBudgetUsd}
									onChange={(e) => setEditBudgetUsd(e.target.value)}
									className="ui-input flex-1 min-w-0"
									placeholder={t("budget.none")}
									aria-label={t("budget.amount")}
									data-testid="vk-detail-budget"
								/>
								<select
									id="vk-detail-budget-period"
									value={editBudgetPeriod}
									onChange={(e) =>
										setEditBudgetPeriod(
											e.target.value as typeof editBudgetPeriod,
										)
									}
									className="ui-input flex-1 min-w-0"
									disabled={editBudgetUsd === ""}
									aria-label={t("budget.period")}
									data-testid="vk-detail-budget-period"
								>
									{BUDGET_PERIODS.map((p) => (
										<option key={p} value={p}>
											{t(`budget.periods.${p}`)}
										</option>
									))}
								</select>
							</div>
						) : undefined}
					</DetailItem>
					<div className="sm:col-span-2 grid grid-cols-1 sm:grid-cols-3 gap-2">
						{(
							[
								{
									field: "burst",
									icon: Zap,
									label: "virtualKeys.detail.burst",
									tooltip: "virtualkeys.tooltip.burst",
									inputLabel: "virtualkeys.modal.form.rateLimitBurst",
									stored: vk.rate_limit_burst,
									draft: editBurst,
									setDraft: setEditBurst,
								},
								{
									field: "tpm",
									icon: Gauge,
									label: "virtualKeys.detail.tpm",
									tooltip: "virtualkeys.tooltip.tpm",
									inputLabel: "virtualkeys.modal.form.rateLimitTpm",
									stored: vk.rate_limit_tpm,
									draft: editTpm,
									setDraft: setEditTpm,
								},
								{
									field: "rps",
									icon: Gauge,
									label: "virtualKeys.detail.rps",
									tooltip: "virtualkeys.tooltip.rps",
									inputLabel: "virtualkeys.modal.form.rateLimitRps",
									stored: vk.rate_limit_rps,
									draft: editRps,
									setDraft: setEditRps,
								},
							] as const
						).map((l) => (
							<DetailItem
								key={l.field}
								emphasis="stat"
								icon={l.icon}
								label={t(l.label)}
								labelExtra={<InfoHint tooltip={t(l.tooltip)} />}
								value={limitText(l.stored)}
								mono
							>
								{editing ? (
									<input
										id={`vk-detail-${l.field}`}
										type="number"
										{...RATE_LIMIT_BOUNDS[l.field]}
										value={l.draft}
										onChange={(e) => l.setDraft(e.target.value)}
										className="ui-input"
										placeholder={t("virtualkeys.modal.form.placeholderGlobal")}
										aria-label={t(l.inputLabel)}
									/>
								) : undefined}
							</DetailItem>
						))}
					</div>
					<DetailItem
						icon={ShieldCheck}
						label={t("virtualkeys.modal.sections.providerAccess")}
						labelExtra={
							editing && excludedProviders.length > 0 ? (
								<button
									type="button"
									onClick={resetProviders}
									className="text-gray-500 hover:text-gray-300 transition-colors"
									aria-label={t("virtualkeys.modal.form.restoreAccess")}
									title={t("virtualkeys.modal.form.restoreAccess")}
								>
									<RotateCcw size={14} />
								</button>
							) : undefined
						}
						className="sm:col-span-2"
					>
						{editing ? (
							<ProviderAccessPicker
								cap={cap}
								instructionsKey="virtualkeys.modal.form.providerInstructions"
								providerError={providerError}
							/>
						) : sortedProviders.length === 0 ? (
							<p className="text-xs text-gray-500 italic">
								{t("virtualkeys.modal.noProvidersConfigured")}
							</p>
						) : !vk.allowed_providers && outsideCapIds.length === 0 ? (
							<span className="inline-flex items-center px-2 py-px leading-[1.6] text-xs font-medium ui-badge ui-badge-neutral">
								{t("users.providerAccessAll")}
							</span>
						) : (
							<>
								{showCapNote && (
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
									{listedProviders.length === 0 && (
										<span className="inline-flex items-center px-2 py-px leading-[1.6] text-xs font-medium ui-badge ui-badge-neutral">
											{t("users.providerAccessNone")}
										</span>
									)}
									{listedProviders.map((provider) => {
										// Being named by the key is not the same as being
										// granted. A stored list wider than the owner's cap is a
										// deliberate, ordinary state here (the key keeps its
										// stored intent and effectiveAllowedProviders in
										// internal/proxy intersects the two on every request),
										// so a provider the cap excludes reads as denied however
										// the stored list spells it: struck through, with the
										// note saying why.
										const outsideCap = isOutsideCap(provider.id);
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
													outsideCap
														? "ui-badge-neutral line-through opacity-60"
														: "ui-badge-accent"
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

				{managed && (
					// The page-level ManagedBanner sits behind this full-screen modal, so
					// without the footer actions the modal would read as blank. Restate the
					// read-only intent inline (same copy as the synced Settings sections).
					<p data-testid="managed-note" className="ui-hint">
						{t("settings.managed.sectionNote")}
					</p>
				)}
			</Modal>
			{confirmFields && (
				<ConfirmDialog
					title={t("delete_confirm.unsaved_changes")}
					fields={confirmFields}
					confirmLabel={t("common.discard")}
					onConfirm={() => {
						setConfirmFields(null);
						onClose();
					}}
					onCancel={() => setConfirmFields(null)}
				/>
			)}
			{confirmDelete && (
				<DeleteConfirmModal
					title={t("virtualKeys.detail.deleteKey")}
					confirmText={vk.name}
					entityName={vk.name}
					isPending={deleteMutation.isPending}
					onConfirm={() => deleteMutation.mutate()}
					onCancel={() => setConfirmDelete(false)}
				/>
			)}
		</>
	);
}
