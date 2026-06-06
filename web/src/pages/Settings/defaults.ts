/**
 * Default values for all settings. When a setting is deleted from the
 * database, the server falls through to its Go-side default — these
 * frontend defaults must match.
 */
export const SETTING_DEFAULTS: Record<string, string> = {
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

	// Data storage & logging
	log_retention: "0",
	stale_request_timeout: "30m0s",
};

/**
 * Mapping of section names to their contained setting keys.
 * Used for section-level reset.
 */
export const SECTION_SETTINGS: Record<string, string[]> = {
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
	],
	dataStorage: ["log_retention", "stale_request_timeout"],
};
