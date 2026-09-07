import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ShieldCheck } from "@/lib/icons";
import { ApiError, api, resetToLogin } from "../../api/client";
import { PageHeader } from "../../components/PageHeader";
import { TotpEnrollmentPanel } from "../../components/totp/TotpEnrollmentPanel";
import { useTotpEnrollment } from "../../components/totp/useTotpEnrollment";
import { useToast } from "../../context/ToastContext";
import { isBreachedPasswordError } from "../../utils/passwordPolicy";

/**
 * Self-service security page for users-row identities: the shared TOTP
 * enrolment panel against /api/auth/totp/*, plus a password change. No session
 * re-mint is needed here because enabling a user's TOTP changes login
 * requirements only, not the session the caller already holds.
 */
export function Security() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();

	const [currentPassword, setCurrentPassword] = useState("");
	const [newPassword, setNewPassword] = useState("");
	const [confirmPassword, setConfirmPassword] = useState("");
	const resetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
	useEffect(
		() => () => {
			if (resetTimerRef.current) clearTimeout(resetTimerRef.current);
		},
		[],
	);

	const totp = useTotpEnrollment(api.userTotp, ["user-totp", "status"]);

	const passwordMutation = useMutation({
		mutationFn: () => api.userTotp.changePassword(currentPassword, newPassword),
		onSuccess: () => {
			// The server revoked every session of the account, this one included.
			// Give the toast a moment, then tear down auth state the same way the
			// logout button does and land on the login screen. The timer is held so
			// navigating away in the meantime does not reload the page underneath.
			toast(t("security.password.success"), "success");
			resetTimerRef.current = setTimeout(() => resetToLogin(queryClient), 1500);
		},
		onError: (err: Error) => {
			if (err instanceof ApiError && err.status === 401) {
				toast(t("security.password.wrongCurrent"), "error");
				return;
			}
			if (isBreachedPasswordError(err)) {
				toast(t("users.validation.passwordBreached"), "error");
				return;
			}
			toast(t("security.password.failed"), "error");
		},
	});

	const passwordFormValid =
		currentPassword.length > 0 &&
		newPassword.length >= 8 &&
		newPassword === confirmPassword;

	return (
		<div className="space-y-6 pb-8">
			<PageHeader
				icon={ShieldCheck}
				title={t("security.title")}
				description={t("security.description")}
			/>
			<div className="ui-card p-6 max-w-2xl">
				<h2 className="text-base font-semibold text-(--text-primary) mb-4">
					{t("settings.totp.title")}
				</h2>
				<TotpEnrollmentPanel
					totp={totp}
					idPrefix="user-totp"
					testIdPrefix="security"
					recoveryRemaining={totp.status?.recovery_remaining}
					recoveryTotal={totp.status?.recovery_total}
				/>
			</div>
			<div className="ui-card p-6 max-w-2xl">
				<h2 className="text-base font-semibold text-(--text-primary) mb-1">
					{t("security.password.title")}
				</h2>
				<p className="text-(--text-secondary) text-sm mb-4">
					{t("security.password.description")}
				</p>
				<form
					className="space-y-3"
					onSubmit={(e) => {
						e.preventDefault();
						passwordMutation.mutate();
					}}
				>
					{/* Hidden username field helps password managers bind the entry. */}
					<input
						type="text"
						autoComplete="username"
						className="hidden"
						tabIndex={-1}
						aria-hidden="true"
						readOnly
					/>
					<div>
						<label
							htmlFor="security-current-password"
							className="block text-sm font-medium text-(--text-primary) mb-2"
						>
							{t("security.password.current")}
						</label>
						<input
							id="security-current-password"
							type="password"
							value={currentPassword}
							onChange={(e) => setCurrentPassword(e.target.value)}
							autoComplete="current-password"
							className="ui-input"
							data-testid="security-current-password"
						/>
					</div>
					<div>
						<label
							htmlFor="security-new-password"
							className="block text-sm font-medium text-(--text-primary) mb-2"
						>
							{t("security.password.new")}
						</label>
						<input
							id="security-new-password"
							type="password"
							value={newPassword}
							onChange={(e) => setNewPassword(e.target.value)}
							autoComplete="new-password"
							placeholder={t("users.modal.passwordPlaceholder")}
							className="ui-input"
							data-testid="security-new-password"
						/>
						{newPassword.length > 0 && newPassword.length < 8 && (
							<p className="text-sm text-red-400 mt-1">
								{t("users.validation.passwordShort")}
							</p>
						)}
					</div>
					<div>
						<label
							htmlFor="security-confirm-password"
							className="block text-sm font-medium text-(--text-primary) mb-2"
						>
							{t("security.password.confirm")}
						</label>
						<input
							id="security-confirm-password"
							type="password"
							value={confirmPassword}
							onChange={(e) => setConfirmPassword(e.target.value)}
							autoComplete="new-password"
							className="ui-input"
							data-testid="security-confirm-password"
						/>
						{confirmPassword.length > 0 && newPassword !== confirmPassword && (
							<p className="text-sm text-red-400 mt-1">
								{t("security.password.mismatch")}
							</p>
						)}
					</div>
					<button
						type="submit"
						disabled={passwordMutation.isPending || !passwordFormValid}
						className="ui-btn ui-btn-primary"
						data-testid="security-password-submit"
					>
						{t("security.password.submit")}
					</button>
				</form>
			</div>
		</div>
	);
}
