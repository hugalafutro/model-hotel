import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { deviceSummary } from "@web-shared/device";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { AuthSession } from "../../api/types";
import { useToast } from "../../context/ToastContext";
import { useArmedConfirm } from "../../hooks/useArmedConfirm";
import { formatRelativeTime } from "../../utils/format";

// Arm key for the bulk button. Session ids are UUIDs, so it cannot collide
// with a row.
const ALL_SESSIONS = "all";

/**
 * The identity's live sessions, one row per device, with a per-row sign-out
 * and the bulk "sign out others" the panel started as.
 *
 * Logging in does not revoke an existing session, so a stolen token stays
 * usable until it expires. This panel is the operator's lever for that: the
 * list (device, IP, last seen) is how they spot a session that isn't theirs,
 * the row button ends exactly that one, and the bulk button ends everything
 * but this browser. Revocation stays a deliberate action rather than a
 * side-effect of login, because three admin login front-ends share one
 * identity: automatic revocation would evict the operator's other devices
 * during routine sign-ins.
 *
 * Both actions use a two-step confirm (arm, then commit) rather than a modal,
 * matching the error shelf's "clear all": disruptive enough to want a beat of
 * hesitation, not enough to warrant blocking the page.
 */
export function ActiveSessionsPanel() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	// One arm slot for both buttons, keyed by row id (or ALL_SESSIONS for the
	// bulk action), so arming one disarms any other.
	const { armed, fire, disarm } = useArmedConfirm<string>();
	const armedAll = armed === ALL_SESSIONS;

	const sessionsQuery = useQuery({
		queryKey: ["auth-sessions"],
		queryFn: api.auth.listSessions,
	});

	const invalidate = () =>
		queryClient.invalidateQueries({ queryKey: ["auth-sessions"] });

	const revokeAllMutation = useMutation({
		mutationFn: () => api.auth.revokeOtherSessions(),
		onSuccess: ({ revoked }) => {
			invalidate();
			toast(
				revoked > 0
					? t("settings.activeSessions.signedOut", { count: revoked })
					: t("settings.activeSessions.noneToSignOut"),
				revoked > 0 ? "success" : "info",
			);
		},
		onError: (err: Error) => {
			toast(err.message, "error");
		},
	});

	const revokeOneMutation = useMutation({
		mutationFn: (id: string) => api.auth.revokeSession(id),
		onSuccess: () => {
			invalidate();
			toast(t("settings.activeSessions.signedOut", { count: 1 }), "success");
		},
		onError: (err: Error) => {
			// The list may be stale (the session already gone, or it became the
			// current one); refetch so the rows match the server again.
			invalidate();
			toast(err.message, "error");
		},
	});

	const sessions = sessionsQuery.data?.sessions;

	return (
		<div className="space-y-3">
			<div className="flex items-center justify-between gap-4">
				<div>
					<p className="text-sm font-medium text-gray-300">
						{t("settings.activeSessions.label")}
					</p>
					<p className="text-gray-500 text-xs mt-0.5">
						{t("settings.activeSessions.description")}
					</p>
				</div>
				{/* Danger styling with a ring while armed, matching the clear-all
				    control in ArenaHistoryModal. The label alone was too quiet a
				    signal for an action that ends every other session. */}
				<button
					type="button"
					data-testid="revoke-other-sessions"
					className={`ui-btn ui-btn-danger shrink-0 ${
						armedAll ? "ring-2 ring-red-400/50" : ""
					}`}
					disabled={revokeAllMutation.isPending}
					onBlur={disarm}
					onClick={() => fire(ALL_SESSIONS, () => revokeAllMutation.mutate())}
				>
					{armedAll
						? t("settings.activeSessions.confirm")
						: t("settings.activeSessions.action")}
				</button>
			</div>

			{sessionsQuery.isError && (
				<p className="text-xs text-red-400">
					{(sessionsQuery.error as Error).message}
				</p>
			)}

			{sessions && (
				/* Capped and scrollable: an identity can accumulate dozens of live
				   sessions, and the panel must not stretch the Settings page. The
				   current session is served first, so the anchor row stays visible
				   without scrolling. */
				<ul className="m-0 max-h-64 list-none divide-y divide-white/5 overflow-y-auto p-0 pr-1">
					{sessions.map((s) => (
						<SessionRow
							key={s.id}
							session={s}
							armed={armed === s.id}
							pending={
								revokeOneMutation.isPending &&
								revokeOneMutation.variables === s.id
							}
							onClick={() => fire(s.id, () => revokeOneMutation.mutate(s.id))}
							onDisarm={disarm}
						/>
					))}
				</ul>
			)}
		</div>
	);
}

function SessionRow({
	session,
	armed,
	pending,
	onClick,
	onDisarm,
}: {
	session: AuthSession;
	armed: boolean;
	pending: boolean;
	onClick: () => void;
	onDisarm: () => void;
}) {
	const { t } = useTranslation();
	const device =
		deviceSummary(session.user_agent) ??
		t("settings.activeSessions.unknownDevice");

	// IP, signed-in age, and (when the session has authenticated a request
	// since the stamp existed) last activity. Joined here rather than in one
	// translated sentence so each fragment stays optional.
	const details = [
		session.ip,
		t("settings.activeSessions.signedIn", {
			when: formatRelativeTime(session.created_at),
		}),
		session.last_seen_at
			? t("settings.activeSessions.lastSeen", {
					when: formatRelativeTime(session.last_seen_at),
				})
			: null,
	].filter(Boolean);

	return (
		<li
			data-testid="auth-session-row"
			className="flex items-center justify-between gap-4 py-2"
		>
			<div className="min-w-0">
				<div className="flex items-center gap-2">
					{/* The raw user agent in the tooltip: the summary is meant to be
					    recognizable, the tooltip is the evidence. */}
					<p
						className="truncate text-sm font-medium text-gray-300"
						title={session.user_agent}
					>
						{device}
					</p>
					{session.current && (
						<span
							data-testid="current-session-chip"
							className="ui-badge ui-badge-accent shrink-0"
						>
							{t("settings.activeSessions.currentBadge")}
						</span>
					)}
				</div>
				<p className="text-gray-500 text-xs mt-0.5 truncate">
					{details.join(" · ")}
				</p>
			</div>
			{!session.current && (
				<button
					type="button"
					data-testid="revoke-session"
					className={`ui-btn ui-btn-danger shrink-0 ${
						armed ? "ring-2 ring-red-400/50" : ""
					}`}
					disabled={pending}
					onBlur={onDisarm}
					onClick={onClick}
				>
					{armed
						? t("settings.activeSessions.confirm")
						: t("settings.activeSessions.signOutOne")}
				</button>
			)}
		</li>
	);
}
