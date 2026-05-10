import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ScrollText } from "lucide-react";
import { useState } from "react";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { useToast } from "../../context/ToastContext";

const LOG_RETENTION_OPTIONS = [
	{ value: "0", label: "Disabled" },
	{ value: "24h", label: "1 day" },
	{ value: "168h", label: "1 week" },
	{ value: "720h", label: "1 month" },
];

const STALE_REQUEST_TIMEOUT_OPTIONS = [
	{ value: "5m0s", label: "5 minutes" },
	{ value: "10m0s", label: "10 minutes" },
	{ value: "15m0s", label: "15 minutes" },
	{ value: "30m0s", label: "30 minutes (default)" },
	{ value: "1h0m0s", label: "1 hour" },
	{ value: "2h0m0s", label: "2 hours" },
	{ value: "0s", label: "Disabled (never mark as stale)" },
];

interface LoggingSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function LoggingSettings({ collapsed, onToggle }: LoggingSettingsProps) {
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const [confirmDelete, setConfirmDelete] = useState(false);
	const [deleteSelection, setDeleteSelection] = useState("");
	const [confirmDeleteAppLogs, setConfirmDeleteAppLogs] = useState(false);

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

	const purgeMutation = useMutation({
		mutationFn: (olderThan: string) => api.logs.purge(olderThan),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["logs"] });
			toast("Requests deleted", "success");
			setConfirmDelete(false);
			setDeleteSelection("");
		},
		onError: (err: Error) => {
			toast(`Failed to delete requests: ${err.message}`, "error");
			setConfirmDelete(false);
		},
	});

	const purgeAppLogsMutation = useMutation({
		mutationFn: () => api.appLogs.purge(),
		onSuccess: (data) => {
			queryClient.invalidateQueries({ queryKey: ["appLogs"] });
			toast(`Deleted ${data.deleted} log entries`, "success");
			setConfirmDeleteAppLogs(false);
		},
		onError: (err: Error) => {
			toast(`Failed to delete app logs: ${err.message}`, "error");
			setConfirmDeleteAppLogs(false);
		},
	});

	const logRetention = settings?.log_retention || "0";
	const staleRequestTimeout = settings?.stale_request_timeout || "30m0s";

	const getDeleteOlderThan = (selection: string): string => {
		switch (selection) {
			case "1d":
				return "24h";
			case "1w":
				return "168h";
			case "1m":
				return "720h";
			case "all":
				return "all";
			default:
				return "";
		}
	};

	return (
		<SettingsSection
			icon={ScrollText}
			title="Logging"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<div>
					<label
						htmlFor="log-retention"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						Log Retention
					</label>
					<select
						id="log-retention"
						value={logRetention}
						onChange={(e) =>
							updateMutation.mutate({
								log_retention: e.target.value,
							})
						}
						className="ui-input"
					>
						{LOG_RETENTION_OPTIONS.map((opt) => (
							<option key={opt.value} value={opt.value}>
								{opt.label}
							</option>
						))}
					</select>
					{logRetention === "0" ? (
						<p className="text-amber-400 text-xs mt-1">
							Log retention is disabled. Logs will accumulate indefinitely until
							manually purged.
						</p>
					) : (
						<p className="text-gray-500 text-xs mt-1">
							Automatically delete logs older than this period
						</p>
					)}
				</div>

				<div>
					<label
						htmlFor="stale-request-timeout"
						className="block text-sm font-medium text-gray-300 mb-2"
					>
						Stale Request Timeout
					</label>
					<select
						id="stale-request-timeout"
						value={staleRequestTimeout}
						onChange={(e) =>
							updateMutation.mutate({
								stale_request_timeout: e.target.value,
							})
						}
						className="ui-input"
					>
						{STALE_REQUEST_TIMEOUT_OPTIONS.map((opt) => (
							<option key={opt.value} value={opt.value}>
								{opt.label}
							</option>
						))}
					</select>
					{staleRequestTimeout === "0s" ? (
						<p className="text-amber-400 text-xs mt-1">
							Stale request detection is disabled. Orphaned requests from server
							restarts will still be marked as failed, but age-based cleanup
							will not run.
						</p>
					) : (
						<p className="text-gray-500 text-xs mt-1">
							Mark pending/streaming requests as &ldquo;interrupted&rdquo; if
							they remain in-progress longer than this. Accounts for providers
							with long time-to-first-token.
						</p>
					)}
				</div>

				<div>
					<div className="flex items-center justify-between">
						<div>
							{!confirmDelete ? (
								<button
									type="button"
									onClick={() => setConfirmDelete(true)}
									className="ui-btn ui-btn-danger"
								>
									Delete Requests
								</button>
							) : (
								<div className="flex items-center gap-2">
									<select
										value={deleteSelection}
										onChange={(e) => setDeleteSelection(e.target.value)}
										className="ui-input px-3 py-1.5 text-xs"
									>
										<option value="">Select range...</option>
										<option value="1d">Older than 1 day</option>
										<option value="1w">Older than 1 week</option>
										<option value="1m">Older than 1 month</option>
										<option value="all">All logs</option>
									</select>
									<button
										type="button"
										disabled={!deleteSelection}
										onClick={() => {
											const olderThan = getDeleteOlderThan(deleteSelection);
											if (olderThan) purgeMutation.mutate(olderThan);
										}}
										className="ui-btn ui-btn-danger disabled:opacity-50 disabled:cursor-not-allowed"
									>
										Confirm Delete
									</button>
									<button
										type="button"
										onClick={() => {
											setConfirmDelete(false);
											setDeleteSelection("");
										}}
										className="ui-btn ui-btn-secondary"
									>
										Cancel
									</button>
								</div>
							)}
						</div>
						<div>
							{!confirmDeleteAppLogs ? (
								<button
									type="button"
									onClick={() => setConfirmDeleteAppLogs(true)}
									className="ui-btn ui-btn-danger"
								>
									Delete Logs
								</button>
							) : (
								<div className="flex items-center gap-2">
									<span className="text-xs text-red-400">
										Clear all application logs?
									</span>
									<button
										type="button"
										onClick={() => purgeAppLogsMutation.mutate()}
										disabled={purgeAppLogsMutation.isPending}
										className="ui-btn ui-btn-danger disabled:opacity-50 disabled:cursor-not-allowed"
									>
										{purgeAppLogsMutation.isPending ? "Deleting…" : "Confirm"}
									</button>
									<button
										type="button"
										onClick={() => setConfirmDeleteAppLogs(false)}
										className="ui-btn ui-btn-secondary"
									>
										Cancel
									</button>
								</div>
							)}
						</div>
					</div>
				</div>
			</div>
		</SettingsSection>
	);
}
