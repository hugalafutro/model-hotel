import { deviceSummary } from "@web-shared/device";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type { AuthSession } from "../api/types";
import { useToast } from "../context/ToastContext";
import { formatRelative } from "../utils/time";
import { ConfirmModal } from "./ConfirmModal";

// ActiveSessionsPanel is Settings → Active sessions: the admin identity's live
// browser sessions (device, IP, signed-in / last-active times), a per-row
// sign-out, and the bulk "sign out others" that keeps only this browser.
//
// Logging in does not revoke an existing session, so a stolen token stays
// usable until it expires; this panel is the operator's lever for that. The
// list is how they spot a session that isn't theirs, the row action ends
// exactly that one, and both actions confirm through the shared ConfirmModal
// like every other destructive action on this page. Mirrors the main
// dashboard's panel; paired Bellhop devices are separate (and managed in the
// Paired devices panel) — they never appear here.

export function ActiveSessionsPanel() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [sessions, setSessions] = useState<AuthSession[] | null>(null);
	const [revoking, setRevoking] = useState<AuthSession | null>(null);
	const [revokingAll, setRevokingAll] = useState(false);
	const [busy, setBusy] = useState(false);

	// A failed refresh keeps the last known list; a failed initial load leaves
	// the panel quiet (renders header only), like the other Settings panels.
	const refresh = useCallback(() => {
		api
			.getSessions()
			.then((r) => setSessions(r.sessions))
			.catch(() => {});
	}, []);

	useEffect(() => {
		refresh();
	}, [refresh]);

	const confirmRevokeOne = async () => {
		if (!revoking) return;
		setBusy(true);
		try {
			await api.revokeSession(revoking.id);
			toast(t("settings.sessions.signedOut", { count: 1 }), "success");
		} catch {
			toast(t("errors.generic"), "error");
		} finally {
			setBusy(false);
			setRevoking(null);
			refresh();
		}
	};

	const confirmRevokeAll = async () => {
		setBusy(true);
		try {
			const { revoked } = await api.revokeOtherSessions();
			toast(
				revoked > 0
					? t("settings.sessions.signedOut", { count: revoked })
					: t("settings.sessions.noneToSignOut"),
				revoked > 0 ? "success" : "info",
			);
		} catch {
			toast(t("errors.generic"), "error");
		} finally {
			setBusy(false);
			setRevokingAll(false);
			refresh();
		}
	};

	return (
		<section className="ui-card ui-card-pad">
			<div
				className="fd-row"
				style={{ justifyContent: "space-between", alignItems: "flex-start" }}
			>
				<div>
					<h2 className="fd-card-title">{t("settings.sessions.title")}</h2>
					<p
						className="fd-faint"
						style={{ fontSize: "0.82rem", margin: "0.3rem 0 0" }}
					>
						{t("settings.sessions.description")}
					</p>
				</div>
				<button
					type="button"
					data-testid="revoke-other-sessions"
					className="ui-btn ui-btn-danger"
					disabled={busy}
					onClick={() => setRevokingAll(true)}
				>
					{t("settings.sessions.signOutOthers")}
				</button>
			</div>

			{sessions && (
				/* Capped and scrollable: sessions can accumulate, and the panel must
				   not stretch the Settings page. The current session is served
				   first, so the anchor row stays visible without scrolling. */
				<ul
					className="fd-stack"
					style={{
						listStyle: "none",
						margin: "0.8rem 0 0",
						padding: "0 0.25rem 0 0",
						gap: "0.5rem",
						maxHeight: "16rem",
						overflowY: "auto",
					}}
				>
					{sessions.map((s) => (
						<SessionRow
							key={s.id}
							session={s}
							busy={busy}
							onRevoke={() => setRevoking(s)}
						/>
					))}
				</ul>
			)}

			{revoking && (
				<ConfirmModal
					title={t("settings.sessions.confirmSignOutTitle")}
					confirmLabel={t("settings.sessions.confirmSignOut")}
					busy={busy}
					onConfirm={confirmRevokeOne}
					onClose={() => setRevoking(null)}
				>
					<p>
						{t("settings.sessions.confirmSignOutBody", {
							device:
								deviceSummary(revoking.user_agent) ??
								t("settings.sessions.unknownDevice"),
						})}
					</p>
				</ConfirmModal>
			)}

			{revokingAll && (
				<ConfirmModal
					title={t("settings.sessions.confirmSignOutOthersTitle")}
					confirmLabel={t("settings.sessions.confirmSignOutOthers")}
					busy={busy}
					onConfirm={confirmRevokeAll}
					onClose={() => setRevokingAll(false)}
				>
					<p>{t("settings.sessions.confirmSignOutOthersBody")}</p>
				</ConfirmModal>
			)}
		</section>
	);
}

function SessionRow({
	session,
	busy,
	onRevoke,
}: {
	session: AuthSession;
	busy: boolean;
	onRevoke: () => void;
}) {
	const { t } = useTranslation();
	const device =
		deviceSummary(session.user_agent) ?? t("settings.sessions.unknownDevice");

	// IP, signed-in age, and (once the session has authenticated a request
	// since the stamp existed) last activity. Joined here rather than in one
	// translated sentence so each fragment stays optional.
	const details = [
		session.ip,
		t("settings.sessions.signedIn", {
			when: formatRelative(session.created_at),
		}),
		session.last_seen_at
			? t("settings.sessions.lastSeen", {
					when: formatRelative(session.last_seen_at),
				})
			: null,
	].filter(Boolean);

	return (
		<li
			data-testid="auth-session-row"
			className="fd-row"
			style={{ justifyContent: "space-between", gap: "0.8rem" }}
		>
			<div style={{ minWidth: 0 }}>
				<div className="fd-row" style={{ gap: "0.5rem" }}>
					{/* The raw user agent in the tooltip: the summary is meant to be
					    recognizable, the tooltip is the evidence. */}
					<span title={session.user_agent}>{device}</span>
					{session.current && (
						<span
							data-testid="current-session-chip"
							className="ui-badge ui-badge-info"
						>
							{t("settings.sessions.currentBadge")}
						</span>
					)}
				</div>
				<div className="fd-faint" style={{ fontSize: "0.78rem" }}>
					{details.join(" · ")}
				</div>
			</div>
			{!session.current && (
				<button
					type="button"
					data-testid="revoke-session"
					className="ui-btn ui-btn-danger"
					disabled={busy}
					onClick={onRevoke}
				>
					{t("settings.sessions.signOut")}
				</button>
			)}
		</li>
	);
}
