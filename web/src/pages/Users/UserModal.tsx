import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { DashboardUser, UserUpsertRequest } from "../../api/types";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";
import { useIdentity } from "../../context/IdentityContext";

/** Duck-typed ApiError body (robust across module boundaries, like App.tsx). */
function errMessage(err: unknown, fallback: string): string {
	if (err && typeof err === "object" && "message" in err) {
		const m = (err as { message?: unknown }).message;
		if (typeof m === "string" && m) return m;
	}
	return fallback;
}

export function UserModal({
	user,
	onClose,
	onToast,
}: {
	/** null opens the modal in create mode. */
	user: DashboardUser | null;
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
	const [error, setError] = useState<string | null>(null);
	const [confirmDelete, setConfirmDelete] = useState(false);
	const [resetValue, setResetValue] = useState("");

	// The checkbox list renders from the backend catalog, so a new grant kind
	// appears here without a frontend release.
	const { data: catalog } = useQuery({
		queryKey: ["user-grants"],
		queryFn: () => api.users.grants(),
		staleTime: Number.POSITIVE_INFINITY,
	});
	const allGrants = catalog?.grants ?? [];

	const invalidate = () =>
		queryClient.invalidateQueries({ queryKey: ["users"] });

	const buildRequest = (): UserUpsertRequest => ({
		username: username.trim(),
		display_name: displayName.trim(),
		email: email.trim() ? email.trim() : null,
		role,
		grants: role === "admin" ? [] : grants,
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
		onError: (err) => setError(errMessage(err, t("users.toast.saveFailed"))),
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
		onError: (err) => setError(errMessage(err, t("users.toast.saveFailed"))),
	});

	const handleSave = () => {
		setError(null);
		if (!username.trim()) {
			setError(t("users.validation.usernameRequired"));
			return;
		}
		if (!isEdit && password.length < 8) {
			setError(t("users.validation.passwordShort"));
			return;
		}
		saveMutation.mutate();
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
						disabled={isSelf}
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
									/>
									{t(`users.grant.${g}`, { defaultValue: g })}
								</label>
							))}
						</div>
					</fieldset>
				)}

				{isEdit && (
					<div className="flex items-center justify-between">
						<span className="text-sm font-medium text-gray-300">
							{t("users.modal.enabled")}
						</span>
						<Toggle
							checked={enabled}
							onChange={setEnabled}
							disabled={isSelf}
							ariaLabel={t("users.modal.enabled")}
						/>
					</div>
				)}

				<div className="flex gap-3 pt-2">
					<button
						type="button"
						onClick={handleSave}
						disabled={saveMutation.isPending}
						className="ui-btn ui-btn-primary flex-1 disabled:opacity-50"
						data-testid="user-modal-save"
					>
						{isEdit ? t("users.modal.save") : t("users.modal.create")}
					</button>
					<button type="button" onClick={onClose} className="ui-btn flex-1">
						{t("users.modal.cancel")}
					</button>
				</div>

				{isEdit && (
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
									className="ui-btn disabled:opacity-50"
								>
									{t("users.modal.resetButton")}
								</button>
							</div>
							<p className="text-xs text-gray-500 mt-1">
								{t("users.modal.resetHint")}
							</p>
						</div>

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
					message={t("users.modal.deleteConfirmMessage")}
					fields={[user?.username ?? ""]}
					onConfirm={() => deleteMutation.mutate()}
					onCancel={() => setConfirmDelete(false)}
					confirmTestId="user-delete-confirm"
				/>
			)}
		</Modal>
	);
}
