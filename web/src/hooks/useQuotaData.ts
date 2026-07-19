import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type {
	DeepSeekBalance,
	KimiCodeQuotaResponse,
	KimiCodeQuotaWindow,
	NanoGPTUsage,
	NeuralWattQuotaResponse,
	OllamaCloudAccount,
	OpenRouterBalance,
	Provider,
	ZAICodingQuotaLimit,
	ZAICodingQuotaResponse,
} from "../api/types";

// ── Cache helpers (shared across consumers) ──────────────────────────────

const CACHE_PREFIX = "model-hotel";

/** Subscription plans that don't qualify for quota badge display (lowest/free tier). */
const NEURALWATT_BADGE_EXCLUDED_PLANS = new Set(["free", "starter"]);

export function getCachedData<T>(key: string): T | undefined {
	try {
		const raw = localStorage.getItem(`${CACHE_PREFIX}:${key}`);
		if (raw) return JSON.parse(raw) as T;
	} catch {
		/* ignore */
	}
	return undefined;
}

export function setCachedData<T>(key: string, data: T) {
	try {
		localStorage.setItem(`${CACHE_PREFIX}:${key}`, JSON.stringify(data));
	} catch {
		/* ignore */
	}
}

// ── Provider type detection ──────────────────────────────────────────────

export type QuotaProviderType =
	| "nanogpt"
	| "zai-coding"
	| "kimi-code"
	| "deepseek"
	| "openrouter"
	| "ollama-cloud"
	| "neuralwatt";

function hostnameMatches(url: string, suffix: string, exact?: string): boolean {
	try {
		const h = new URL(url).hostname;
		return exact ? h === exact || h.endsWith(suffix) : h.endsWith(suffix);
	} catch {
		return false;
	}
}

/** Detect which quota-supporting provider type a base URL belongs to. */
export function detectQuotaProviderType(
	baseUrl: string,
): QuotaProviderType | null {
	if (hostnameMatches(baseUrl, "nano-gpt.com")) return "nanogpt";
	if (hostnameMatches(baseUrl, ".z.ai", "z.ai")) return "zai-coding";
	if (hostnameMatches(baseUrl, ".kimi.com", "kimi.com")) return "kimi-code";
	if (hostnameMatches(baseUrl, "deepseek.com")) return "deepseek";
	if (hostnameMatches(baseUrl, "openrouter.ai")) return "openrouter";
	if (hostnameMatches(baseUrl, "ollama.com")) return "ollama-cloud";
	if (hostnameMatches(baseUrl, "neuralwatt.com")) return "neuralwatt";
	return null;
}

/** Find the first provider ID matching a quota provider type. */
function findProviderId(
	providers: Provider[] | undefined,
	type: QuotaProviderType,
): string | undefined {
	return providers?.find((p) => detectQuotaProviderType(p.base_url) === type)
		?.id;
}

// ── Z.ai Coding limit helpers ───────────────────────────────────────────

export function getZaiCodingFiveHourLimit(
	data: ZAICodingQuotaResponse | undefined | null,
): ZAICodingQuotaLimit | undefined {
	return data?.data?.limits?.find(
		(l) => l.type === "TOKENS_LIMIT" && l.unit === 3,
	);
}

export function getZaiCodingWeeklyLimit(
	data: ZAICodingQuotaResponse | undefined | null,
): ZAICodingQuotaLimit | undefined {
	return data?.data?.limits?.find(
		(l) => l.type === "TOKENS_LIMIT" && l.unit === 6,
	);
}

// ── Kimi Code limit helpers ─────────────────────────────────────────────
// Kimi encodes limit/remaining as JSON strings; parse with Number() before
// computing percentage. Percentage used = (limit - remaining) / limit * 100.

function toKimiCodeWindow(
	limitStr: string | undefined,
	remainingStr: string | undefined,
	resetTime: string | undefined,
): KimiCodeQuotaWindow | undefined {
	if (limitStr == null || remainingStr == null) return undefined;
	const limit = Number(limitStr);
	const remaining = Number(remainingStr);
	if (!Number.isFinite(limit) || !Number.isFinite(remaining)) return undefined;
	const percentage = limit > 0 ? ((limit - remaining) / limit) * 100 : 0;
	return { limit, remaining, resetTime: resetTime ?? "", percentage };
}

/** The rolling 300-minute (5-hour) window. */
export function getKimiCodeFiveHourLimit(
	data: KimiCodeQuotaResponse | undefined | null,
): KimiCodeQuotaWindow | undefined {
	const entry = data?.limits?.find(
		(l) =>
			l.window?.timeUnit === "TIME_UNIT_MINUTE" && l.window?.duration === 300,
	);
	if (!entry) return undefined;
	return toKimiCodeWindow(
		entry.detail?.limit,
		entry.detail?.remaining,
		entry.detail?.resetTime,
	);
}

/** The weekly window (top-level `usage`). */
export function getKimiCodeWeeklyLimit(
	data: KimiCodeQuotaResponse | undefined | null,
): KimiCodeQuotaWindow | undefined {
	const usage = data?.usage;
	if (!usage) return undefined;
	return toKimiCodeWindow(usage.limit, usage.remaining, usage.resetTime);
}

// ── Hook options ─────────────────────────────────────────────────────────

export interface UseQuotaDataOptions {
	/** Optional auto-refresh interval in ms. false = disabled. */
	refetchInterval?: number | false;
	/** Whether the hook is conceptually "collapsed" (disables auto-refresh). */
	collapsed?: boolean;
	/** Toast errors to user (requires a toast fn). If omitted, errors are silent. */
	toastErrors?: (msg: string, severity: "warning") => void;
}

// ── Return type ──────────────────────────────────────────────────────────

export interface QuotaDataResult {
	/** Per-provider IDs (undefined if no such provider exists). */
	nanogptProviderId: string | undefined;
	zaiCodingProviderId: string | undefined;
	kimiCodeProviderId: string | undefined;
	deepseekProviderId: string | undefined;
	openrouterProviderId: string | undefined;
	ollamaCloudProviderId: string | undefined;
	neuralwattProviderId: string | undefined;

	/** Raw query data. */
	nanogptUsage: NanoGPTUsage | undefined;
	zaiCodingUsage: ZAICodingQuotaResponse | undefined;
	kimiCodeUsage: KimiCodeQuotaResponse | undefined;
	deepseekBalance: DeepSeekBalance | undefined;
	openrouterBalance: OpenRouterBalance | undefined;
	ollamaCloudAccount: OllamaCloudAccount | undefined;
	neuralwattQuota: NeuralWattQuotaResponse | null | undefined;

	/** Derived Z.ai limits. */
	zaiCodingFiveHour: ZAICodingQuotaLimit | undefined;
	zaiCodingWeekly: ZAICodingQuotaLimit | undefined;

	/** Derived Kimi Code limits. */
	kimiCodeFiveHour: KimiCodeQuotaWindow | undefined;
	kimiCodeWeekly: KimiCodeQuotaWindow | undefined;

	/** NanoGPT weekly helpers. */
	nanoWeeklyUsed: number | null | undefined;
	nanoWeeklyLimit: number | null | undefined;

	/** Badge visibility booleans (already account for providerId + data). */
	showNanoBadge: boolean;
	showZaiCodingBadge: boolean;
	showKimiCodeBadge: boolean;
	showDsBadge: boolean;
	showOrBadge: boolean;
	showOllamaCloudBadge: boolean;
	showNeuralwattBadge: boolean;

	/** Whether any quota-supporting provider exists. */
	hasAnyProvider: boolean;

	/** Individual refetch fns. */
	refetchNano: () => Promise<void>;
	refetchZaiCoding: () => Promise<void>;
	refetchKimiCode: () => Promise<void>;
	refetchDeepseek: () => Promise<void>;
	refetchOpenRouter: () => Promise<void>;
	refetchOllamaCloud: () => Promise<void>;
	refetchNeuralwatt: () => Promise<void>;

	/** Individual isRefetching flags. */
	isNanoRefetching: boolean;
	isZaiCodingRefetching: boolean;
	isKimiCodeRefetching: boolean;
	isDsRefetching: boolean;
	isOrRefetching: boolean;
	isOllamaCloudRefetching: boolean;
	isNeuralwattRefetching: boolean;

	/** dataUpdatedAt for modals. */
	openrouterDataUpdatedAt: number;
	nanogptDataUpdatedAt: number;
	zaiCodingDataUpdatedAt: number;
	kimiCodeDataUpdatedAt: number;
	deepseekDataUpdatedAt: number;
	ollamaCloudDataUpdatedAt: number;
	neuralwattDataUpdatedAt: number;

	/** Invalidate all quota query keys. */
	invalidateAll: () => void;
}

// ── Hook ─────────────────────────────────────────────────────────────────

export function useQuotaData(
	providers: Provider[] | undefined,
	options: UseQuotaDataOptions = {},
): QuotaDataResult {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const { refetchInterval, collapsed, toastErrors } = options;

	// ── Provider detection ──
	const nanogptProviderId = useMemo(
		() => findProviderId(providers, "nanogpt"),
		[providers],
	);
	const zaiCodingProviderId = useMemo(
		() => findProviderId(providers, "zai-coding"),
		[providers],
	);
	const kimiCodeProviderId = useMemo(
		() => findProviderId(providers, "kimi-code"),
		[providers],
	);
	const deepseekProviderId = useMemo(
		() => findProviderId(providers, "deepseek"),
		[providers],
	);
	const openrouterProviderId = useMemo(
		() => findProviderId(providers, "openrouter"),
		[providers],
	);
	const ollamaCloudProviderId = useMemo(
		() => findProviderId(providers, "ollama-cloud"),
		[providers],
	);
	const neuralwattProviderId = useMemo(
		() => findProviderId(providers, "neuralwatt"),
		[providers],
	);

	// Derive effective refetch interval: disabled when collapsed or explicit false
	const effectiveRefetchInterval =
		collapsed === true
			? false
			: refetchInterval === false
				? false
				: refetchInterval;

	// ── NanoGPT query ──
	const {
		data: nanogptUsage,
		dataUpdatedAt: nanogptDataUpdatedAt,
		isRefetching: isNanoRefetching,
		isError: isNanoGPTError,
		refetch: refetchNanoRaw,
	} = useQuery({
		queryKey: ["nanogpt-usage", nanogptProviderId],
		queryFn: () =>
			api.providers.getUsage(
				nanogptProviderId as string,
			) as Promise<NanoGPTUsage>,
		enabled: Boolean(nanogptProviderId),
		refetchInterval: effectiveRefetchInterval,
		initialData: () => getCachedData<NanoGPTUsage>("nanogpt-usage"),
	});

	// Cache writes
	useEffect(() => {
		if (nanogptUsage) setCachedData("nanogpt-usage", nanogptUsage);
	}, [nanogptUsage]);

	// ── Z.ai Coding query ──
	const {
		data: zaiCodingUsage,
		dataUpdatedAt: zaiCodingDataUpdatedAt,
		isRefetching: isZaiCodingRefetching,
		isError: isZAICodingError,
		refetch: refetchZaiRaw,
	} = useQuery({
		queryKey: ["zai-coding-usage", zaiCodingProviderId],
		queryFn: () =>
			api.providers.getUsage(
				zaiCodingProviderId as string,
			) as Promise<ZAICodingQuotaResponse>,
		enabled: Boolean(zaiCodingProviderId),
		refetchInterval: effectiveRefetchInterval,
		initialData: () =>
			getCachedData<ZAICodingQuotaResponse>("zai-coding-usage"),
	});

	useEffect(() => {
		if (zaiCodingUsage) setCachedData("zai-coding-usage", zaiCodingUsage);
	}, [zaiCodingUsage]);

	// ── Kimi Code query ──
	const {
		data: kimiCodeUsage,
		dataUpdatedAt: kimiCodeDataUpdatedAt,
		isRefetching: isKimiCodeRefetching,
		isError: isKimiCodeError,
		refetch: refetchKimiRaw,
	} = useQuery({
		queryKey: ["kimi-code-usage", kimiCodeProviderId],
		queryFn: () =>
			api.providers.getUsage(
				kimiCodeProviderId as string,
			) as Promise<KimiCodeQuotaResponse>,
		enabled: Boolean(kimiCodeProviderId),
		refetchInterval: effectiveRefetchInterval,
		initialData: () => getCachedData<KimiCodeQuotaResponse>("kimi-code-usage"),
	});

	useEffect(() => {
		if (kimiCodeUsage) setCachedData("kimi-code-usage", kimiCodeUsage);
	}, [kimiCodeUsage]);

	// ── DeepSeek query ──
	const {
		data: deepseekBalance,
		dataUpdatedAt: deepseekDataUpdatedAt,
		isRefetching: isDsRefetching,
		isError: isDeepseekError,
		refetch: refetchDsRaw,
	} = useQuery({
		queryKey: ["deepseek-balance", deepseekProviderId],
		queryFn: () => api.providers.getBalance(deepseekProviderId as string),
		enabled: Boolean(deepseekProviderId),
		refetchInterval: effectiveRefetchInterval,
		initialData: () => getCachedData<DeepSeekBalance>("deepseek-balance"),
	});

	useEffect(() => {
		if (deepseekBalance) setCachedData("deepseek-balance", deepseekBalance);
	}, [deepseekBalance]);

	// ── OpenRouter query ──
	const {
		data: openrouterBalance,
		dataUpdatedAt: openrouterDataUpdatedAt,
		isRefetching: isOrRefetching,
		isError: isOpenRouterError,
		refetch: refetchOrRaw,
	} = useQuery<OpenRouterBalance>({
		queryKey: ["openrouter-balance", openrouterProviderId],
		queryFn: () =>
			api.providers.getOpenRouterBalance(openrouterProviderId as string),
		enabled: Boolean(openrouterProviderId),
		refetchInterval: effectiveRefetchInterval,
		initialData: () => getCachedData<OpenRouterBalance>("openrouter-balance"),
	});

	useEffect(() => {
		if (openrouterBalance !== undefined)
			setCachedData("openrouter-balance", openrouterBalance);
	}, [openrouterBalance]);

	// ── Ollama Cloud query ──
	const {
		data: ollamaCloudAccount,
		dataUpdatedAt: ollamaCloudDataUpdatedAt,
		isRefetching: isOllamaCloudRefetching,
		isError: isOllamaCloudError,
		refetch: refetchOcRaw,
	} = useQuery<OllamaCloudAccount>({
		queryKey: ["ollama-cloud-account", ollamaCloudProviderId],
		queryFn: () =>
			api.providers.getOllamaCloudAccount(ollamaCloudProviderId as string),
		enabled: Boolean(ollamaCloudProviderId),
		refetchInterval: effectiveRefetchInterval,
		initialData: () =>
			getCachedData<OllamaCloudAccount>("ollama-cloud-account"),
	});

	useEffect(() => {
		if (ollamaCloudAccount)
			setCachedData("ollama-cloud-account", ollamaCloudAccount);
	}, [ollamaCloudAccount]);

	// ── NeuralWatt query ──
	const {
		data: neuralwattQuota,
		dataUpdatedAt: neuralwattDataUpdatedAt,
		isRefetching: isNeuralwattRefetching,
		isError: isNeuralwattError,
		refetch: refetchNwRaw,
	} = useQuery<NeuralWattQuotaResponse | null>({
		queryKey: ["neuralwatt-quota", neuralwattProviderId],
		queryFn: () =>
			api.providers.getNeuralWattQuota(neuralwattProviderId as string),
		enabled: Boolean(neuralwattProviderId),
		refetchInterval: effectiveRefetchInterval,
		initialData: () =>
			getCachedData<NeuralWattQuotaResponse>("neuralwatt-quota"),
	});

	useEffect(() => {
		if (neuralwattQuota) setCachedData("neuralwatt-quota", neuralwattQuota);
	}, [neuralwattQuota]);

	// ── Error toasting ──
	const nanoErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isNanoGPTError && !nanoErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.nanoGPTError"), "warning");
			nanoErrorToasted.current = true;
		}
		if (!isNanoGPTError) nanoErrorToasted.current = false;
	}, [isNanoGPTError, toastErrors, t]);

	const zaiErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isZAICodingError && !zaiErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.zaiError"), "warning");
			zaiErrorToasted.current = true;
		}
		if (!isZAICodingError) zaiErrorToasted.current = false;
	}, [isZAICodingError, toastErrors, t]);

	const kimiErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isKimiCodeError && !kimiErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.kimiError"), "warning");
			kimiErrorToasted.current = true;
		}
		if (!isKimiCodeError) kimiErrorToasted.current = false;
	}, [isKimiCodeError, toastErrors, t]);

	const dsErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isDeepseekError && !dsErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.deepSeekError"), "warning");
			dsErrorToasted.current = true;
		}
		if (!isDeepseekError) dsErrorToasted.current = false;
	}, [isDeepseekError, toastErrors, t]);

	const orErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isOpenRouterError && !orErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.openRouterError"), "warning");
			orErrorToasted.current = true;
		}
		if (!isOpenRouterError) orErrorToasted.current = false;
	}, [isOpenRouterError, toastErrors, t]);

	const ocErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isOllamaCloudError && !ocErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.ollamaCloudError"), "warning");
			ocErrorToasted.current = true;
		}
		if (!isOllamaCloudError) ocErrorToasted.current = false;
	}, [isOllamaCloudError, toastErrors, t]);

	const nwErrorToasted = useRef(false);
	useEffect(() => {
		if (!toastErrors) return;
		if (isNeuralwattError && !nwErrorToasted.current) {
			toastErrors(t("hooks.useQuotaData.neuralwattError"), "warning");
			nwErrorToasted.current = true;
		}
		if (!isNeuralwattError) nwErrorToasted.current = false;
	}, [isNeuralwattError, toastErrors, t]);

	// ── Derived values ──
	const zaiCodingFiveHour = getZaiCodingFiveHourLimit(zaiCodingUsage);
	const zaiCodingWeekly = getZaiCodingWeeklyLimit(zaiCodingUsage);

	const kimiCodeFiveHour = getKimiCodeFiveHourLimit(kimiCodeUsage);
	const kimiCodeWeekly = getKimiCodeWeeklyLimit(kimiCodeUsage);

	const nanoWeeklyUsed = nanogptUsage?.weeklyInputTokens?.used;
	const nanoWeeklyLimit = nanogptUsage?.limits?.weeklyInputTokens;

	const isNanoCancelled =
		nanogptUsage?.providerStatus === "canceled" ||
		nanogptUsage?.providerStatus === "cancelled";

	const showNanoBadge =
		Boolean(nanogptProviderId) &&
		Boolean(nanogptUsage) &&
		!isNanoCancelled &&
		nanoWeeklyUsed != null &&
		Boolean(nanoWeeklyLimit);

	const showZaiCodingBadge =
		Boolean(zaiCodingProviderId) &&
		Boolean(zaiCodingUsage?.success) &&
		Boolean(zaiCodingFiveHour || zaiCodingWeekly);

	const showKimiCodeBadge =
		Boolean(kimiCodeProviderId) &&
		Boolean(kimiCodeUsage) &&
		Boolean(kimiCodeFiveHour || kimiCodeWeekly);

	const showDsBadge =
		Boolean(deepseekProviderId) &&
		Boolean(deepseekBalance) &&
		Boolean(deepseekBalance?.is_available);

	const showOrBadge =
		Boolean(openrouterProviderId) &&
		Boolean(openrouterBalance) &&
		openrouterBalance?.credits_remaining != null;

	const isOllamaCloudSuspended =
		ollamaCloudAccount?.suspended_at?.valid === true;

	const showOllamaCloudBadge =
		Boolean(ollamaCloudProviderId) &&
		Boolean(ollamaCloudAccount) &&
		!isOllamaCloudSuspended;

	const showNeuralwattBadge =
		Boolean(neuralwattProviderId) &&
		Boolean(neuralwattQuota) &&
		neuralwattQuota?.balance?.credits_remaining_usd != null &&
		!NEURALWATT_BADGE_EXCLUDED_PLANS.has(
			neuralwattQuota?.subscription?.plan?.toLowerCase() ?? "",
		);

	const hasAnyProvider = Boolean(
		nanogptProviderId ||
			zaiCodingProviderId ||
			kimiCodeProviderId ||
			deepseekProviderId ||
			openrouterProviderId ||
			ollamaCloudProviderId ||
			neuralwattProviderId,
	);

	// ── Refetch helpers ──
	const refetchNano = useCallback(async () => {
		await refetchNanoRaw();
	}, [refetchNanoRaw]);

	const refetchZaiCoding = useCallback(async () => {
		await refetchZaiRaw();
	}, [refetchZaiRaw]);

	const refetchKimiCode = useCallback(async () => {
		await refetchKimiRaw();
	}, [refetchKimiRaw]);

	const refetchDeepseek = useCallback(async () => {
		await refetchDsRaw();
	}, [refetchDsRaw]);

	const refetchOpenRouter = useCallback(async () => {
		await refetchOrRaw();
	}, [refetchOrRaw]);

	const refetchOllamaCloud = useCallback(async () => {
		await refetchOcRaw();
	}, [refetchOcRaw]);

	const refetchNeuralwatt = useCallback(async () => {
		await refetchNwRaw();
	}, [refetchNwRaw]);

	const invalidateAll = useCallback(() => {
		queryClient.invalidateQueries({ queryKey: ["nanogpt-usage"] });
		queryClient.invalidateQueries({ queryKey: ["zai-coding-usage"] });
		queryClient.invalidateQueries({ queryKey: ["kimi-code-usage"] });
		queryClient.invalidateQueries({ queryKey: ["deepseek-balance"] });
		queryClient.invalidateQueries({ queryKey: ["openrouter-balance"] });
		queryClient.invalidateQueries({ queryKey: ["ollama-cloud-account"] });
		queryClient.invalidateQueries({ queryKey: ["neuralwatt-quota"] });
	}, [queryClient]);

	return {
		nanogptProviderId,
		zaiCodingProviderId,
		kimiCodeProviderId,
		deepseekProviderId,
		openrouterProviderId,
		ollamaCloudProviderId,
		neuralwattProviderId,
		nanogptUsage,
		zaiCodingUsage,
		kimiCodeUsage,
		deepseekBalance,
		openrouterBalance,
		ollamaCloudAccount,
		neuralwattQuota,
		zaiCodingFiveHour,
		zaiCodingWeekly,
		kimiCodeFiveHour,
		kimiCodeWeekly,
		nanoWeeklyUsed,
		nanoWeeklyLimit,
		showNanoBadge,
		showZaiCodingBadge,
		showKimiCodeBadge,
		showDsBadge,
		showOrBadge,
		showOllamaCloudBadge,
		showNeuralwattBadge,
		hasAnyProvider,
		refetchNano,
		refetchZaiCoding,
		refetchKimiCode,
		refetchDeepseek,
		refetchOpenRouter,
		refetchOllamaCloud,
		refetchNeuralwatt,
		isNanoRefetching,
		isZaiCodingRefetching,
		isKimiCodeRefetching,
		isDsRefetching,
		isOrRefetching,
		isOllamaCloudRefetching,
		isNeuralwattRefetching,
		nanogptDataUpdatedAt,
		zaiCodingDataUpdatedAt,
		kimiCodeDataUpdatedAt,
		deepseekDataUpdatedAt,
		openrouterDataUpdatedAt,
		ollamaCloudDataUpdatedAt,
		neuralwattDataUpdatedAt,
		invalidateAll,
	};
}
