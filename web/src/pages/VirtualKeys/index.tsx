import { useQuery } from "@tanstack/react-query";
import { Brain, KeyRound, ShieldCheck } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { api } from "../../api/client";
import type { VirtualKey } from "../../api/types";
import { CopyablePill } from "../../components/CopyablePill";
import type { SortState } from "../../components/DataTable";
import {
	PaginationBar,
	Row,
	SortableHeader,
	StaticHeader,
} from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { LoadingSpinner } from "../../components/LoadingSpinner";
import { PageHeader } from "../../components/PageHeader";
import { TerminalPreview } from "../../components/TerminalPreview";
import { useToast } from "../../context/ToastContext";
import {
	countLabel,
	formatNumber,
	formatRelativeTime,
} from "../../utils/format";
import {
	snippetBash,
	snippetBashText,
	snippetClaudeCode,
	snippetClaudeCodeText,
	snippetHermes,
	snippetHermesText,
	snippetJS,
	snippetJSText,
	snippetLibreChat,
	snippetLibreChatText,
	snippetOpenClaw,
	snippetOpenClawText,
	snippetOpencodeVK,
	snippetOpencodeVKText,
	snippetPowershell,
	snippetPowershellText,
	snippetPython,
	snippetPythonText,
	snippetZedVK,
	snippetZedVKText,
} from "../../utils/snippets";
import { CreateKeyModal } from "./CreateKeyModal";
import { KeyDetailModal } from "./KeyDetailModal";

type VKSortField =
	| "name"
	| "rps"
	| "burst"
	| "created"
	| "tokens"
	| "last_used";

export function VirtualKeys() {
	const { toast } = useToast();
	const [showCreate, setShowCreate] = useState(false);
	const [selectedKey, setSelectedKey] = useState<VirtualKey | null>(null);
	const [sort, setSort] = useState<SortState<VKSortField>>({
		field: "name",
		dir: "asc",
	});
	const [pageSize, setPageSize] = useState(10);
	const [currentPage, setCurrentPage] = useState(1);

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
				case "rps": {
					const aR = a.rate_limit_rps ?? 0;
					const bR = b.rate_limit_rps ?? 0;
					return dir * (aR - bR);
				}
				case "burst": {
					const aB = a.rate_limit_burst ?? 0;
					const bB = b.rate_limit_burst ?? 0;
					return dir * (aB - bB);
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
		<div className="space-y-6 pb-8">
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
							<col className="w-[22%]" />
							<col className="w-[16%]" />
							<col className="w-[8%]" />
							<col className="w-[8%]" />
							<col className="w-[18%]" />
							<col className="w-[16%]" />
							<col className="w-[12%]" />
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
									label="RPS"
									field="rps"
									sort={sort}
									onSort={handleSort}
									tooltip="Requests per second rate limit"
								/>
								<SortableHeader
									label="Burst"
									field="burst"
									sort={sort}
									onSort={handleSort}
									tooltip="Burst capacity for rate limiting"
								/>
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
								<Row key={vk.id} onClick={() => setSelectedKey(vk)}>
									<td className="px-4 py-3 text-sm text-gray-200 truncate overflow-hidden text-ellipsis max-w-0">
										<div className="flex items-center gap-1.5">
											{vk.allowed_providers &&
												vk.allowed_providers.length > 0 && (
													<span title="Provider-restricted key">
														<ShieldCheck
															size={14}
															className="text-(--accent) shrink-0"
														/>
													</span>
												)}
											{vk.strip_reasoning && (
												<span
													title="Reasoning fields stripped"
													className="relative"
												>
													<Brain
														size={14}
														className="text-(--text-tertiary) shrink-0"
													/>
													<svg
														viewBox="0 0 24 24"
														className="absolute inset-0 w-[14px] h-[14px] text-red-400/80"
														fill="none"
														stroke="currentColor"
														strokeWidth="2.5"
														strokeLinecap="round"
													>
														<title>Reasoning stripped</title>
														<line x1="4" y1="4" x2="20" y2="20" />
													</svg>
												</span>
											)}
											<span className="truncate">{vk.name}</span>
										</div>
									</td>
									<td className="px-4 py-3 text-gray-500 font-mono text-xs">
										{vk.key_preview}
									</td>
									<td className="px-4 py-3 text-sm font-mono">
										{vk.rate_limit_rps != null ? (
											<span className="text-gray-200">{vk.rate_limit_rps}</span>
										) : (
											<span className="text-gray-500">Global</span>
										)}
									</td>
									<td className="px-4 py-3 text-sm font-mono">
										{vk.rate_limit_burst != null ? (
											<span className="text-gray-200">
												{vk.rate_limit_burst}
											</span>
										) : (
											<span className="text-gray-500">Global</span>
										)}
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
				<div className="ui-card p-6 space-y-5">
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

					<div className="space-y-4">
						<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
							<TerminalPreview
								variant="code"
								title="cURL"
								icon="curl"
								copyText={snippetBashText({ origin: proxyOrigin })}
							>
								{snippetBash({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title="PowerShell"
								icon="powershell"
								copyText={snippetPowershellText({ origin: proxyOrigin })}
							>
								{snippetPowershell({ origin: proxyOrigin })}
							</TerminalPreview>
						</div>

						<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
							<TerminalPreview
								variant="code"
								title="Python"
								icon="python"
								copyText={snippetPythonText({ origin: proxyOrigin })}
							>
								{snippetPython({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title="OpenClaw"
								icon="openclaw"
								copyText={snippetOpenClawText({ origin: proxyOrigin })}
							>
								{snippetOpenClaw({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title="JavaScript"
								icon="javascript"
								copyText={snippetJSText({ origin: proxyOrigin })}
							>
								{snippetJS({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title="LibreChat"
								icon="librechat"
								copyText={snippetLibreChatText({ origin: proxyOrigin })}
							>
								{snippetLibreChat({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title="Claude Code"
								icon="claude"
								copyText={snippetClaudeCodeText({ origin: proxyOrigin })}
							>
								{snippetClaudeCode({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title="ZED"
								icon="zed"
								copyText={snippetZedVKText({ origin: proxyOrigin })}
							>
								{snippetZedVK({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title="Hermes"
								icon="hermes"
								copyText={snippetHermesText({ origin: proxyOrigin })}
							>
								{snippetHermes({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title="OpenCode"
								icon="opencode"
								copyText={snippetOpencodeVKText({ origin: proxyOrigin })}
							>
								{snippetOpencodeVK({ origin: proxyOrigin })}
							</TerminalPreview>
						</div>
					</div>

					<div className="flex items-start gap-3 p-4 rounded-lg bg-(--accent-light) border border-(--accent-lighter)">
						<div className="w-1.5 h-1.5 rounded-full bg-(--accent) mt-1.5 shrink-0" />
						<p className="text-xs text-gray-300 leading-relaxed">
							<span className="text-gray-200 font-medium">Note:</span> Virtual
							keys are used to authenticate requests to the proxy. Each key
							tracks its own token usage. You can create multiple keys for
							different clients or environments.
						</p>
					</div>
				</div>
			)}

			{showCreate && (
				<CreateKeyModal onClose={() => setShowCreate(false)} onToast={toast} />
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
