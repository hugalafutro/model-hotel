import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Gauge } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";

interface RateLimitSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function RateLimitSettings({
	collapsed,
	onToggle,
}: RateLimitSettingsProps) {
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

	const rateLimitEnabled = settings?.rate_limit_enabled !== "false";
	const rateLimitRPS = settings?.rate_limit_rps || "10";
	const rateLimitBurst = settings?.rate_limit_burst || "20";
	const rateLimitIpEnabled = settings?.rate_limit_ip_enabled !== "false";
	const rateLimitIpRPS = settings?.rate_limit_ip_rps || "30";
	const rateLimitIpBurst = settings?.rate_limit_ip_burst || "60";
	const rateLimitMaxWaitMs = settings?.rate_limit_max_wait_ms || "200";

	const RATE_LIMIT_RPS_OPTIONS = [
		{ value: "5", label: t("settings.rateLimit.rps.5") },
		{ value: "10", label: t("settings.rateLimit.rps.10") },
		{ value: "20", label: t("settings.rateLimit.rps.20") },
		{ value: "50", label: t("settings.rateLimit.rps.50") },
		{ value: "100", label: t("settings.rateLimit.rps.100") },
		{ value: "0", label: t("settings.rateLimit.rps.unlimited") },
	];

	const RATE_LIMIT_BURST_OPTIONS = [
		{ value: "10", label: t("settings.rateLimit.burst.10") },
		{ value: "20", label: t("settings.rateLimit.burst.20") },
		{ value: "50", label: t("settings.rateLimit.burst.50") },
		{ value: "100", label: t("settings.rateLimit.burst.100") },
		{ value: "200", label: t("settings.rateLimit.burst.200") },
	];

	return (
		<SettingsSection
			icon={Gauge}
			title={t("settings.rateLimit.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.rateLimit.description")}
				</p>
				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							{t("settings.rateLimit.enable")}
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							{t("settings.rateLimit.enableDescription")}
						</p>
					</div>
					<Toggle
						checked={rateLimitEnabled}
						onChange={(v) =>
							updateMutation.mutate({
								rate_limit_enabled: v ? "true" : "false",
							})
						}
					/>
				</div>

				{rateLimitEnabled && (
					<>
						<SettingsSelect
							id="rate-limit-rps"
							label={t("settings.rateLimit.requestsPerSecond")}
							value={rateLimitRPS}
							options={RATE_LIMIT_RPS_OPTIONS}
							onChange={(v) => updateMutation.mutate({ rate_limit_rps: v })}
							description={t(
								"settings.rateLimit.requestsPerSecond.description",
							)}
						/>

						<SettingsSelect
							id="rate-limit-burst"
							label={t("settings.rateLimit.burstSize")}
							value={rateLimitBurst}
							options={RATE_LIMIT_BURST_OPTIONS}
							onChange={(v) => updateMutation.mutate({ rate_limit_burst: v })}
							description={t("settings.rateLimit.burstSize.description")}
						/>
					</>
				)}

				<div className="pt-2">
					<div className="flex items-center justify-between">
						<div>
							<p className="text-sm font-medium text-gray-300">
								{t("settings.rateLimit.ipRateLimiting")}
							</p>
							<p className="text-gray-500 text-xs mt-0.5">
								{t("settings.rateLimit.ipRateLimitingDescription")}
							</p>
						</div>
						<Toggle
							checked={rateLimitIpEnabled}
							onChange={(v) =>
								updateMutation.mutate({
									rate_limit_ip_enabled: v ? "true" : "false",
								})
							}
						/>
					</div>

					{rateLimitIpEnabled && (
						<>
							<div className="mt-4">
								<SettingsSelect
									id="rate-limit-ip-rps"
									label={t("settings.rateLimit.ipRequestsPerSecond")}
									value={rateLimitIpRPS}
									options={RATE_LIMIT_RPS_OPTIONS}
									onChange={(v) =>
										updateMutation.mutate({ rate_limit_ip_rps: v })
									}
									description={t(
										"settings.rateLimit.ipRequestsPerSecond.description",
									)}
								/>
							</div>

							<div className="mt-4">
								<SettingsSelect
									id="rate-limit-ip-burst"
									label={t("settings.rateLimit.ipBurstSize")}
									value={rateLimitIpBurst}
									options={RATE_LIMIT_BURST_OPTIONS}
									onChange={(v) =>
										updateMutation.mutate({ rate_limit_ip_burst: v })
									}
									description={t("settings.rateLimit.ipBurstSize.description")}
								/>
							</div>
						</>
					)}
				</div>

				{(rateLimitEnabled || rateLimitIpEnabled) && (
					<div className="pt-2">
						<p className="text-sm font-medium text-gray-300 mb-1">
							{t("settings.rateLimit.backpressure")}
						</p>
						<p className="text-gray-500 text-xs mb-3">
							{t("settings.rateLimit.backpressureDescription")}
						</p>
						<div>
							<label
								htmlFor="rate-limit-max-wait"
								className="block text-sm font-medium text-gray-300 mb-2"
							>
								{t("settings.rateLimit.maxWait")}
							</label>
							<input
								id="rate-limit-max-wait"
								type="number"
								min="0"
								max="10000"
								value={rateLimitMaxWaitMs}
								onChange={(e) =>
									updateMutation.mutate({
										rate_limit_max_wait_ms: e.target.value,
									})
								}
								className="ui-input"
							/>
							<p className="text-gray-500 text-xs mt-1">
								{t("settings.rateLimit.maxWait.description")}
							</p>
						</div>
					</div>
				)}
			</div>
		</SettingsSection>
	);
}
