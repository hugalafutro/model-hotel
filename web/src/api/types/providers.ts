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
	last_discovered_at: string | null;
	last_used_at: string | null;
	created_at: string;
	updated_at: string;
	model_count: number;
	total_tokens: number;
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
}
