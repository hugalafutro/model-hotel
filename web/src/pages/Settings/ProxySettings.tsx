import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Timer } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { useToast } from "../../context/ToastContext";

interface ProxySettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function ProxySettings({ collapsed, onToggle }: ProxySettingsProps) {
	const { t } = useTranslation();
	const REQUEST_TIMEOUT_OPTIONS = [
		{ value: "30s", label: t("settings.proxy.timeout.30s") },
		{ value: "1m0s", label: t("settings.proxy.timeout.1m0s") },
		{ value: "2m0s", label: t("settings.proxy.timeout.2m0s") },
		{ value: "5m0s", label: t("settings.proxy.timeout.5m0s") },
		{ value: "10m0s", label: t("settings.proxy.timeout.10m0s") },
	];

	const KEY_CACHE_TTL_OPTIONS = [
		{ value: "1m0s", label: t("settings.proxy.keyCache.1m0s") },
		{ value: "5m0s", label: t("settings.proxy.keyCache.5m0s") },
		{ value: "10m0s", label: t("settings.proxy.keyCache.10m0s") },
		{ value: "30m0s", label: t("settings.proxy.keyCache.30m0s") },
		{ value: "1h0m0s", label: t("settings.proxy.keyCache.1h0m0s") },
	];

	const TTFT_TIMEOUT_OPTIONS = [
		{ value: "15s", label: t("settings.proxy.ttft.15s") },
		{ value: "30s", label: t("settings.proxy.ttft.30s") },
		{ value: "1m0s", label: t("settings.proxy.ttft.1m0s") },
		{ value: "2m0s", label: t("settings.proxy.ttft.2m0s") },
		{ value: "5m0s", label: t("settings.proxy.ttft.5m0s") },
		{ value: "0s", label: t("settings.proxy.ttft.disabled") },
	];

	const STREAM_STALL_TIMEOUT_OPTIONS = [
		{ value: "10s", label: t("settings.proxy.streamStall.10s") },
		{ value: "30s", label: t("settings.proxy.streamStall.30s") },
		{ value: "1m0s", label: t("settings.proxy.streamStall.1m0s") },
		{ value: "2m0s", label: t("settings.proxy.streamStall.2m0s") },
		{ value: "5m0s", label: t("settings.proxy.streamStall.5m0s") },
		{ value: "10m0s", label: t("settings.proxy.streamStall.10m0s") },
		{ value: "0s", label: t("settings.proxy.streamStall.disabled") },
	];
	const { toast } = useToast();
	const queryClient = useQueryClient();

	const { data: settings } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	const updateMutation = useMutation({
		mutationFn: (updates: Record<string, string>) =>
			api.settings.update(updates),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
			toast(t("settings.common.settingsSaved"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.failedToSave", { message: err.message }),
				"error",
			);
		},
	});

	const requestTimeout = settings?.request_timeout || "1m0s";
	const keyCacheTTL = settings?.key_cache_ttl || "10m0s";
	const ttftTimeout = settings?.ttft_timeout || "1m0s";
	const streamStallTimeout = settings?.stream_stall_timeout || "30s";

	return (
		<SettingsSection
			icon={Timer}
			title={t("settings.proxy.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.proxy.description")}
				</p>
				<SettingsSelect
					id="request-timeout"
					label={t("settings.proxy.requestTimeout")}
					value={requestTimeout}
					options={REQUEST_TIMEOUT_OPTIONS}
					onChange={(v) => updateMutation.mutate({ request_timeout: v })}
					description={t("settings.proxy.requestTimeout.description")}
				/>
				<SettingsSelect
					id="key-cache-ttl"
					label={t("settings.proxy.keyCacheTtl")}
					value={keyCacheTTL}
					options={KEY_CACHE_TTL_OPTIONS}
					onChange={(v) => updateMutation.mutate({ key_cache_ttl: v })}
					description={t("settings.proxy.keyCacheTtl.description")}
				/>
				<SettingsSelect
					id="ttft-timeout"
					label={t("settings.proxy.ttftTimeout")}
					value={ttftTimeout}
					options={TTFT_TIMEOUT_OPTIONS}
					onChange={(v) => updateMutation.mutate({ ttft_timeout: v })}
					description={t("settings.proxy.ttftTimeout.description")}
				/>
				<SettingsSelect
					id="stream-stall-timeout"
					label={t("settings.proxy.streamStallTimeout")}
					value={streamStallTimeout}
					options={STREAM_STALL_TIMEOUT_OPTIONS}
					onChange={(v) => updateMutation.mutate({ stream_stall_timeout: v })}
					description={t("settings.proxy.streamStallTimeout.description")}
				/>
			</div>
		</SettingsSection>
	);
}
