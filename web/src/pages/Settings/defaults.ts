/**
 * Default values for all settings. When a setting is deleted from the
 * database, the server falls through to its Go-side default — these
 * frontend defaults MUST match the Go defaults in:
 *   - internal/settings/settings.go (GetBool/GetInt/GetWithDefault fallbacks)
 *   - internal/api/settings.go (allowedSettings map)
 *
 * There is no automated cross-language sync test. When changing a Go
 * default, update the corresponding entry here (and in en.json labels).
 */
export const SETTING_DEFAULTS: Record<SettingKey, string> = {
	// Discovery
	discovery_interval: "6h",
	discovery_on_startup: "true",
	discovery_on_provider_create: "true",

	// Proxy
	request_timeout: "1m0s",
	key_cache_ttl: "10m0s",
	ttft_timeout: "1m0s",
	stream_stall_timeout: "30s",

	// Rate limiting
	rate_limit_enabled: "true",
	rate_limit_ip_enabled: "true",
	rate_limit_rps: "10",
	rate_limit_burst: "20",
	rate_limit_ip_rps: "30",
	rate_limit_ip_burst: "60",
	rate_limit_max_wait_ms: "200",

	// Circuit breaker & failover
	circuit_breaker_enabled: "true",
	circuit_breaker_threshold: "5",
	circuit_breaker_cooldown: "1m0s",
	failover_on_rate_limit: "true",
	hedging_enabled: "false",
	hedge_delay: "4s",

	// Data storage & logging
	log_retention: "0",
	stale_request_timeout: "30m0s",

	// Alerting (Apprise). alert_events MUST match Go's alert.DefaultEnabledCSV()
	// (internal/alert/catalog.go) — the comma-joined default-on event types.
	alert_enabled: "false",
	alert_apprise_api_url: "",
	alert_apprise_targets: "",
	alert_events:
		"circuit_breaker.open,circuit_breaker.closed,failover.sync_error",

	// Authentication. Dashboard auto-logout after inactivity, in minutes; 0
	// disables it. Consumed entirely on the frontend (see useIdleLogout); the
	// backend only validates/stores the value.
	session_idle_timeout_minutes: "60",
};

export type SectionName =
	| "discovery"
	| "proxy"
	| "rateLimit"
	| "circuitBreaker"
	| "dataStorage"
	| "alerts";

/**
 * Mapping of section names to their contained setting keys.
 * Used for section-level reset.
 */
export const SECTION_SETTINGS: Record<SectionName, string[]> = {
	discovery: [
		"discovery_interval",
		"discovery_on_startup",
		"discovery_on_provider_create",
	],
	proxy: [
		"request_timeout",
		"key_cache_ttl",
		"ttft_timeout",
		"stream_stall_timeout",
	],
	rateLimit: [
		"rate_limit_enabled",
		"rate_limit_ip_enabled",
		"rate_limit_rps",
		"rate_limit_burst",
		"rate_limit_ip_rps",
		"rate_limit_ip_burst",
		"rate_limit_max_wait_ms",
	],
	circuitBreaker: [
		"circuit_breaker_enabled",
		"circuit_breaker_threshold",
		"circuit_breaker_cooldown",
		"failover_on_rate_limit",
		"hedging_enabled",
		"hedge_delay",
	],
	dataStorage: ["log_retention", "stale_request_timeout"],
	alerts: [
		"alert_enabled",
		"alert_apprise_api_url",
		"alert_apprise_targets",
		"alert_events",
	],
};

export type SettingKey =
	| "discovery_interval"
	| "discovery_on_startup"
	| "discovery_on_provider_create"
	| "request_timeout"
	| "key_cache_ttl"
	| "ttft_timeout"
	| "stream_stall_timeout"
	| "rate_limit_enabled"
	| "rate_limit_ip_enabled"
	| "rate_limit_rps"
	| "rate_limit_burst"
	| "rate_limit_ip_rps"
	| "rate_limit_ip_burst"
	| "rate_limit_max_wait_ms"
	| "circuit_breaker_enabled"
	| "circuit_breaker_threshold"
	| "circuit_breaker_cooldown"
	| "failover_on_rate_limit"
	| "hedging_enabled"
	| "hedge_delay"
	| "log_retention"
	| "stale_request_timeout"
	| "alert_enabled"
	| "alert_apprise_api_url"
	| "alert_apprise_targets"
	| "alert_events"
	| "session_idle_timeout_minutes";

/**
 * Mapping from DB setting keys to their human-readable i18n keys.
 * Used in reset confirm dialogs to show names the user recognizes.
 */
export const SETTING_LABELS: Record<SettingKey, string> = {
	discovery_interval: "settings.discovery.discoveryInterval",
	discovery_on_startup: "settings.discovery.discoverOnStartup",
	discovery_on_provider_create: "settings.discovery.discoverOnProviderCreation",
	request_timeout: "settings.proxy.requestTimeout",
	key_cache_ttl: "settings.proxy.keyCacheTtl",
	ttft_timeout: "settings.proxy.ttftTimeout",
	stream_stall_timeout: "settings.proxy.streamStallTimeout",
	rate_limit_enabled: "settings.rateLimit.enable",
	rate_limit_ip_enabled: "settings.rateLimit.ipRateLimiting",
	rate_limit_rps: "settings.rateLimit.requestsPerSecond",
	rate_limit_burst: "settings.rateLimit.burstSize",
	rate_limit_ip_rps: "settings.rateLimit.ipRequestsPerSecond",
	rate_limit_ip_burst: "settings.rateLimit.ipBurstSize",
	rate_limit_max_wait_ms: "settings.rateLimit.maxWait",
	circuit_breaker_enabled: "settings.circuitBreaker.enable",
	circuit_breaker_threshold: "settings.circuitBreaker.failureThreshold",
	circuit_breaker_cooldown: "settings.circuitBreaker.cooldownPeriod",
	failover_on_rate_limit: "settings.circuitBreaker.failoverOnRateLimit",
	hedging_enabled: "settings.circuitBreaker.hedging",
	hedge_delay: "settings.circuitBreaker.hedgeDelay",
	log_retention: "settings.logging.logRetention",
	stale_request_timeout: "settings.logging.staleRequestTimeout",
	alert_enabled: "settings.alerts.enable",
	alert_apprise_api_url: "settings.alerts.apiUrl",
	alert_apprise_targets: "settings.alerts.target",
	alert_events: "settings.alerts.eventsLabel",
	session_idle_timeout_minutes: "settings.sessionTimeout.label",
};
