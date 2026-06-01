import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Gauge } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { SettingsSlider } from "../../components/SettingsSlider";
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
				<div className="grid grid-cols-2 gap-x-8 gap-y-5">
					<div className="space-y-5">
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
					</div>

					<div className="space-y-5">
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

								<SettingsSlider
									id="rate-limit-burst"
									label={t("settings.rateLimit.burstSize")}
									value={Number(rateLimitBurst)}
									min={5}
									max={200}
									step={5}
									clampStep={5}
									onChange={(v) =>
										updateMutation.mutate({
											rate_limit_burst: String(v),
										})
									}
									description={t("settings.rateLimit.burstSize.description")}
								/>
							</>
						)}

						{rateLimitIpEnabled && (
							<>
								<SettingsSelect
									id="rate-limit-ip-rps"
									label={t("settings.rateLimit.ipRequestsPerSecond")}
									value={rateLimitIpRPS}
									options={RATE_LIMIT_RPS_OPTIONS}
									onChange={(v) =>
										updateMutation.mutate({
											rate_limit_ip_rps: v,
										})
									}
									description={t(
										"settings.rateLimit.ipRequestsPerSecond.description",
									)}
								/>

								<SettingsSlider
									id="rate-limit-ip-burst"
									label={t("settings.rateLimit.ipBurstSize")}
									value={Number(rateLimitIpBurst)}
									min={5}
									max={200}
									step={5}
									clampStep={5}
									onChange={(v) =>
										updateMutation.mutate({
											rate_limit_ip_burst: String(v),
										})
									}
									description={t("settings.rateLimit.ipBurstSize.description")}
								/>
							</>
						)}
					</div>
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
							<SettingsSlider
								id="rate-limit-max-wait"
								label={t("settings.rateLimit.maxWait")}
								value={Number(rateLimitMaxWaitMs)}
								min={0}
								max={10000}
								step={100}
								clampStep={100}
								unit="ms"
								onChange={(v) =>
									updateMutation.mutate({
										rate_limit_max_wait_ms: String(v),
									})
								}
								description={t("settings.rateLimit.maxWait.description")}
							/>
						</div>
					</div>
				)}
			</div>
		</SettingsSection>
	);
}
