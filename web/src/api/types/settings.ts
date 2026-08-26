export interface BackupEntry {
	filename: string;
	size_bytes: number;
	created_at: string;
	/** "manual" (operator-created), "scheduled" (GFS rotation), or "frontdesk"
	 *  (snapshot Front Desk took before an HA config sync). Absent on responses
	 *  from servers predating origin tracking; treat as manual, matching the
	 *  backend's default for filenames without an origin marker. */
	origin?: "manual" | "scheduled" | "frontdesk";
	/** Whether a signature sidecar exists for this backup, so its integrity can
	 *  be checked on download. Reports presence, not validity: the signature is
	 *  verified when the file is served, not when the list is built. Absent on
	 *  responses from servers predating backup signing, and false for backups
	 *  taken before it or when no MASTER_KEY is configured. */
	signed?: boolean;
}
export interface BackupClassification {
	son: BackupEntry[];
	father: BackupEntry[];
	grandfather: BackupEntry[];
	prune: BackupEntry[];
}
// PublicConfig is the unauthenticated subset of server config the SPA reads to
// render correctly (e.g. hide mutation controls in a read-only demo).
export interface PublicConfig {
	read_only: boolean;
}
// DemoLogin carries the admin token to display on a demo instance's login
// screen (empty unless the server has the demo token feature enabled), so an
// operator can share only the URL. Served by GET /api/demo-login.
export interface DemoLogin {
	token: string;
}
// AlertEventDef describes one operator-subscribable alert event, served by
// GET /api/alert/events. The Alerts settings picker is rendered from this list,
// so a new backend event surfaces in the UI without a frontend change.
export interface AlertEventDef {
	type: string;
	category: string;
	severity: "success" | "info" | "warning" | "error";
	defaultOn: boolean;
}
// AlertStatus reports whether the configured apprise-api container is reachable,
// served by GET /api/alert/status and POST /api/alert/probe. `configured` is
// false when no URL is set; `reachable` means the host answered; `healthy`
// means GET /status returned 2xx. `reason` is a stable machine-readable code
// (not_configured, invalid_url, unreachable, unhealthy) the wizard can branch
// on instead of matching `detail`'s English text.
export interface AlertStatus {
	configured: boolean;
	reachable: boolean;
	healthy: boolean;
	reason?: string;
	detail?: string;
}
// AlertTargets is the saved destination list, served decrypted by
// GET /api/alert/targets for the admin UI's readable list.
export interface AlertTargets {
	targets: string[];
}
