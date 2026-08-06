import { useMutation } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { useToast } from "../../context/ToastContext";

/**
 * Sign this identity's other sessions out, keeping the current one.
 *
 * Logging in does not revoke an existing session, so a stolen token stays
 * usable until it expires. This is the operator's lever for that. It is a
 * deliberate action rather than something that happens automatically on every
 * login, because three admin login front-ends share one identity: automatic
 * revocation would evict the operator's other devices during routine sign-ins.
 *
 * Two-step confirm (arm, then commit) rather than a modal, matching the error
 * shelf's "clear all": the action is disruptive enough to want a beat of
 * hesitation, not enough to warrant blocking the page.
 */
export function ActiveSessionsPanel() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [armed, setArmed] = useState(false);

	useEffect(() => {
		if (!armed) return;
		const id = setTimeout(() => setArmed(false), 3000);
		return () => clearTimeout(id);
	}, [armed]);

	const revokeMutation = useMutation({
		mutationFn: () => api.auth.revokeOtherSessions(),
		onSuccess: ({ revoked }) => {
			setArmed(false);
			toast(
				revoked > 0
					? t("settings.activeSessions.signedOut", { count: revoked })
					: t("settings.activeSessions.noneToSignOut"),
				revoked > 0 ? "success" : "info",
			);
		},
		onError: (err: Error) => {
			setArmed(false);
			toast(err.message, "error");
		},
	});

	return (
		<div className="flex items-center justify-between gap-4">
			<div>
				<p className="text-sm font-medium text-gray-300">
					{t("settings.activeSessions.label")}
				</p>
				<p className="text-gray-500 text-xs mt-0.5">
					{t("settings.activeSessions.description")}
				</p>
			</div>
			<button
				type="button"
				data-testid="revoke-other-sessions"
				className="ui-btn ui-btn-secondary shrink-0 disabled:opacity-50 disabled:cursor-not-allowed"
				disabled={revokeMutation.isPending}
				onClick={() => {
					if (!armed) {
						setArmed(true);
						return;
					}
					revokeMutation.mutate();
				}}
			>
				{armed
					? t("settings.activeSessions.confirm")
					: t("settings.activeSessions.action")}
			</button>
		</div>
	);
}
