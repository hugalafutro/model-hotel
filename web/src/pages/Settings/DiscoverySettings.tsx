import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";
import { DISCOVERY_INTERVALS } from "./constants";

interface DiscoverySettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function DiscoverySettings({
	collapsed,
	onToggle,
}: DiscoverySettingsProps) {
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

	const isUpdating = updateMutation.isPending;
	const discoveryInterval = settings?.discovery_interval || "6h";
	const discoveryOnStartup = settings?.discovery_on_startup !== "false";
	const discoveryOnCreate = settings?.discovery_on_provider_create !== "false";

	return (
		<SettingsSection
			icon={Search}
			title="Model Discovery"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					Configure how and when models are auto-discovered from your providers.
				</p>
				<div>
					<label
						htmlFor="discovery-interval"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						Discovery Interval
					</label>
					<select
						id="discovery-interval"
						value={discoveryInterval}
						onChange={(e) =>
							updateMutation.mutate({
								discovery_interval: e.target.value,
							})
						}
						className="ui-input"
						disabled={isUpdating}
					>
						{DISCOVERY_INTERVALS.map((opt) => (
							<option key={opt.value} value={opt.value}>
								{opt.label}
							</option>
						))}
					</select>
					{discoveryInterval === "0" ? (
						<p className="text-amber-400 text-xs mt-1">
							Periodic discovery is disabled. Models will only be discovered
							when you click "Discover Now" or "Discover All", or when a new
							provider is created.
						</p>
					) : (
						<p className="text-gray-500 text-xs mt-1">
							How often to automatically re-discover models from all enabled
							providers
						</p>
					)}
				</div>

				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							Discover on Startup
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							Run discovery for all providers when the server starts
						</p>
					</div>
					<Toggle
						checked={discoveryOnStartup}
						onChange={(v) =>
							updateMutation.mutate({
								discovery_on_startup: v ? "true" : "false",
							})
						}
						disabled={isUpdating}
					/>
				</div>

				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							Discover on Provider Creation
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							Automatically discover models when a new provider is added
						</p>
					</div>
					<Toggle
						checked={discoveryOnCreate}
						onChange={(v) =>
							updateMutation.mutate({
								discovery_on_provider_create: v ? "true" : "false",
							})
						}
						disabled={isUpdating}
					/>
				</div>
			</div>
		</SettingsSection>
	);
}
