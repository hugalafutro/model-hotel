export interface NanoGPTUsageLimits {
	weeklyInputTokens: number | null;
	dailyInputTokens: number | null;
	dailyImages: number | null;
}
export interface NanoGPTUsageTokenInfo {
	used: number;
	remaining: number;
	percentUsed: number;
	resetAt: number;
}
export interface NanoGPTUsageDailyImages {
	used: number;
	remaining: number;
	percentUsed: number;
	resetAt: number;
}
export interface NanoGPTUsagePeriod {
	currentPeriodEnd: string;
}
export interface NanoGPTUsage {
	active: boolean;
	provider: string;
	providerStatus: string;
	providerStatusRaw: string;
	stripeSubscriptionId: string;
	cancellationReason: string | null;
	canceledAt: string | null;
	endedAt: string | null;
	cancelAt: string | null;
	cancelAtPeriodEnd: boolean;
	limits: NanoGPTUsageLimits;
	allowOverage: boolean;
	period: NanoGPTUsagePeriod;
	dailyImages: NanoGPTUsageDailyImages | null;
	dailyInputTokens: NanoGPTUsageTokenInfo | null;
	weeklyInputTokens: NanoGPTUsageTokenInfo | null;
	state: string;
	graceUntil: string | null;
}
export interface DeepSeekBalanceInfo {
	currency: "CNY" | "USD";
	total_balance: string;
	granted_balance: string;
	topped_up_balance: string;
}
export interface DeepSeekBalance {
	is_available: boolean;
	balance_infos: DeepSeekBalanceInfo[];
}
export interface OpenRouterBalance {
	label: string;
	limit: number | null;
	limit_reset: string;
	limit_remaining: number | null;
	usage: number;
	usage_daily: number;
	usage_weekly: number;
	usage_monthly: number;
	credits_total: number;
	credits_used: number;
	credits_remaining: number;
	is_free_tier: boolean;
}
export interface OllamaCloudAccount {
	id: string;
	email: string;
	name: string;
	plan: string;
	customer_id: { string: string; valid: boolean };
	subscription_id: { string: string; valid: boolean };
	subscription_period_start: { time: string; valid: boolean };
	subscription_period_end: { time: string; valid: boolean };
	suspended_at: { time: string; valid: boolean };
}
export interface ZAICodingQuotaUsageDetail {
	modelCode: string;
	usage: number;
}
export interface ZAICodingQuotaLimit {
	type: string;
	unit: number;
	number: number;
	usage: number;
	currentValue: number;
	remaining: number;
	percentage: number;
	nextResetTime: number;
	usageDetails?: ZAICodingQuotaUsageDetail[];
}
export interface ZAICodingQuotaData {
	limits: ZAICodingQuotaLimit[];
	level: string;
}
export interface ZAICodingQuotaResponse {
	code: number;
	msg: string;
	data: ZAICodingQuotaData;
	success: boolean;
}
export interface NeuralWattQuotaBalance {
	/** Absent when the provider omitted the balance; never coerce to 0. */
	credits_remaining_usd?: number | null;
	total_credits_usd: number;
	credits_used_usd: number;
	accounting_method: string;
}
export interface NeuralWattQuotaUsagePeriod {
	cost_usd: number;
	requests: number;
	tokens: number;
	energy_kwh: number;
}
export interface NeuralWattQuotaUsage {
	lifetime: NeuralWattQuotaUsagePeriod;
	current_month: NeuralWattQuotaUsagePeriod;
}
export interface NeuralWattQuotaLimits {
	overage_limit_usd: number | null;
	rate_limit_tier: string;
}
export interface NeuralWattQuotaSubscription {
	plan: string;
	status: string;
	billing_interval: string;
	current_period_start: string;
	current_period_end: string;
	auto_renew: boolean;
	kwh_included: number;
	kwh_used: number;
	kwh_remaining: number;
	in_overage: boolean;
}
export interface NeuralWattQuotaKey {
	name: string;
	allowance: number | null;
}
export interface NeuralWattQuotaResponse {
	snapshot_at: string;
	balance: NeuralWattQuotaBalance;
	usage: NeuralWattQuotaUsage;
	limits: NeuralWattQuotaLimits;
	subscription: NeuralWattQuotaSubscription;
	key: NeuralWattQuotaKey;
}
