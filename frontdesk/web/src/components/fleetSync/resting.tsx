import { useTranslation } from "react-i18next";
import type { FleetStatus, FleetSyncState, MemberView } from "../../api/types";
import { formatRelative } from "../../utils/time";
import { CopyRow } from "../CopyRow";
import { Notice } from "../Notice";
import { offlinePeers } from "./gates";

// StepResting is the wizard's resting face, shown once a primary is designated:
// which member is primary, the auto-sync state with its Pause/Resume switch,
// where to send traffic, and the token-gated re-run.
export function StepResting({
	status,
	members,
	primaryName,
	enabled,
	lastSync,
	onToggleEnabled,
	onRerun,
}: {
	status: FleetStatus | null;
	members: MemberView[];
	primaryName: string;
	enabled: boolean;
	lastSync: FleetSyncState | null;
	onToggleEnabled: () => void;
	onRerun: () => void;
}) {
	const { t } = useTranslation();
	return (
		<div className="fd-stack">
			<Notice variant="info" style={{ marginTop: "0.2rem" }}>
				<strong>{t("settings.wizard.restingPrimaryLabel")}:</strong>{" "}
				{primaryName}
				<div
					className="fd-faint"
					style={{ fontSize: "0.82rem", marginTop: "0.3rem" }}
				>
					{lastSync
						? t("settings.wizard.restingLastRun", {
								when: formatRelative(lastSync.last_run_at),
							})
						: t("settings.wizard.restingNeverRun")}
				</div>
			</Notice>

			{status && offlinePeers(status).length > 0 && (
				<Notice variant="warn">
					{t("settings.wizard.skippedOffline")}
					<ul style={{ margin: "0.4rem 0 0" }}>
						{offlinePeers(status).map((m) => (
							<li key={m.member_id}>{m.name}</li>
						))}
					</ul>
				</Notice>
			)}

			<div className="fd-row" style={{ gap: "0.6rem", alignItems: "center" }}>
				<span
					className={`ui-badge ${enabled ? "ui-badge-ok" : "ui-badge-warn"}`}
				>
					{t(
						enabled
							? "settings.wizard.autoSyncOn"
							: "settings.wizard.autoSyncOff",
					)}
				</span>
				<button
					type="button"
					className="ui-btn ui-btn-sm"
					onClick={onToggleEnabled}
				>
					{t(
						enabled ? "settings.wizard.pauseBtn" : "settings.wizard.resumeBtn",
					)}
				</button>
			</div>
			<p className="fd-faint" style={{ fontSize: "0.8rem", margin: 0 }}>
				{t(
					enabled
						? "settings.wizard.autoSyncOnHint"
						: "settings.wizard.autoSyncOffHint",
				)}
			</p>

			{status && <UsageSection status={status} members={members} />}

			<div
				className="fd-stack"
				style={{
					marginTop: "0.6rem",
					paddingTop: "0.8rem",
					borderTop: "1px solid var(--border, rgba(128,128,128,0.25))",
				}}
			>
				<h4 style={{ fontSize: "0.95rem", margin: 0 }}>
					{t("settings.wizard.rerunHeading")}
				</h4>
				<Notice variant="warn">{t("settings.wizard.rerunWarning")}</Notice>
				<div>
					<button
						type="button"
						className="ui-btn ui-btn-danger"
						onClick={onRerun}
					>
						{t("settings.wizard.rerunBtn")}
					</button>
				</div>
			</div>
		</div>
	);
}

// UsageSection tells the operator where to send traffic once the fleet is
// converged: the direct /v1 URL, the reverse-proxy forward target, and the
// active-member pool behind the load balancer.
function UsageSection({
	status,
	members,
}: {
	status: FleetStatus;
	members: MemberView[];
}) {
	const { t } = useTranslation();
	// The load balancer's pool is exactly the active members (BuildTraefikConfig
	// drops drained ones), so list those as the instances behind it.
	const pool = members.filter((m) => m.state === "active");
	// Front Desk knows the LB port (LB_PORT) but not the operator's public host,
	// so pair the port with the host they reached this UI on: in the single-stack
	// HA compose that is the same machine the load balancer runs on.
	const port = status.lb_port ?? "8080";
	const host =
		typeof window !== "undefined" ? window.location.hostname : "your-host";
	const directURL = `http://${host}:${port}/v1`;
	const forwardURL = `http://${host}:${port}`;

	return (
		<div>
			<h4 style={{ fontSize: "0.95rem", margin: "0 0 0.3rem" }}>
				{t("settings.wizard.doneUseTitle")}
			</h4>
			<p className="fd-faint" style={{ fontSize: "0.83rem", margin: 0 }}>
				{t("settings.wizard.doneUseIntro")}
			</p>

			<div style={{ marginTop: "0.8rem" }}>
				<div style={{ fontSize: "0.85rem", marginBottom: "0.3rem" }}>
					{t("settings.wizard.doneDirectTitle")}
				</div>
				<CopyRow value={directURL} />
			</div>

			<div style={{ marginTop: "0.9rem" }}>
				<div style={{ fontSize: "0.85rem", marginBottom: "0.3rem" }}>
					{t("settings.wizard.doneProxyTitle")}
				</div>
				<div
					className="fd-faint"
					style={{ fontSize: "0.82rem", marginBottom: "0.3rem" }}
				>
					{t("settings.wizard.doneProxyForward")}
				</div>
				<CopyRow value={forwardURL} />
				<div
					className="fd-faint"
					style={{ fontSize: "0.82rem", marginTop: "0.4rem" }}
				>
					{t("settings.wizard.doneProxyClients")}
				</div>
			</div>

			<p
				className="fd-faint"
				style={{ fontSize: "0.78rem", marginTop: "0.7rem" }}
			>
				{t("settings.wizard.donePortNote", { port })}
			</p>

			<div style={{ marginTop: "0.9rem" }}>
				<div style={{ fontSize: "0.85rem", marginBottom: "0.3rem" }}>
					{t("settings.wizard.donePoolTitle")}
				</div>
				{pool.length === 0 ? (
					<div className="fd-faint" style={{ fontSize: "0.82rem" }}>
						{t("settings.wizard.donePoolEmpty")}
					</div>
				) : (
					<ul className="fd-mono" style={{ margin: 0, fontSize: "0.82rem" }}>
						{pool.map((m) => (
							<li key={m.id}>{m.url}</li>
						))}
					</ul>
				)}
			</div>

			<Notice variant="info" style={{ marginTop: "1rem" }}>
				<strong>{t("settings.wizard.doneHttpsTitle")}</strong>{" "}
				{t("settings.wizard.doneHttpsNote")}
			</Notice>
		</div>
	);
}
