import { useId } from "react";
import { useTranslation } from "react-i18next";
import type { DashboardUser, UserRole } from "../../api/types";
import { BudgetField } from "../../components/BudgetField";
import { budgetText } from "../../components/budget";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { DeleteConfirmModal } from "../../components/DeleteConfirmModal";
import { ErrorCallout } from "../../components/ErrorCallout";
import {
	FieldRow,
	FORM_GRID,
	FORM_GRID_PAIRS,
} from "../../components/FieldRow";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { useUserForm } from "./useUserForm";

export function UserModal({
	user,
	managed = false,
	onClose,
	onToast,
}: {
	/** null opens the modal in create mode. */
	user: DashboardUser | null;
	managed?: boolean;
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
	const { t } = useTranslation();
	const {
		username,
		setUsername,
		displayName,
		setDisplayName,
		email,
		setEmail,
		password,
		setPassword,
		role,
		setRole,
		grants,
		enabled,
		setEnabled,
		limitRps,
		setLimitRps,
		limitBurst,
		setLimitBurst,
		limitTpm,
		setLimitTpm,
		budgetUsd,
		setBudgetUsd,
		budgetPeriod,
		setBudgetPeriod,
		providerMode,
		selectedProviders,
		providerError,
		error,
		setError,
		confirmDelete,
		setConfirmDelete,
		confirmTotpReset,
		setConfirmTotpReset,
		resetValue,
		setResetValue,
		isEdit,
		isSelf,
		allGrants,
		sortedProviders,
		saveMutation,
		deleteMutation,
		resetMutation,
		totpResetMutation,
		handleSave,
		chooseProviderMode,
		toggleProvider,
		toggleGrant,
	} = useUserForm({ user, onClose, onToast });
	const formId = useId();

	return (
		<Modal
			title={isEdit ? t("users.modal.editTitle") : t("users.modal.addTitle")}
			onClose={onClose}
			maxWidth="max-w-2xl"
			footer={
				<div className="flex gap-3 justify-end">
					<button type="button" onClick={onClose} className="ui-btn">
						{t("users.modal.cancel")}
					</button>
					{!managed && (
						<button
							type="submit"
							form={formId}
							disabled={saveMutation.isPending}
							className="ui-btn ui-btn-primary"
							data-testid="user-modal-save"
						>
							{isEdit ? t("users.modal.save") : t("users.modal.create")}
						</button>
					)}
				</div>
			}
		>
			{/* A form so Enter saves and the browser checks the email and the
			   limit bounds; the reset-password section stays outside it, so
			   Enter there does not save the whole user. */}
			<form
				id={formId}
				onSubmit={(e) => {
					e.preventDefault();
					handleSave();
				}}
				className={FORM_GRID_PAIRS}
			>
				{error && (
					<ErrorCallout className="sm:col-span-4" testId="user-modal-error">
						{error}
					</ErrorCallout>
				)}

				<FieldRow label={t("users.modal.username")} htmlFor="user-username">
					<input
						id="user-username"
						type="text"
						value={username}
						onChange={(e) => setUsername(e.target.value)}
						className="ui-input"
						maxLength={64}
						autoComplete="off"
						disabled={managed}
					/>
				</FieldRow>

				<FieldRow
					label={t("users.modal.displayName")}
					htmlFor="user-display-name"
				>
					<input
						id="user-display-name"
						type="text"
						value={displayName}
						onChange={(e) => setDisplayName(e.target.value)}
						className="ui-input"
						maxLength={128}
						disabled={managed}
					/>
				</FieldRow>

				<FieldRow
					label={t("users.modal.email")}
					htmlFor="user-email"
					fieldClassName="sm:col-span-3"
				>
					<input
						id="user-email"
						type="email"
						value={email}
						onChange={(e) => setEmail(e.target.value)}
						className="ui-input"
						autoComplete="off"
						disabled={managed}
					/>
					<p className="ui-hint mt-1">{t("users.modal.emailHint")}</p>
				</FieldRow>

				{!isEdit && (
					<FieldRow label={t("users.modal.password")} htmlFor="user-password">
						<input
							id="user-password"
							type="password"
							value={password}
							onChange={(e) => setPassword(e.target.value)}
							className="ui-input"
							autoComplete="new-password"
							placeholder={t("users.modal.passwordPlaceholder")}
							disabled={managed}
						/>
					</FieldRow>
				)}

				<FieldRow
					label={t("users.modal.role")}
					htmlFor="user-role"
					fieldClassName={isEdit ? "sm:col-span-3" : undefined}
				>
					<select
						id="user-role"
						value={role}
						onChange={(e) => setRole(e.target.value as UserRole)}
						className="ui-input"
						disabled={isSelf || managed}
					>
						<option value="user">{t("users.role.user")}</option>
						<option value="admin">{t("users.role.admin")}</option>
					</select>
					<p className="ui-hint mt-1">
						{role === "admin"
							? t("users.modal.roleAdminHint")
							: t("users.modal.roleUserHint")}
					</p>
				</FieldRow>

				{role === "user" && (
					<fieldset className="sm:col-span-4">
						<legend className="block ui-label mb-2">
							{t("users.modal.grants")}
						</legend>
						<div className="grid grid-cols-3 gap-2">
							{allGrants.map((g) => (
								<label
									key={g}
									className="flex items-center gap-2 text-sm text-gray-200 cursor-pointer"
								>
									<input
										type="checkbox"
										checked={grants.includes(g)}
										onChange={() => toggleGrant(g)}
										className="ui-checkbox"
										data-testid={`grant-${g}`}
										disabled={managed}
									/>
									{t(`users.grant.${g}`, { defaultValue: g })}
								</label>
							))}
						</div>
					</fieldset>
				)}

				<fieldset className="sm:col-span-4">
					<legend className="block ui-label mb-2">
						{t("users.providerAccess")}
					</legend>
					<div className="space-y-2" data-testid="provider-access-mode">
						<label className="flex items-center gap-2 text-sm text-gray-200 cursor-pointer">
							<input
								type="radio"
								name="user-provider-access"
								value="all"
								checked={providerMode === "all"}
								onChange={() => chooseProviderMode("all")}
								disabled={managed}
							/>
							{t("users.providerAccessAll")}
						</label>
						<label className="flex items-center gap-2 text-sm text-gray-200 cursor-pointer">
							<input
								type="radio"
								name="user-provider-access"
								value="selected"
								checked={providerMode === "selected"}
								onChange={() => chooseProviderMode("selected")}
								disabled={managed}
							/>
							{t("users.providerAccessSelected")}
						</label>
					</div>
					{providerMode === "selected" && (
						<div
							className="grid grid-cols-2 gap-2 mt-2 max-h-40 overflow-y-auto"
							data-testid="provider-access-list"
						>
							{sortedProviders.map((p) => (
								<label
									key={p.id}
									className="flex items-center gap-2 text-sm text-gray-200 cursor-pointer"
								>
									<input
										type="checkbox"
										checked={selectedProviders.includes(p.id)}
										onChange={() => toggleProvider(p.id)}
										className="ui-checkbox"
										data-testid={`provider-access-option-${p.id}`}
										disabled={managed}
									/>
									{p.name}
								</label>
							))}
						</div>
					)}
					{providerError && (
						<p
							className="text-xs text-red-400 mt-2"
							data-testid="provider-access-error"
							data-error-kind={providerError}
						>
							{providerError === "required"
								? t("users.providerAccessRequired")
								: t("users.providerAccessEmpty")}
						</p>
					)}
				</fieldset>

				<fieldset className="sm:col-span-4">
					<legend className="block ui-label mb-1">
						{t("users.modal.limits")}
					</legend>
					<p className="ui-hint mb-2">{t("users.modal.limitsHint")}</p>
					<div className="grid grid-cols-3 gap-2">
						<div>
							<label
								htmlFor="user-limit-rps"
								className="block text-xs text-gray-400 mb-1"
							>
								{t("users.modal.limitRps")}
							</label>
							<input
								id="user-limit-rps"
								type="number"
								min="0"
								max="10000"
								step="any"
								value={limitRps}
								onChange={(e) => setLimitRps(e.target.value)}
								className="ui-input"
								placeholder={t("users.modal.noCap")}
								disabled={managed}
								data-testid="user-limit-rps"
							/>
						</div>
						<div>
							<label
								htmlFor="user-limit-burst"
								className="block text-xs text-gray-400 mb-1"
							>
								{t("users.modal.limitBurst")}
							</label>
							<input
								id="user-limit-burst"
								type="number"
								min="1"
								max="10000"
								value={limitBurst}
								onChange={(e) => setLimitBurst(e.target.value)}
								className="ui-input"
								placeholder={t("users.modal.noCap")}
								disabled={managed}
								data-testid="user-limit-burst"
							/>
						</div>
						<div>
							<label
								htmlFor="user-limit-tpm"
								className="block text-xs text-gray-400 mb-1"
							>
								{t("users.modal.limitTpm")}
							</label>
							<input
								id="user-limit-tpm"
								type="number"
								min="1"
								max="100000000"
								value={limitTpm}
								onChange={(e) => setLimitTpm(e.target.value)}
								className="ui-input"
								placeholder={t("users.modal.noCap")}
								disabled={managed}
								data-testid="user-limit-tpm"
							/>
						</div>
					</div>
					<div className="mt-3">
						<BudgetField
							idPrefix="user"
							amount={budgetUsd}
							period={budgetPeriod}
							onAmountChange={setBudgetUsd}
							onPeriodChange={setBudgetPeriod}
							disabled={managed}
							labelClassName="block text-xs text-gray-400 mb-1"
						/>
						{user?.budget_usd != null && (
							<p
								className="text-xs text-gray-400 mt-1"
								data-testid="user-budget-spent"
							>
								{budgetText(t, user)}
							</p>
						)}
					</div>
				</fieldset>

				{isEdit && (
					<div className="sm:col-span-4 flex items-center justify-between">
						<span className="ui-label">{t("users.modal.enabled")}</span>
						<Toggle
							checked={enabled}
							onChange={setEnabled}
							disabled={isSelf || managed}
							ariaLabel={t("users.modal.enabled")}
						/>
					</div>
				)}

				{managed && isEdit && (
					<p data-testid="managed-note" className="sm:col-span-4 ui-hint">
						{t("settings.managed.sectionNote")}
					</p>
				)}
			</form>

			{isEdit && !managed && (
				<div className={`mt-4 border-t border-gray-700 pt-4 ${FORM_GRID}`}>
					<FieldRow
						label={t("users.modal.resetPassword")}
						htmlFor="user-reset-password"
					>
						<div className="flex gap-2">
							<input
								id="user-reset-password"
								type="password"
								value={resetValue}
								onChange={(e) => setResetValue(e.target.value)}
								className="ui-input flex-1"
								autoComplete="new-password"
								placeholder={t("users.modal.passwordPlaceholder")}
							/>
							<button
								type="button"
								onClick={() => {
									setError(null);
									if (resetValue.length < 8) {
										setError(t("users.validation.passwordShort"));
										return;
									}
									resetMutation.mutate();
								}}
								disabled={resetMutation.isPending}
								className="ui-btn"
							>
								{t("users.modal.resetButton")}
							</button>
						</div>
						<p className="ui-hint mt-1">{t("users.modal.resetHint")}</p>
					</FieldRow>

					{user?.totp_enabled && (
						<div className="sm:col-span-2">
							<button
								type="button"
								onClick={() => setConfirmTotpReset(true)}
								disabled={totpResetMutation.isPending}
								className="ui-btn w-full"
								data-testid="user-modal-totp-reset"
							>
								{t("users.modal.totpResetButton")}
							</button>
							<p className="ui-hint mt-1">{t("users.modal.totpResetHint")}</p>
						</div>
					)}

					{!isSelf && (
						<button
							type="button"
							onClick={() => setConfirmDelete(true)}
							className="sm:col-span-2 ui-btn ui-btn-danger w-full"
							data-testid="user-modal-delete"
						>
							{t("users.modal.deleteButton")}
						</button>
					)}
				</div>
			)}

			{confirmDelete && user && (
				<DeleteConfirmModal
					title={t("users.modal.deleteConfirmTitle")}
					message={`${t("users.modal.deleteConfirmMessage")} ${t("users.modal.deleteKeysNote")}`}
					confirmText={user.username}
					isPending={deleteMutation.isPending}
					onConfirm={() => deleteMutation.mutate()}
					onCancel={() => setConfirmDelete(false)}
					confirmTestId="user-delete-confirm"
				/>
			)}
			{confirmTotpReset && (
				<ConfirmDialog
					title={t("users.modal.totpResetConfirmTitle")}
					message={t("users.modal.totpResetConfirmMessage")}
					fields={[user?.username ?? ""]}
					confirmLabel={t("users.modal.totpResetConfirmLabel")}
					onConfirm={() => totpResetMutation.mutate()}
					onCancel={() => setConfirmTotpReset(false)}
					confirmTestId="user-totp-reset-confirm"
				/>
			)}
		</Modal>
	);
}
