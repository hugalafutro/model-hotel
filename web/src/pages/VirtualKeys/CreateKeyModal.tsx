import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Brain, RotateCcw } from "lucide-react";
import { useState } from "react";
import { api } from "../../api/client";
import type { VirtualKey } from "../../api/types";
import { CopyablePill } from "../../components/CopyablePill";
import { Modal } from "../../components/Modal";

function BrainSlashIcon({
	size = 14,
	className = "",
}: {
	size?: number;
	className?: string;
}) {
	return (
		<span
			className={`relative inline-block ${className}`}
			style={{ width: size, height: size }}
		>
			<Brain size={size} />
			<span className="absolute inset-0 flex items-center justify-center pointer-events-none">
				<span className="w-full h-[1.5px] bg-current rotate-45" />
			</span>
		</span>
	);
}

export function CreateKeyModal({
	onClose,
	onToast,
}: {
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
	const queryClient = useQueryClient();
	const [name, setName] = useState("");
	const [rateLimitRps, setRateLimitRps] = useState<string>("");
	const [rateLimitBurst, setRateLimitBurst] = useState<string>("");
	const [excludedProviders, setExcludedProviders] = useState<string[]>([]);
	const [stripReasoning, setStripReasoning] = useState(false);
	const [createdKey, setCreatedKey] = useState<VirtualKey | null>(null);
	const [providerError, setProviderError] = useState("");

	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});

	const sortedProviders = (providers ?? [])
		.slice()
		.sort((a, b) => a.name.localeCompare(b.name));

	const toggleProvider = (providerId: string) => {
		setExcludedProviders((prev) =>
			prev.includes(providerId)
				? prev.filter((id) => id !== providerId)
				: [...prev, providerId],
		);
	};

	const resetProviders = () => setExcludedProviders([]);

	const createMutation = useMutation({
		mutationFn: ({
			name,
			rate_limit_rps,
			rate_limit_burst,
			allowed_providers,
			strip_reasoning,
		}: {
			name: string;
			rate_limit_rps?: number | null;
			rate_limit_burst?: number | null;
			allowed_providers?: string[] | null;
			strip_reasoning?: boolean;
		}) =>
			api.virtualKeys.create(
				name,
				rate_limit_rps,
				rate_limit_burst,
				allowed_providers,
				strip_reasoning,
			),
		onSuccess: (vk) => {
			setCreatedKey(vk);
			queryClient.invalidateQueries({ queryKey: ["virtualKeys"] });
			onToast("Virtual key created", "success");
		},
		onError: (err: Error) => {
			onToast(`Failed: ${err.message}`, "error");
		},
	});

	const handleSubmit = (e: React.FormEvent) => {
		e.preventDefault();
		if (!name.trim()) return;
		setProviderError("");
		const allProviderIds = sortedProviders.map((p) => p.id);
		const allowedProviders =
			excludedProviders.length > 0
				? allProviderIds.filter((id) => !excludedProviders.includes(id))
				: null;
		if (allowedProviders && allowedProviders.length === 0) {
			setProviderError("At least one provider must remain accessible");
			return;
		}
		createMutation.mutate({
			name: name.trim(),
			rate_limit_rps: rateLimitRps !== "" ? parseFloat(rateLimitRps) : null,
			rate_limit_burst:
				rateLimitBurst !== "" ? parseInt(rateLimitBurst, 10) : null,
			allowed_providers: allowedProviders,
			strip_reasoning: stripReasoning,
		});
	};

	return (
		<Modal
			title={createdKey ? "Virtual Key Created" : "Create Virtual Key"}
			closeOnBackdrop={!createdKey}
			onClose={onClose}
		>
			{createdKey ? (
				<>
					<div className="bg-red-500/10 border-2 border-red-500/40 rounded-lg p-3 mb-4">
						<p className="text-red-400 font-semibold text-sm">
							This key cannot be recovered after you close this modal.
						</p>
						<p className="text-red-400/70 text-xs mt-1">
							Virtual keys are hashed before storage. Copy it now or it is gone
							forever.
						</p>
					</div>
					<div className="bg-gray-950 rounded-lg p-3 mb-4">
						{createdKey.key && (
							<CopyablePill
								text={createdKey.key}
								displayText={createdKey.key}
								textClassName="text-sm text-green-400 font-mono break-all"
								tooltip="Click to copy key"
							/>
						)}
					</div>
					<p className="text-sm text-gray-500 mb-4">
						Use as:{" "}
						<code className="text-gray-400">Bearer {createdKey.key}</code> at{" "}
						<code className="text-gray-400">{window.location.origin}/v1</code>
					</p>
					<div className="flex justify-end">
						<button
							type="button"
							onClick={onClose}
							className="ui-btn ui-btn-secondary"
						>
							Done
						</button>
					</div>
				</>
			) : (
				<form onSubmit={handleSubmit} className="space-y-4">
					<div>
						<label
							htmlFor="vk-name"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							Name
						</label>
						<input
							id="vk-name"
							type="text"
							required
							maxLength={100}
							value={name}
							onChange={(e) => setName(e.target.value)}
							className="ui-input"
							placeholder="e.g., My App"
						/>
					</div>
					<div>
						<label
							htmlFor="vk-rate-limit-rps"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							Rate Limit RPS (requests/sec)
						</label>
						<input
							id="vk-rate-limit-rps"
							type="number"
							min="0"
							value={rateLimitRps}
							onChange={(e) => setRateLimitRps(e.target.value)}
							className="ui-input"
							placeholder="Use global setting"
						/>
					</div>
					<div>
						<label
							htmlFor="vk-rate-limit-burst"
							className="block text-sm font-medium text-gray-300 mb-1"
						>
							Rate Limit Burst (max concurrent)
						</label>
						<input
							id="vk-rate-limit-burst"
							type="number"
							min="0"
							value={rateLimitBurst}
							onChange={(e) => setRateLimitBurst(e.target.value)}
							className="ui-input"
							placeholder="Use global setting"
						/>
					</div>
					<div>
						<div className="flex items-center justify-between mb-1">
							<span className="text-sm font-medium text-gray-300">
								Provider Access
							</span>
							{excludedProviders.length > 0 && (
								<button
									type="button"
									onClick={resetProviders}
									className="text-gray-500 hover:text-gray-300 transition-colors cursor-pointer"
									aria-label="Restore access to all providers"
									title="Restore access to all providers"
								>
									<RotateCcw size={14} />
								</button>
							)}
						</div>
						<p className="text-xs text-gray-500 mb-2">
							Click a provider to restrict access. All are accessible by
							default.
						</p>
						{sortedProviders.length === 0 ? (
							<p className="text-xs text-gray-500 italic">
								No providers available.
							</p>
						) : (
							<div className="flex flex-wrap gap-1.5 max-h-40 overflow-y-auto">
								{sortedProviders.map((provider) => {
									const isExcluded = excludedProviders.includes(provider.id);
									return (
										<button
											key={provider.id}
											type="button"
											onClick={() => toggleProvider(provider.id)}
											aria-pressed={isExcluded}
											className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium cursor-pointer transition-colors
												${
													isExcluded
														? "bg-gray-800/60 text-gray-500 border border-gray-700/60 line-through hover:bg-gray-700/60"
														: "bg-(--accent)/20 text-(--accent) border border-(--accent)/40"
												}`}
										>
											{provider.name}
										</button>
									);
								})}
							</div>
						)}
					</div>
					{providerError && (
						<p className="text-xs text-red-400 mt-1">{providerError}</p>
					)}

					<div>
						<div className="flex items-center gap-2 text-(--accent) mb-2">
							<BrainSlashIcon size={12} className="shrink-0" />
							<span className="text-xs font-semibold uppercase tracking-wider">
								Strip Reasoning
							</span>
						</div>
						<div className="flex items-center gap-3">
							<button
								type="button"
								onClick={() => setStripReasoning(!stripReasoning)}
								aria-pressed={stripReasoning}
								aria-label={
									stripReasoning
										? "Disable strip reasoning"
										: "Enable strip reasoning"
								}
								className={`relative inline-flex items-center h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none ${
									stripReasoning
										? "bg-(--accent) shadow-[var(--glow-accent)]"
										: "bg-gray-600"
								}`}
							>
								<span
									aria-hidden="true"
									className={`pointer-events-none block h-4 w-4 transform rounded-full bg-white shadow-sm ring-0 transition-transform duration-200 ease-in-out ${
										stripReasoning ? "translate-x-6" : "translate-x-0"
									}`}
								/>
							</button>
							<span className="text-sm text-gray-200">
								{stripReasoning ? "Enabled" : "Disabled"}
							</span>
						</div>
						<p className="text-xs text-gray-400 mt-1.5">
							When enabled, reasoning/thinking tokens are removed from streaming
							responses for clients that cannot handle them (e.g., Warp.dev).
						</p>
					</div>

					<div className="flex space-x-3 justify-end pt-2">
						<button
							type="button"
							onClick={onClose}
							className="ui-btn ui-btn-secondary"
						>
							Cancel
						</button>
						<button
							type="submit"
							disabled={createMutation.isPending}
							className="ui-btn ui-btn-primary disabled:opacity-50"
						>
							{createMutation.isPending ? "Creating\u2026" : "Create Key"}
						</button>
					</div>
				</form>
			)}
		</Modal>
	);
}
