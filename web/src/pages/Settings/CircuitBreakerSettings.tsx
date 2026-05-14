import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Shield } from "lucide-react";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";

const CIRCUIT_BREAKER_COOLDOWN_OPTIONS = [
	{ value: "30s", label: "30 seconds" },
	{ value: "1m0s", label: "1 minute (default)" },
	{ value: "2m0s", label: "2 minutes" },
	{ value: "5m0s", label: "5 minutes" },
	{ value: "10m0s", label: "10 minutes" },
];

interface CircuitBreakerSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function CircuitBreakerSettings({
	collapsed,
	onToggle,
}: CircuitBreakerSettingsProps) {
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

	const circuitBreakerEnabled = settings?.circuit_breaker_enabled !== "false";
	const circuitBreakerThreshold = settings?.circuit_breaker_threshold || "5";
	const circuitBreakerCooldown = settings?.circuit_breaker_cooldown || "1m0s";
	const failoverOnRateLimit = settings?.failover_on_rate_limit === "true";

	return (
		<SettingsSection
			icon={Shield}
			title="Circuit Breaker & Failover"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					Configure how the proxy handles provider failures and rate-limited
					requests.
				</p>
				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							Enable Circuit Breaker
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							Temporarily stop routing to providers that are failing
						</p>
					</div>
					<Toggle
						checked={circuitBreakerEnabled}
						onChange={(v) =>
							updateMutation.mutate({
								circuit_breaker_enabled: v ? "true" : "false",
							})
						}
					/>
				</div>

				{circuitBreakerEnabled && (
					<>
						<div className="mt-4">
							<label
								htmlFor="circuit-breaker-threshold"
								className="block text-sm font-medium text-gray-300 mb-2"
							>
								Failure Threshold
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
								Consecutive failures before the circuit opens and stops routing
								to the provider
							</p>
						</div>

						<div className="mt-4">
							<SettingsSelect
								id="circuit-breaker-cooldown"
								label="Cooldown Period"
								value={circuitBreakerCooldown}
								options={CIRCUIT_BREAKER_COOLDOWN_OPTIONS}
								onChange={(v) =>
									updateMutation.mutate({ circuit_breaker_cooldown: v })
								}
								description="Time to wait before retrying a provider with an open circuit"
							/>
						</div>
					</>
				)}

				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							Failover on Rate Limit
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							Route to failover group when a provider returns 429
						</p>
					</div>
					<Toggle
						checked={failoverOnRateLimit}
						onChange={(v) =>
							updateMutation.mutate({
								failover_on_rate_limit: v ? "true" : "false",
							})
						}
					/>
				</div>
			</div>
		</SettingsSection>
	);
}
