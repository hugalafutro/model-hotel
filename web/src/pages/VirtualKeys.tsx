import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Pencil } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { api } from "../api/client";
import type { VirtualKey } from "../api/types";
import {
	CollapsibleToggle,
	useCollapsible,
} from "../components/CollapsibleToggle";
import { ConfirmDeleteButton } from "../components/ConfirmDeleteButton";
import { CopyablePill } from "../components/CopyablePill";
import type { SortState } from "../components/DataTable";
import {
	PaginationBar,
	Row,
	SortableHeader,
	StaticHeader,
} from "../components/DataTable";
import { EmptyState } from "../components/EmptyState";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { Modal } from "../components/Modal";
import { PageHeader } from "../components/PageHeader";
import { useToast } from "../context/ToastContext";
import { countLabel, formatNumber, formatRelativeTime } from "../utils/format";
import { snippetBash, snippetPowershell } from "../utils/snippets";

type VKSortField = "name" | "key" | "created" | "tokens" | "last_used";

function CreateKeyModal({
	onClose,
	onToast,
	existingKey,
}: {
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
	existingKey?: VirtualKey;
}) {
	const queryClient = useQueryClient();
	const [name, setName] = useState(existingKey?.name ?? "");
	const [rateLimitRps, setRateLimitRps] = useState<string>(
		existingKey?.rate_limit_rps?.toString() ?? "",
	);
	const [rateLimitBurst, setRateLimitBurst] = useState<string>(
		existingKey?.rate_limit_burst?.toString() ?? "",
	);
	const [createdKey, setCreatedKey] = useState<VirtualKey | null>(null);

	const createMutation = useMutation({
		mutationFn: ({
			name,
			rate_limit_rps,
			rate_limit_burst,
		}: {
			name: string;
			rate_limit_rps?: number | null;
			rate_limit_burst?: number | null;
		}) => api.virtualKeys.create(name, rate_limit_rps, rate_limit_burst),
		onSuccess: (vk) => {
			setCreatedKey(vk);
			queryClient.invalidateQueries({ queryKey: ["virtualKeys"] });
			onToast("Virtual key created", "success");
		},
		onError: (err: Error) => {
			onToast(`Failed: ${err.message}`, "error");
		},
	});

	const updateMutation = useMutation({
		mutationFn: ({
			name,
			rate_limit_rps,
			rate_limit_burst,
		}: {
			name: string;
			rate_limit_rps?: number | null;
			rate_limit_burst?: number | null;
		}) => {
			if (!existingKey) {
				throw new Error("No key to update");
			}
			return api.virtualKeys.update(existingKey.id, {
				name,
				rate_limit_rps,
				rate_limit_burst,
			});
		},
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["virtualKeys"] });
			onToast("Virtual key updated", "success");
			onClose();
		},
		onError: (err: Error) => {
			onToast(`Failed: ${err.message}`, "error");
		},
	});

	const handleSubmit = (e: React.FormEvent) => {
		e.preventDefault();
		if (!name.trim()) return;
		if (existingKey) {
			updateMutation.mutate({
				name: name.trim(),
				rate_limit_rps: rateLimitRps !== "" ? parseFloat(rateLimitRps) : null,
				rate_limit_burst:
					rateLimitBurst !== "" ? parseInt(rateLimitBurst, 10) : null,
			});
		} else {
			createMutation.mutate({
				name: name.trim(),
				rate_limit_rps: rateLimitRps !== "" ? parseFloat(rateLimitRps) : null,
				rate_limit_burst:
					rateLimitBurst !== "" ? parseInt(rateLimitBurst, 10) : null,
			});
		}
	};

	return (
		<Modal
			title={
				existingKey
					? "Edit Virtual Key"
					: createdKey
						? "Virtual Key Created"
						: "Create Virtual Key"
			}
			closeOnBackdrop={!createdKey || !!existingKey}
			onClose={onClose}
		>
			{createdKey && !existingKey ? (
				<>
					<p className="text-sm text-gray-400 mb-3">
						Copy this key now. It won't be shown again.
					</p>
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
							disabled={
								existingKey
									? updateMutation.isPending
									: createMutation.isPending
							}
							className="ui-btn ui-btn-primary disabled:opacity-50"
						>
							{existingKey
								? updateMutation.isPending
									? "Saving…"
									: "Save Changes"
								: createMutation.isPending
									? "Creating…"
									: "Create Key"}
						</button>
					</div>
				</form>
			)}
		</Modal>
	);
}

function KeyDetailModal({
	vk,
	onClose,
	onToast,
}: {
	vk: VirtualKey;
	onClose: () => void;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
}) {
	const queryClient = useQueryClient();

	const deleteMutation = useMutation({
		mutationFn: () => api.virtualKeys.delete(vk.id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["virtualKeys"] });
			onToast("Virtual key deleted", "success");
			onClose();
		},
		onError: (err: Error) => {
			onToast(`Failed to delete: ${err.message}`, "error");
		},
	});

	return (
		<Modal title="Virtual Key Details" onClose={onClose}>
			<div className="space-y-3 mb-6">
				<div>
					<span className="text-sm text-gray-500">Name</span>
					<p className="text-gray-200">{vk.name}</p>
				</div>
				<div>
					<span className="text-sm text-gray-500">Key</span>
					<p className="text-gray-200 font-mono">{vk.key_preview}</p>
				</div>
				<div>
					<span className="text-sm text-gray-500">Created</span>
					<p className="text-gray-200">
						{new Date(vk.created_at).toLocaleString()}
					</p>
				</div>
				<div>
					<span className="text-sm text-gray-500">Tokens Consumed</span>
					<p className="text-gray-200">{formatNumber(vk.tokens_used)}</p>
				</div>
				<div>
					<span className="text-sm text-gray-500">Last Used</span>
					<p className="text-gray-200">
						{vk.last_used_at
							? new Date(vk.last_used_at).toLocaleString()
							: "Never"}
					</p>
				</div>
			</div>

			<div className="flex justify-start items-center">
				<ConfirmDeleteButton
					onConfirm={() => deleteMutation.mutate()}
					loading={deleteMutation.isPending}
				/>
			</div>
		</Modal>
	);
}

export function VirtualKeys() {
	const { toast } = useToast();
	const [showCreate, setShowCreate] = useState(false);
	const [selectedKey, setSelectedKey] = useState<VirtualKey | null>(null);
	const [editingKey, setEditingKey] = useState<VirtualKey | undefined>(
		undefined,
	);
	const [sort, setSort] = useState<SortState<VKSortField>>({
		field: "name",
		dir: "asc",
	});
	const [pageSize, setPageSize] = useState(10);
	const [currentPage, setCurrentPage] = useState(1);
	const [terminalTab, setTerminalTab] = useState<"bash" | "powershell">("bash");
	const { collapsed: exampleCollapsed, toggle: toggleExample } = useCollapsible(
		"vk-example-collapsed",
		false,
	);

	const { data: keys, isLoading } = useQuery({
		queryKey: ["virtualKeys"],
		queryFn: () => api.virtualKeys.list(),
	});

	const handleSort = useCallback((field: VKSortField) => {
		setSort((prev) => ({
			field,
			dir: prev.field === field && prev.dir === "asc" ? "desc" : "asc",
		}));
		setCurrentPage(1);
	}, []);

	const proxyOrigin =
		typeof window !== "undefined"
			? window.location.origin
			: "http://localhost:8080";

	const sortedKeys = useMemo(() => {
		if (!keys) return [];
		const dir = sort.dir === "asc" ? 1 : -1;
		return [...keys].sort((a, b) => {
			switch (sort.field) {
				case "name":
					return dir * a.name.localeCompare(b.name);
				case "created":
					return (
						dir *
						(new Date(a.created_at).getTime() -
							new Date(b.created_at).getTime())
					);
				case "tokens":
					return dir * (a.tokens_used - b.tokens_used);
				case "last_used": {
					const aT = a.last_used_at ? new Date(a.last_used_at).getTime() : 0;
					const bT = b.last_used_at ? new Date(b.last_used_at).getTime() : 0;
					return dir * (aT - bT);
				}
				default:
					return 0;
			}
		});
	}, [keys, sort]);

	const totalPages = Math.ceil(sortedKeys.length / pageSize);
	const paginatedKeys = sortedKeys.slice(
		(currentPage - 1) * pageSize,
		currentPage * pageSize,
	);

	if (isLoading) {
		return <LoadingSpinner />;
	}

	return (
		<div className="space-y-6">
			<PageHeader
				icon={KeyRound}
				title={countLabel(keys?.length, "Virtual Key", "Virtual Keys")}
				description={
					<span>
						Issue keys for clients to access the proxy at{" "}
						<CopyablePill
							text={`${proxyOrigin}/v1`}
							displayText={`${proxyOrigin}/v1`}
							textClassName="text-(--accent) text-sm font-medium"
							iconClassName="w-3 h-3"
							className="inline-flex"
							tooltip="Click to copy proxy URL"
						/>
					</span>
				}
				actions={
					<button
						type="button"
						onClick={() => setShowCreate(true)}
						className="ui-btn ui-btn-primary"
					>
						+ Create Key
					</button>
				}
			/>

			{sortedKeys.length > 0 && (
				<div className="flex items-center justify-end">
					<PaginationBar
						page={currentPage}
						totalPages={totalPages}
						totalItems={sortedKeys.length}
						pageSize={pageSize}
						onPageChange={setCurrentPage}
						onPageSizeChange={(s) => {
							setPageSize(s);
							setCurrentPage(1);
						}}
						label="keys"
					/>
				</div>
			)}

			{sortedKeys.length > 0 ? (
				<div className="ui-card overflow-hidden">
					<table className="w-full table-fixed ui-table">
						<colgroup>
							<col className="w-[28%]" />
							<col className="w-[18%]" />
							<col className="w-[22%]" />
							<col className="w-[18%]" />
							<col className="w-[14%]" />
						</colgroup>
						<thead>
							<tr>
								<SortableHeader
									label="Name"
									field="name"
									sort={sort}
									onSort={handleSort}
									tooltip="Display name for the virtual key"
								/>
								<StaticHeader tooltip="Preview of the API key (full key only shown once on creation)">
									Key
								</StaticHeader>
								<SortableHeader
									label="Created"
									field="created"
									sort={sort}
									onSort={handleSort}
									tooltip="When the key was created"
								/>
								<SortableHeader
									label="Tokens"
									field="tokens"
									sort={sort}
									onSort={handleSort}
									tooltip="Total tokens consumed using this key"
								/>
								<SortableHeader
									label="Last Used"
									field="last_used"
									sort={sort}
									onSort={handleSort}
									tooltip="When the key was last used for a request"
								/>
							</tr>
						</thead>
						<tbody>
							{paginatedKeys.map((vk) => (
								<Row key={vk.id}>
									<td className="px-4 py-3">
										<div className="flex items-center gap-2">
											<button
												type="button"
												onClick={() => setSelectedKey(vk)}
												className="text-gray-200 hover:text-(--accent) transition-colors cursor-pointer text-sm"
											>
												{vk.name}
											</button>
											<button
												type="button"
												onClick={(e) => {
													e.stopPropagation();
													setEditingKey(vk);
												}}
												className="text-gray-500 hover:text-(--accent) transition-colors"
												title="Edit"
												aria-label="Edit"
											>
												<Pencil className="w-3.5 h-3.5" />
											</button>
										</div>
									</td>
									<td className="px-4 py-3 text-gray-500 font-mono text-xs">
										{vk.key_preview}
									</td>
									<td className="px-4 py-3 text-sm text-gray-400">
										{new Date(vk.created_at).toLocaleString()}
									</td>
									<td className="px-4 py-3 text-sm text-gray-400 font-mono">
										{formatNumber(vk.tokens_used)}
									</td>
									<td className="px-4 py-3 text-sm text-gray-400">
										{formatRelativeTime(vk.last_used_at)}
									</td>
								</Row>
							))}
						</tbody>
					</table>
				</div>
			) : (
				<EmptyState message="No virtual keys. Create one to start using the proxy." />
			)}

			{sortedKeys.length > 0 && (
				<div className="ui-card p-6">
					<div className="flex items-center justify-between">
						<h3 className="text-sm font-medium text-gray-300">Quick Start</h3>
						<CollapsibleToggle
							collapsed={exampleCollapsed}
							onToggle={toggleExample}
							variant="muted"
							size={14}
						/>
					</div>
					<div
						className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
							exampleCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
						}`}
					>
						<div
							className={`overflow-hidden space-y-5 transition-[margin] duration-300 ${
								exampleCollapsed ? "mt-0" : "mt-5"
							}`}
						>
							<div className="grid grid-cols-1 md:grid-cols-3 gap-3">
								<div className="flex items-start gap-3 p-4 ui-card">
									<div className="flex items-center justify-center w-7 h-7 rounded-lg bg-(--accent-light) text-(--accent) text-sm font-bold shrink-0">
										1
									</div>
									<div>
										<h3 className="text-sm font-medium text-gray-200">
											Create a Key
										</h3>
										<p className="text-xs text-gray-400 mt-1">
											Click the button above to generate a new virtual key
										</p>
									</div>
								</div>
								<div className="flex items-start gap-3 p-4 ui-card">
									<div className="flex items-center justify-center w-7 h-7 rounded-lg bg-(--accent-light) text-(--accent) text-sm font-bold shrink-0">
										2
									</div>
									<div>
										<h3 className="text-sm font-medium text-gray-200">
											Copy the Full Key
										</h3>
										<p className="text-xs text-gray-400 mt-1">
											The complete key is shown only once on creation
										</p>
									</div>
								</div>
								<div className="flex items-start gap-3 p-4 ui-card">
									<div className="flex items-center justify-center w-7 h-7 rounded-lg bg-(--accent-light) text-(--accent) text-sm font-bold shrink-0">
										3
									</div>
									<div>
										<h3 className="text-sm font-medium text-gray-200">
											Make Requests
										</h3>
										<p className="text-xs text-gray-400 mt-1">
											Use your key to call the proxy API endpoints
										</p>
									</div>
								</div>
							</div>

							<div>
								<div className="terminal-tab-bar">
									<button
										type="button"
										onClick={() => setTerminalTab("bash")}
										className={`terminal-tab ${terminalTab === "bash" ? "terminal-tab-active" : "terminal-tab-inactive"}`}
									>
										<svg
											viewBox="0 0 24 24"
											className="w-3.5 h-3.5"
											fill="none"
											stroke="currentColor"
											strokeWidth="2"
											strokeLinecap="round"
											strokeLinejoin="round"
										>
											<title>Terminal</title>
											<polyline points="4 17 10 11 4 5" />
											<line x1="12" y1="19" x2="20" y2="19" />
										</svg>
										bash
									</button>
									<button
										type="button"
										onClick={() => setTerminalTab("powershell")}
										className={`terminal-tab ${terminalTab === "powershell" ? "terminal-tab-active" : "terminal-tab-inactive"}`}
									>
										<svg
											viewBox="0 0 24 24"
											className="w-3.5 h-3.5"
											fill="none"
											stroke="currentColor"
											strokeWidth="2"
											strokeLinecap="round"
											strokeLinejoin="round"
										>
											<title>Monitor</title>
											<rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
											<line x1="8" y1="21" x2="16" y2="21" />
											<line x1="12" y1="17" x2="12" y2="21" />
										</svg>
										PowerShell
									</button>
								</div>
								{terminalTab === "bash" ? (
									<div className="relative rounded-b-lg rounded-tr-lg bg-gray-950 border border-gray-800 overflow-hidden min-h-70">
										<div className="flex items-center gap-1.5 px-3 py-2 border-b border-gray-800 terminal-titlebar">
											<div className="w-2.5 h-2.5 rounded-full bg-red-500" />
											<div className="w-2.5 h-2.5 rounded-full bg-yellow-500" />
											<div className="w-2.5 h-2.5 rounded-full bg-green-500" />
											<span className="text-xs text-gray-600 ml-2 font-mono terminal-titlebar-label">
												bash
											</span>
										</div>
										<pre className="p-4 text-xs text-gray-400 font-mono overflow-x-auto terminal-body">
											<code className="terminal-code">
												{snippetBash({
													origin:
														typeof window !== "undefined"
															? window.location.origin
															: "http://localhost:8080",
												})}
											</code>
										</pre>
									</div>
								) : (
									<div className="terminal-win11 relative rounded-b-lg rounded-tr-lg overflow-hidden border border-[#333] min-h-70">
										<div className="terminal-win11-titlebar flex items-center justify-between px-3 py-1.5 border-b border-[#333]">
											<div className="flex items-center gap-2">
												<svg
													className="win11-icon"
													viewBox="0 0 24 24"
													width="14"
													height="14"
													fill="currentColor"
												>
													<title>Windows</title>
													<path d="M0 3.449L9.75 2.1v9.45H0m10.95 0H24v9.35L10.95 21.9M0 12.6h9.75v9.15L0 20.1m10.95-9.5H24V2.1L10.95 3.65" />
												</svg>
												<span className="terminal-win11-titlebar-label text-xs font-mono text-[#ccc]">
													PowerShell
												</span>
											</div>
											<div className="flex items-center" aria-hidden="true">
												<span className="inline-flex items-center justify-center w-11.5 h-7.5 text-[#999] hover:text-white hover:bg-[#e81123] transition-colors cursor-default">
													<svg
														width="10"
														height="10"
														viewBox="0 0 10 10"
														fill="none"
														stroke="currentColor"
														strokeWidth="1.2"
													>
														<title>Close</title>
														<line x1="0" y1="0" x2="10" y2="10" />
														<line x1="10" y1="0" x2="0" y2="10" />
													</svg>
												</span>
											</div>
										</div>
										<pre className="terminal-win11-body p-4 text-xs font-mono overflow-x-auto text-[#ccc] bg-[#0c0c0c]">
											<code className="terminal-win11-code">
												{snippetPowershell({
													origin:
														typeof window !== "undefined"
															? window.location.origin
															: "http://localhost:8080",
												})}
											</code>
										</pre>
									</div>
								)}
							</div>

							<div className="flex items-start gap-3 p-4 rounded-lg bg-(--accent-light) border border-(--accent-lighter)">
								<div className="w-1.5 h-1.5 rounded-full bg-(--accent) mt-1.5 shrink-0" />
								<p className="text-xs text-gray-300 leading-relaxed">
									<span className="text-gray-200 font-medium">Note:</span>{" "}
									Virtual keys are used to authenticate requests to the proxy.
									Each key tracks its own token usage. You can create multiple
									keys for different clients or environments.
								</p>
							</div>
						</div>
					</div>
				</div>
			)}

			{(showCreate || editingKey) && (
				<CreateKeyModal
					onClose={() => {
						setShowCreate(false);
						setEditingKey(undefined);
					}}
					onToast={toast}
					existingKey={editingKey}
				/>
			)}

			{selectedKey && (
				<KeyDetailModal
					vk={selectedKey}
					onClose={() => setSelectedKey(null)}
					onToast={toast}
				/>
			)}
		</div>
	);
}
