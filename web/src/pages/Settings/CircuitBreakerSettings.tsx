import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Shield } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";

interface CircuitBreakerSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function CircuitBreakerSettings({
	collapsed,
	onToggle,
}: CircuitBreakerSettingsProps) {
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

	const circuitBreakerEnabled = settings?.circuit_breaker_enabled !== "false";
	const circuitBreakerThreshold = settings?.circuit_breaker_threshold || "5";
	const circuitBreakerCooldown = settings?.circuit_breaker_cooldown || "1m0s";
	const failoverOnRateLimit = settings?.failover_on_rate_limit === "true";

	const CIRCUIT_BREAKER_COOLDOWN_OPTIONS = [
		{ value: "30s", label: t("settings.circuitBreaker.cooldown.30s") },
		{ value: "1m0s", label: t("settings.circuitBreaker.cooldown.1m0s") },
		{ value: "2m0s", label: t("settings.circuitBreaker.cooldown.2m0s") },
		{ value: "5m0s", label: t("settings.circuitBreaker.cooldown.5m0s") },
		{ value: "10m0s", label: t("settings.circuitBreaker.cooldown.10m0s") },
	];

	return (
		<SettingsSection
			icon={Shield}
			title={t("settings.circuitBreaker.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.circuitBreaker.description")}
				</p>
				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							{t("settings.circuitBreaker.enable")}
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							{t("settings.circuitBreaker.enableDescription")}
						</p>
					</div>
					<Toggle
						checked={circuitBreakerEnabled}
						onChange={(v) =>
							updateMutation.mutate({
								circuit_breaker_enabled: v ? "true" : "false",
							})
						}
						ariaLabel={t("settings.circuitBreaker.enable")}
					/>
				</div>

				{circuitBreakerEnabled && (
					<>
						<div className="mt-4">
							<label
								htmlFor="circuit-breaker-threshold"
								className="block text-sm font-medium text-gray-300 mb-2"
							>
								{t("settings.circuitBreaker.failureThreshold")}
							</label>
							<input
								id="circuit-breaker-threshold"
								type="number"
								min="1"
								max="100"
								value={circuitBreakerThreshold}
								onChange={(e) =>
									updateMutation.mutate({
										circuit_breaker_threshold: e.target.value,
									})
								}
								className="ui-input"
							/>
							<p className="text-gray-500 text-xs mt-1">
								{t("settings.circuitBreaker.failureThreshold.description")}
							</p>
						</div>

						<div className="mt-4">
							<SettingsSelect
								id="circuit-breaker-cooldown"
								label={t("settings.circuitBreaker.cooldownPeriod")}
								value={circuitBreakerCooldown}
								options={CIRCUIT_BREAKER_COOLDOWN_OPTIONS}
								onChange={(v) =>
									updateMutation.mutate({ circuit_breaker_cooldown: v })
								}
								description={t(
									"settings.circuitBreaker.cooldownPeriod.description",
								)}
							/>
						</div>
					</>
				)}

				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							{t("settings.circuitBreaker.failoverOnRateLimit")}
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							{t("settings.circuitBreaker.failoverOnRateLimitDescription")}
						</p>
					</div>
					<Toggle
						checked={failoverOnRateLimit}
						onChange={(v) =>
							updateMutation.mutate({
								failover_on_rate_limit: v ? "true" : "false",
							})
						}
						ariaLabel={t("settings.circuitBreaker.failoverOnRateLimit")}
					/>
				</div>
			</div>
		</SettingsSection>
	);
}
