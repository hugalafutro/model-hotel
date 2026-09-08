/**
 * AlertStatus reports whether the configured apprise-api container is reachable,
 * served by GET /api/alert/status and POST /api/alert/probe and mirroring
 * alert.Status. `configured` is false when no URL is set; `reachable` means the
 * host answered; `healthy` means GET /status returned 2xx. `reason` is a stable
 * machine-readable code (not_configured, invalid_url, unreachable, unhealthy)
 * the wizard can branch on instead of matching `detail`'s English text.
 *
 * One declaration for both apps and the shared wizard machine, so a field the
 * server adds reaches every reader at once.
 */
export interface AlertStatus {
	configured: boolean;
	reachable: boolean;
	healthy: boolean;
	reason?: string;
	detail?: string;
}
