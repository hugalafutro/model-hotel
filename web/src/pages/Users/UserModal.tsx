import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { DollarSign, Gauge, Users as UsersIcon, Zap } from "@/lib/icons";
import type { DashboardUser, UserRole } from "../../api/types";
import { BudgetField } from "../../components/BudgetField";
import { budgetText } from "../../components/budget";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { DeleteConfirmModal } from "../../components/DeleteConfirmModal";
import { ErrorCallout } from "../../components/ErrorCallout";
import { InfoHint } from "../../components/InfoHint";
import { DetailItem } from "../../components/LogDetailItem";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { UserDetailCard } from "./UserDetailCard";
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
	// An existing user opens as a read-only card; Edit turns it into the form.
	// A new user has nothing to show yet and starts in the form.
	const [editing, setEditing] = useState(!isEdit);
	const submitReset = () => {
		setError(null);
		if (resetValue.length < 8) {
			setError(t("users.validation.passwordShort"));
			return;
		}
		resetMutation.mutate();
	};

	return (
		<Modal
			title={
				!isEdit
					? t("users.modal.addTitle")
					: editing
						? t("users.modal.editTitle")
						: (user?.username ?? "")
			}
			icon={UsersIcon}
			onClose={onClose}
			maxWidth="max-w-2xl"
			scrollable
			footer={
				<div className="flex gap-3 justify-between items-center">
					{isEdit && !managed && !isSelf ? (
						<button
							type="button"
							onClick={() => setConfirmDelete(true)}
							className="ui-btn ui-btn-danger"
							data-testid="user-modal-delete"
						>
							{t("users.modal.deleteButton")}
						</button>
					) : (
						<span />
					)}
					<div className="flex gap-3">
						{editing && (
							<button
								type="button"
								onClick={onClose}
								className="ui-btn ui-btn-secondary"
							>
								{t("users.modal.cancel")}
							</button>
						)}
						{/* Keyed apart: in one slot React would keep the Edit button's
						   node and only flip it to a submit button while the click
						   that pressed Edit is still being dispatched, and the
						   browser would then submit the form it just became part of. */}
						{!managed &&
							(editing ? (
								<button
									key="save"
									type="submit"
									form={formId}
									disabled={saveMutation.isPending}
									className="ui-btn ui-btn-primary"
									data-testid="user-modal-save"
								>
									{isEdit ? t("users.modal.save") : t("users.modal.create")}
								</button>
							) : (
								<button
									key="edit"
									type="button"
									onClick={() => setEditing(true)}
									className="ui-btn ui-btn-primary"
									data-testid="user-modal-edit"
								>
									{t("common.edit")}
								</button>
							))}
					</div>
				</div>
			}
		>
			{error && (
				<ErrorCallout className="mb-2" testId="user-modal-error">
					{error}
				</ErrorCallout>
			)}

			{user && !editing && (
				<UserDetailCard
					user={user}
					managed={managed}
					providers={sortedProviders}
				/>
			)}

			{/* A form so Enter saves and the browser checks the email and the
			   limit bounds. Enter in the reset-password field resets the
			   password instead of saving the whole user. */}
			{editing && (
				<form
					id={formId}
					onSubmit={(e) => {
						e.preventDefault();
						handleSave();
					}}
					className="grid grid-cols-1 sm:grid-cols-2 gap-2"
				>
					<DetailItem label={t("users.modal.username")} htmlFor="user-username">
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
					</DetailItem>

					<DetailItem
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
					</DetailItem>

					<DetailItem
						label={t("users.modal.email")}
						htmlFor="user-email"
						className="sm:col-span-2"
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
					</DetailItem>

					{!isEdit && (
						<DetailItem
							label={t("users.modal.password")}
							htmlFor="user-password"
						>
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
						</DetailItem>
					)}

					<DetailItem label={t("users.modal.role")} htmlFor="user-role">
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
					</DetailItem>

					{isEdit && (
						<DetailItem label={t("users.modal.enabled")}>
							<div className="flex items-center gap-3">
								<Toggle
									checked={enabled}
									onChange={setEnabled}
									disabled={isSelf || managed}
									ariaLabel={t("users.modal.enabled")}
								/>
								<span className="text-sm text-(--text-primary)">
									{enabled ? t("common.enabled") : t("common.disabled")}
								</span>
							</div>
						</DetailItem>
					)}

					{isEdit && !managed && (
						<DetailItem
							label={t("users.modal.resetPassword")}
							htmlFor="user-reset-password"
							className="sm:col-span-2"
						>
							<div className="flex flex-col gap-2 sm:flex-row">
								<input
									id="user-reset-password"
									type="password"
									value={resetValue}
									onChange={(e) => setResetValue(e.target.value)}
									onKeyDown={(e) => {
										if (e.key === "Enter") {
											e.preventDefault();
											submitReset();
										}
									}}
									className="ui-input flex-1 min-w-0"
									autoComplete="new-password"
									placeholder={t("users.modal.passwordPlaceholder")}
								/>
								<button
									type="button"
									onClick={submitReset}
									disabled={resetMutation.isPending}
									className="ui-btn"
								>
									{t("users.modal.resetButton")}
								</button>
							</div>
							<p className="ui-hint mt-1">{t("users.modal.resetHint")}</p>
							{user?.totp_enabled && (
								<div className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-start">
									<button
										type="button"
										onClick={() => setConfirmTotpReset(true)}
										disabled={totpResetMutation.isPending}
										className="ui-btn shrink-0"
										data-testid="user-modal-totp-reset"
									>
										{t("users.modal.totpResetButton")}
									</button>
									<p className="ui-hint sm:pt-2">
										{t("users.modal.totpResetHint")}
									</p>
								</div>
							)}
						</DetailItem>
					)}

					{role === "user" && (
						<DetailItem
							label={t("users.modal.grants")}
							className="sm:col-span-2"
						>
							<div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
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
						</DetailItem>
					)}

					<DetailItem
						label={t("users.providerAccess")}
						className="sm:col-span-2"
					>
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
					</DetailItem>

					{/* Aggregate caps across the user's keys, one stat tile each. */}
					<div className="sm:col-span-2 grid grid-cols-1 sm:grid-cols-3 gap-2">
						{(
							[
								{
									field: "rps",
									icon: Gauge,
									label: "users.modal.limitRps",
									min: "0",
									max: "10000",
									step: "any",
									draft: limitRps,
									setDraft: setLimitRps,
								},
								{
									field: "burst",
									icon: Zap,
									label: "users.modal.limitBurst",
									min: "1",
									max: "10000",
									step: undefined,
									draft: limitBurst,
									setDraft: setLimitBurst,
								},
								{
									field: "tpm",
									icon: Gauge,
									label: "users.modal.limitTpm",
									min: "1",
									max: "100000000",
									step: undefined,
									draft: limitTpm,
									setDraft: setLimitTpm,
								},
							] as const
						).map((l) => (
							<DetailItem
								key={l.field}
								emphasis="stat"
								icon={l.icon}
								label={t(l.label)}
								labelExtra={<InfoHint tooltip={t("users.modal.limitsHint")} />}
								htmlFor={`user-limit-${l.field}`}
							>
								<input
									id={`user-limit-${l.field}`}
									type="number"
									min={l.min}
									max={l.max}
									step={l.step}
									value={l.draft}
									onChange={(e) => l.setDraft(e.target.value)}
									className="ui-input"
									placeholder={t("users.modal.noCap")}
									disabled={managed}
									data-testid={`user-limit-${l.field}`}
								/>
							</DetailItem>
						))}
					</div>

					<DetailItem
						emphasis="stat"
						icon={DollarSign}
						label={t("budget.label")}
						labelExtra={<InfoHint tooltip={t("budget.tooltip")} />}
						className="sm:col-span-2"
					>
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
					</DetailItem>
				</form>
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
