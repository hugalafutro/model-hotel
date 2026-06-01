import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Timer } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { useToast } from "../../context/ToastContext";

interface ProxySettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function ProxySettings({ collapsed, onToggle }: ProxySettingsProps) {
	const { t } = useTranslation();
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

	// Parse Go duration string to seconds
	function goDurationToSeconds(d: string): number {
		if (!d) return 0;
		let total = 0;
		const hourMatch = d.match(/(\d+)h/);
		const minMatch = d.match(/(\d+)m/);
		const secMatch = /(\d+)s/.exec(d);
		if (hourMatch) total += Number(hourMatch[1]) * 3600;
		if (minMatch) total += Number(minMatch[1]) * 60;
		if (secMatch) total += Number(secMatch[1]);
		return total;
	}

	// Convert seconds to Go duration string
	function secondsToGoDuration(s: number): string {
		if (s <= 0) return "0s";
		const h = Math.floor(s / 3600);
		const m = Math.floor((s % 3600) / 60);
		const sec = s % 60;
		let result = "";
		if (h > 0) result += `${h}h`;
		if (m > 0) result += `${m}m`;
		if (sec > 0 || result === "") result += `${sec}s`;
		return result;
	}

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
				<div className="grid grid-cols-2 gap-x-8 gap-y-5">
					<div className="space-y-5">
						<SettingsSlider
							id="request-timeout"
							label={t("settings.proxy.requestTimeout")}
							value={goDurationToSeconds(requestTimeout)}
							min={30}
							max={600}
							step={30}
							clampStep={30}
							unit="s"
							onChange={(v) =>
								updateMutation.mutate({
									request_timeout: secondsToGoDuration(v),
								})
							}
							description={t("settings.proxy.requestTimeout.description")}
						/>
						<SettingsSlider
							id="key-cache-ttl"
							label={t("settings.proxy.keyCacheTtl")}
							value={goDurationToSeconds(keyCacheTTL)}
							min={60}
							max={3600}
							step={60}
							clampStep={60}
							unit="s"
							onChange={(v) =>
								updateMutation.mutate({ key_cache_ttl: secondsToGoDuration(v) })
							}
							description={t("settings.proxy.keyCacheTtl.description")}
						/>
					</div>
					<div className="space-y-5">
						<SettingsSlider
							id="ttft-timeout"
							label={t("settings.proxy.ttftTimeout")}
							value={goDurationToSeconds(ttftTimeout)}
							min={0}
							max={300}
							step={5}
							clampStep={5}
							unit="s"
							onChange={(v) =>
								updateMutation.mutate({ ttft_timeout: secondsToGoDuration(v) })
							}
							description={t("settings.proxy.ttftTimeout.description")}
						/>
						<SettingsSlider
							id="stream-stall-timeout"
							label={t("settings.proxy.streamStallTimeout")}
							value={goDurationToSeconds(streamStallTimeout)}
							min={0}
							max={600}
							step={10}
							clampStep={10}
							unit="s"
							onChange={(v) =>
								updateMutation.mutate({
									stream_stall_timeout: secondsToGoDuration(v),
								})
							}
							description={t("settings.proxy.streamStallTimeout.description")}
						/>
					</div>
				</div>
			</div>
		</SettingsSection>
	);
}
