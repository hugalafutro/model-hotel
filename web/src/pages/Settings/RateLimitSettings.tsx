import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Gauge } from "lucide-react";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";

const RATE_LIMIT_RPS_OPTIONS = [
	{ value: "5", label: "5 req/s" },
	{ value: "10", label: "10 req/s" },
	{ value: "20", label: "20 req/s" },
	{ value: "50", label: "50 req/s" },
	{ value: "100", label: "100 req/s" },
	{ value: "0", label: "Unlimited" },
];

const RATE_LIMIT_BURST_OPTIONS = [
	{ value: "10", label: "10" },
	{ value: "20", label: "20" },
	{ value: "50", label: "50" },
	{ value: "100", label: "100" },
	{ value: "200", label: "200" },
];

interface RateLimitSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function RateLimitSettings({
	collapsed,
	onToggle,
}: RateLimitSettingsProps) {
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

	const rateLimitEnabled = settings?.rate_limit_enabled !== "false";
	const rateLimitRPS = settings?.rate_limit_rps || "10";
	const rateLimitBurst = settings?.rate_limit_burst || "20";
	const rateLimitIpEnabled = settings?.rate_limit_ip_enabled !== "false";
	const rateLimitIpRPS = settings?.rate_limit_ip_rps || "30";
	const rateLimitIpBurst = settings?.rate_limit_ip_burst || "60";
	const rateLimitMaxWaitMs = settings?.rate_limit_max_wait_ms || "200";

	return (
		<SettingsSection
			icon={Gauge}
			title="Rate Limiting"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					Control request throughput per virtual key to prevent abuse and ensure
					fair usage.
				</p>
				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							Enable Rate Limiting
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							Throttle proxy requests per virtual key
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
							label="Requests per Second"
							value={rateLimitRPS}
							options={RATE_LIMIT_RPS_OPTIONS}
							onChange={(v) => updateMutation.mutate({ rate_limit_rps: v })}
							description="Sustained request rate allowed per virtual key (0 = unlimited)"
						/>

						<SettingsSelect
							id="rate-limit-burst"
							label="Burst Size"
							value={rateLimitBurst}
							options={RATE_LIMIT_BURST_OPTIONS}
							onChange={(v) => updateMutation.mutate({ rate_limit_burst: v })}
							description="Maximum number of simultaneous requests before throttling kicks in"
						/>
					</>
				)}

				<div className="pt-2">
					<div className="flex items-center justify-between">
						<div>
							<p className="text-sm font-medium text-gray-300">
								IP Rate Limiting
							</p>
							<p className="text-gray-500 text-xs mt-0.5">
								Per-IP rate limiter (DoS protection, runs before auth)
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
									label="IP Requests per Second"
									value={rateLimitIpRPS}
									options={RATE_LIMIT_RPS_OPTIONS}
									onChange={(v) =>
										updateMutation.mutate({ rate_limit_ip_rps: v })
									}
									description="Sustained request rate per IP address (0 = unlimited)"
								/>
							</div>

							<div className="mt-4">
								<SettingsSelect
									id="rate-limit-ip-burst"
									label="IP Burst Size"
									value={rateLimitIpBurst}
									options={RATE_LIMIT_BURST_OPTIONS}
									onChange={(v) =>
										updateMutation.mutate({ rate_limit_ip_burst: v })
									}
									description="Maximum simultaneous requests per IP before throttling kicks in"
								/>
							</div>
						</>
					)}
				</div>

				{(rateLimitEnabled || rateLimitIpEnabled) && (
					<div className="pt-2">
						<p className="text-sm font-medium text-gray-300 mb-1">
							Rate Limit Backpressure
						</p>
						<p className="text-gray-500 text-xs mb-3">
							Shared wait behavior for both per-key and IP rate limiters
						</p>
						<div>
							<label
								htmlFor="rate-limit-max-wait"
								className="block text-sm font-medium text-gray-300 mb-2"
							>
								Max Wait (ms)
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
								Maximum time to wait before rejecting a rate-limited request. If
								a token becomes available within this window, the request
								proceeds; otherwise 429 is returned.
							</p>
						</div>
					</div>
				)}
			</div>
		</SettingsSection>
	);
}
