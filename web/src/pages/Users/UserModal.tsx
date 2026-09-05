import type { DashboardUser } from "../../api/types";
import { ConfirmDialog } from "../../components/ConfirmDialog";
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
		t,
	} = useUserForm({ user, onClose, onToast });

	return (
		<Modal
			title={isEdit ? t("users.modal.editTitle") : t("users.modal.addTitle")}
			onClose={onClose}
			maxWidth="max-w-lg"
		>
			<div className="space-y-4">
				{error && (
					<div
						className="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm"
						data-testid="user-modal-error"
					>
						{error}
					</div>
				)}

				<div>
					<label
						htmlFor="user-username"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						{t("users.modal.username")}
					</label>
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
				</div>

				<div>
					<label
						htmlFor="user-display-name"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						{t("users.modal.displayName")}
					</label>
					<input
						id="user-display-name"
						type="text"
						value={displayName}
						onChange={(e) => setDisplayName(e.target.value)}
						className="ui-input"
						maxLength={128}
						disabled={managed}
					/>
				</div>

				<div>
					<label
						htmlFor="user-email"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						{t("users.modal.email")}
					</label>
					<input
						id="user-email"
						type="email"
						value={email}
						onChange={(e) => setEmail(e.target.value)}
						className="ui-input"
						autoComplete="off"
						disabled={managed}
					/>
					<p className="text-xs text-gray-500 mt-1">
						{t("users.modal.emailHint")}
					</p>
				</div>

				{!isEdit && (
					<div>
						<label
							htmlFor="user-password"
							className="block text-sm font-medium text-gray-300 mb-2"
						>
							{t("users.modal.password")}
						</label>
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
					</div>
				)}

				<div>
					<label
						htmlFor="user-role"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						{t("users.modal.role")}
					</label>
					<select
						id="user-role"
						value={role}
						onChange={(e) => setRole(e.target.value as "admin" | "user")}
						className="ui-input"
						disabled={isSelf || managed}
					>
						<option value="user">{t("users.role.user")}</option>
						<option value="admin">{t("users.role.admin")}</option>
					</select>
					<p className="text-xs text-gray-500 mt-1">
						{role === "admin"
							? t("users.modal.roleAdminHint")
							: t("users.modal.roleUserHint")}
					</p>
				</div>

				{role === "user" && (
					<fieldset>
						<legend className="block text-sm font-medium text-gray-300 mb-2">
							{t("users.modal.grants")}
						</legend>
						<div className="grid grid-cols-2 gap-2">
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

				<fieldset>
					<legend className="block text-sm font-medium text-gray-300 mb-2">
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

				<fieldset>
					<legend className="block text-sm font-medium text-gray-300 mb-1">
						{t("users.modal.limits")}
					</legend>
					<p className="text-xs text-gray-500 mb-2">
						{t("users.modal.limitsHint")}
					</p>
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
								value={limitTpm}
								onChange={(e) => setLimitTpm(e.target.value)}
								className="ui-input"
								placeholder={t("users.modal.noCap")}
								disabled={managed}
								data-testid="user-limit-tpm"
							/>
						</div>
					</div>
				</fieldset>

				{isEdit && (
					<div className="flex items-center justify-between">
						<span className="text-sm font-medium text-gray-300">
							{t("users.modal.enabled")}
						</span>
						<Toggle
							checked={enabled}
							onChange={setEnabled}
							disabled={isSelf || managed}
							ariaLabel={t("users.modal.enabled")}
						/>
					</div>
				)}

				<div className="flex gap-3 pt-2">
					{!managed && (
						<button
							type="button"
							onClick={handleSave}
							disabled={saveMutation.isPending}
							className="ui-btn ui-btn-primary flex-1"
							data-testid="user-modal-save"
						>
							{isEdit ? t("users.modal.save") : t("users.modal.create")}
						</button>
					)}
					<button type="button" onClick={onClose} className="ui-btn flex-1">
						{t("users.modal.cancel")}
					</button>
				</div>

				{managed && isEdit && (
					<p data-testid="managed-note" className="text-xs text-(--text-muted)">
						{t("settings.managed.sectionNote")}
					</p>
				)}

				{isEdit && !managed && (
					<div className="border-t border-gray-700 pt-4 space-y-4">
						<div>
							<label
								htmlFor="user-reset-password"
								className="block text-sm font-medium text-gray-300 mb-2"
							>
								{t("users.modal.resetPassword")}
							</label>
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
							<p className="text-xs text-gray-500 mt-1">
								{t("users.modal.resetHint")}
							</p>
						</div>

						{user?.totp_enabled && (
							<div>
								<button
									type="button"
									onClick={() => setConfirmTotpReset(true)}
									disabled={totpResetMutation.isPending}
									className="ui-btn w-full"
									data-testid="user-modal-totp-reset"
								>
									{t("users.modal.totpResetButton")}
								</button>
								<p className="text-xs text-gray-500 mt-1">
									{t("users.modal.totpResetHint")}
								</p>
							</div>
						)}

						{!isSelf && (
							<button
								type="button"
								onClick={() => setConfirmDelete(true)}
								className="ui-btn ui-btn-danger w-full"
								data-testid="user-modal-delete"
							>
								{t("users.modal.deleteButton")}
							</button>
						)}
					</div>
				)}
			</div>

			{confirmDelete && (
				<ConfirmDialog
					title={t("users.modal.deleteConfirmTitle")}
					message={`${t("users.modal.deleteConfirmMessage")} ${t("users.modal.deleteKeysNote")}`}
					fields={[user?.username ?? ""]}
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
					onConfirm={() => totpResetMutation.mutate()}
					onCancel={() => setConfirmTotpReset(false)}
					confirmTestId="user-totp-reset-confirm"
				/>
			)}
		</Modal>
	);
}
