import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { DashboardUser, UserUpsertRequest } from "../../api/types";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { useIdentity } from "../../context/IdentityContext";
import { isBreachedPasswordError } from "../../utils/passwordPolicy";

/** Duck-typed ApiError body (robust across module boundaries, like App.tsx). */
function errMessage(err: unknown, fallback: string): string {
	if (err && typeof err === "object" && "message" in err) {
		const m = (err as { message?: unknown }).message;
		if (typeof m === "string" && m) return m;
	}
	return fallback;
}

// eslint-disable-next-line max-lines-per-function -- size ratchet: split this component
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
	const queryClient = useQueryClient();
	const { me } = useIdentity();
	const isEdit = user !== null;
	const isSelf = isEdit && me?.username === user.username;

	const [username, setUsername] = useState(user?.username ?? "");
	const [displayName, setDisplayName] = useState(user?.display_name ?? "");
	const [email, setEmail] = useState(user?.email ?? "");
	const [password, setPassword] = useState("");
	const [role, setRole] = useState<"admin" | "user">(user?.role ?? "user");
	const [grants, setGrants] = useState<string[]>(user?.grants ?? []);
	const [enabled, setEnabled] = useState(user?.enabled ?? true);
	const [limitRps, setLimitRps] = useState(
		user?.rate_limit_rps?.toString() ?? "",
	);
	const [limitBurst, setLimitBurst] = useState(
		user?.rate_limit_burst?.toString() ?? "",
	);
	const [limitTpm, setLimitTpm] = useState(
		user?.rate_limit_tpm?.toString() ?? "",
	);
	// A stored cap is an explicit array; null/absent means "every provider".
	// On create neither mode is preselected: capping a new account has to be a
	// deliberate choice, since existing accounts were grandfathered uncapped.
	const [providerMode, setProviderMode] = useState<"all" | "selected" | null>(
		user ? (user.allowed_providers ? "selected" : "all") : null,
	);
	const [selectedProviders, setSelectedProviders] = useState<string[]>(
		user?.allowed_providers ?? [],
	);
	const [providerError, setProviderError] = useState<
		"required" | "empty" | null
	>(null);
	const [error, setError] = useState<string | null>(null);
	const [confirmDelete, setConfirmDelete] = useState(false);
	const [confirmTotpReset, setConfirmTotpReset] = useState(false);
	const [resetValue, setResetValue] = useState("");

	// The checkbox list renders from the backend catalog, so a new grant kind
	// appears here without a frontend release.
	const { data: catalog } = useQuery({
		queryKey: ["user-grants"],
		queryFn: () => api.users.grants(),
		staleTime: Number.POSITIVE_INFINITY,
	});
	const allGrants = catalog?.grants ?? [];

	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});
	const sortedProviders = (providers ?? [])
		.slice()
		.sort((a, b) => a.name.localeCompare(b.name));

	// True while an edit leaves the stored cap exactly as it was found. Such a
	// save OMITS allowed_providers, which the API reads as "preserve" (see
	// UpdateUser: an absent key copies the stored value forward). That is the
	// only way to edit a user whose stored cap is an empty array, which is a
	// reachable state -- pruning the last capped provider empties it -- and one
	// the control cannot re-state without widening the account.
	const storedCap = user?.allowed_providers ?? null;
	const capUnchanged =
		isEdit &&
		providerMode === (storedCap ? "selected" : "all") &&
		(providerMode === "all" ||
			(selectedProviders.length === (storedCap?.length ?? 0) &&
				selectedProviders.every((id) => storedCap?.includes(id))));

	const invalidate = () =>
		queryClient.invalidateQueries({ queryKey: ["users"] });

	// A breached-password rejection comes back as a stable English 400 string;
	// swap it for localized copy while leaving every other server error (e.g. a
	// duplicate username) to surface its own message verbatim.
	const passwordSaveError = (err: unknown): string =>
		isBreachedPasswordError(err)
			? t("users.validation.passwordBreached")
			: errMessage(err, t("users.toast.saveFailed"));

	const buildRequest = (): UserUpsertRequest => ({
		username: username.trim(),
		display_name: displayName.trim(),
		email: email.trim() ? email.trim() : null,
		role,
		grants: role === "admin" ? [] : grants,
		rate_limit_rps: limitRps !== "" ? parseFloat(limitRps) : null,
		rate_limit_burst: limitBurst !== "" ? parseInt(limitBurst, 10) : null,
		rate_limit_tpm: limitTpm !== "" ? parseInt(limitTpm, 10) : null,
		// Omitted when untouched (preserve), explicit null clears the cap.
		// handleSave guarantees a mode was picked before anything is sent.
		...(capUnchanged
			? {}
			: {
					allowed_providers: providerMode === "all" ? null : selectedProviders,
				}),
		...(isEdit ? { enabled } : { password }),
	});

	const saveMutation = useMutation({
		mutationFn: () =>
			isEdit
				? api.users.update(user.id, buildRequest())
				: api.users.create(buildRequest()),
		onSuccess: () => {
			invalidate();
			onToast(
				isEdit ? t("users.toast.updated") : t("users.toast.created"),
				"success",
			);
			onClose();
		},
		onError: (err) => setError(passwordSaveError(err)),
	});

	const deleteMutation = useMutation({
		mutationFn: () => api.users.remove(user?.id ?? ""),
		onSuccess: () => {
			invalidate();
			onToast(t("users.toast.deleted"), "success");
			onClose();
		},
		onError: (err) => {
			setConfirmDelete(false);
			setError(errMessage(err, t("users.toast.deleteFailed")));
		},
	});

	const resetMutation = useMutation({
		mutationFn: () => api.users.setPassword(user?.id ?? "", resetValue),
		onSuccess: () => {
			setResetValue("");
			onToast(t("users.toast.passwordReset"), "success");
		},
		onError: (err) => setError(passwordSaveError(err)),
	});

	const totpResetMutation = useMutation({
		mutationFn: () => api.users.resetTotp(user?.id ?? ""),
		onSuccess: () => {
			setConfirmTotpReset(false);
			invalidate();
			onToast(t("users.toast.totpReset"), "success");
		},
		onError: (err) => {
			setConfirmTotpReset(false);
			setError(errMessage(err, t("users.toast.saveFailed")));
		},
	});

	const handleSave = () => {
		setError(null);
		setProviderError(null);
		if (!username.trim()) {
			setError(t("users.validation.usernameRequired"));
			return;
		}
		if (!isEdit && password.length < 8) {
			setError(t("users.validation.passwordShort"));
			return;
		}
		// Skipped when the cap is untouched: that save omits the field entirely,
		// so a stored empty array stays stored instead of blocking every edit.
		if (!capUnchanged) {
			if (providerMode === null) {
				setProviderError("required");
				return;
			}
			// The API rejects an empty array, so never let one leave the form.
			if (providerMode === "selected" && selectedProviders.length === 0) {
				setProviderError("empty");
				return;
			}
		}
		saveMutation.mutate();
	};

	// Both provider controls clear the validation message as soon as the admin
	// acts on it, so a stale complaint never outlives the thing it complained about.
	const chooseProviderMode = (mode: "all" | "selected") => {
		setProviderError(null);
		setProviderMode(mode);
	};

	const toggleProvider = (id: string) => {
		setProviderError(null);
		setSelectedProviders((prev) =>
			prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
		);
	};

	const toggleGrant = (g: string) =>
		setGrants((prev) =>
			prev.includes(g) ? prev.filter((x) => x !== g) : [...prev, g],
		);

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

						{user.totp_enabled && (
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
