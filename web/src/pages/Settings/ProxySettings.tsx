import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Timer } from "lucide-react";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { useToast } from "../../context/ToastContext";

const REQUEST_TIMEOUT_OPTIONS = [
	{ value: "30s", label: "30 seconds" },
	{ value: "1m0s", label: "1 minute (default)" },
	{ value: "2m0s", label: "2 minutes" },
	{ value: "5m0s", label: "5 minutes" },
	{ value: "10m0s", label: "10 minutes" },
];

const KEY_CACHE_TTL_OPTIONS = [
	{ value: "1m0s", label: "1 minute" },
	{ value: "5m0s", label: "5 minutes" },
	{ value: "10m0s", label: "10 minutes (default)" },
	{ value: "30m0s", label: "30 minutes" },
	{ value: "1h0m0s", label: "1 hour" },
];

const TTFT_TIMEOUT_OPTIONS = [
	{ value: "15s", label: "15 seconds" },
	{ value: "30s", label: "30 seconds" },
	{ value: "1m0s", label: "1 minute (default)" },
	{ value: "2m0s", label: "2 minutes" },
	{ value: "5m0s", label: "5 minutes" },
	{ value: "0s", label: "Disabled" },
];

const STREAM_STALL_TIMEOUT_OPTIONS = [
	{ value: "10s", label: "10 seconds" },
	{ value: "30s", label: "30 seconds (default)" },
	{ value: "1m0s", label: "1 minute" },
	{ value: "2m0s", label: "2 minutes" },
	{ value: "5m0s", label: "5 minutes" },
	{ value: "10m0s", label: "10 minutes" },
	{ value: "0s", label: "Disabled" },
];

interface ProxySettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function ProxySettings({ collapsed, onToggle }: ProxySettingsProps) {
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
			toast("Settings saved", "success");
		},
		onError: (err: Error) => {
			toast(`Failed to save: ${err.message}`, "error");
		},
	});

	const requestTimeout = settings?.request_timeout || "1m0s";
	const keyCacheTTL = settings?.key_cache_ttl || "10m0s";
	const ttftTimeout = settings?.ttft_timeout || "1m0s";
	const streamStallTimeout = settings?.stream_stall_timeout || "30s";

	return (
		<SettingsSection
			icon={Timer}
			title="Proxy"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					Configure proxy request behavior and timeouts.
				</p>
				<SettingsSelect
					id="request-timeout"
					label="Request Timeout"
					value={requestTimeout}
					options={REQUEST_TIMEOUT_OPTIONS}
					onChange={(v) => updateMutation.mutate({ request_timeout: v })}
					description="Maximum time for non-streaming requests before timing out. Streaming requests automatically get 10× this duration to accommodate thinking/reasoning models."
				/>
				<SettingsSelect
					id="key-cache-ttl"
					label="Key Cache TTL"
					value={keyCacheTTL}
					options={KEY_CACHE_TTL_OPTIONS}
					onChange={(v) => updateMutation.mutate({ key_cache_ttl: v })}
					description="How long decrypted provider API keys are cached. Higher values reduce latency on the first request after cache expiry (Argon2id key derivation is ~12ms)."
				/>
				<SettingsSelect
					id="ttft-timeout"
					label="TTFT Timeout"
					value={ttftTimeout}
					options={TTFT_TIMEOUT_OPTIONS}
					onChange={(v) => updateMutation.mutate({ ttft_timeout: v })}
					description="Maximum time to wait for the first streaming token before failing over to the next provider. Disabled means immediate commit (no failover on slow first token)."
				/>
				<SettingsSelect
					id="stream-stall-timeout"
					label="Stream Stall Timeout"
					value={streamStallTimeout}
					options={STREAM_STALL_TIMEOUT_OPTIONS}
					onChange={(v) => updateMutation.mutate({ stream_stall_timeout: v })}
					description="Maximum silence during streaming before the connection is cut. After 50 chunks, the timeout is automatically extended 3× to tolerate tool calls and long reasoning."
				/>
			</div>
		</SettingsSection>
	);
}
