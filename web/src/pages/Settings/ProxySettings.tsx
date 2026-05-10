import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Timer } from "lucide-react";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { useToast } from "../../context/ToastContext";

const REQUEST_TIMEOUT_OPTIONS = [
	{ value: "30s", label: "30 seconds" },
	{ value: "1m0s", label: "1 minute (default)" },
	{ value: "2m0s", label: "2 minutes" },
	{ value: "5m0s", label: "5 minutes" },
	{ value: "10m0s", label: "10 minutes" },
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
				<div>
					<label
						htmlFor="request-timeout"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						Request Timeout
					</label>
					<select
						id="request-timeout"
						value={requestTimeout}
						onChange={(e) =>
							updateMutation.mutate({
								request_timeout: e.target.value,
							})
						}
						className="ui-input"
					>
						{REQUEST_TIMEOUT_OPTIONS.map((opt) => (
							<option key={opt.value} value={opt.value}>
								{opt.label}
							</option>
						))}
					</select>
					<p className="text-gray-500 text-xs mt-1">
						Maximum time for non-streaming requests before timing out. Streaming
						requests automatically get 10× this duration to accommodate
						thinking/reasoning models.
					</p>
				</div>
			</div>
		</SettingsSection>
	);
}
