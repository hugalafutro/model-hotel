export interface Provider {
	id: string;
	name: string;
	base_url: string;
	/** Vendor/API family chosen when the provider was added. Fixed after that. */
	provider_type: string;
	masked_key: string;
	enabled: boolean;
	autodiscovery_enabled: boolean;
	scheduled_disable_on: string | null;
	/** Operator's hard ceiling on concurrent requests; null = no ceiling. */
	max_in_flight: number | null;
	last_discovered_at: string | null;
	last_used_at: string | null;
	created_at: string;
	updated_at: string;
	model_count: number;
	total_tokens: number;
	/** The last exhausted 429 this provider answered since the gateway started:
	 *  the only quota reading a provider with no usage API ever gives. Absent
	 *  when there has been none. */
	last_cap?: CapNote;
}
export interface CapNote {
	/** The phrase-table entry the classifier matched; absent when the headers
	 *  decided. Never the response body. */
	phrase?: string;
	model: string;
	/** A spent balance or plan (a person fixes it) rather than a window. */
	entitled?: boolean;
	at: string;
}
export interface CreateProviderRequest {
	name: string;
	base_url: string;
	provider_type: string;
	api_key: string;
}
export interface UpdateProviderRequest {
	name?: string;
	/** Corrects the stored type. A self-hosted type is re-verified by probing. */
	provider_type?: string;
	base_url?: string;
	api_key?: string;
	enabled?: boolean;
	autodiscovery_enabled?: boolean;
	scheduled_disable_on?: string | null;
	/** A number sets the ceiling, null clears it, absent keeps it. */
	max_in_flight?: number | null;
}
