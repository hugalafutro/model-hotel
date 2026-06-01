import { useQuery } from "@tanstack/react-query";
import { Brain, KeyRound, ShieldCheck } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
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
import { formatNumber, formatRelativeTime } from "../../utils/format";
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
	const { t } = useTranslation();
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
				title={t("virtualkeys.title.plural")}
				description={
					<span>
						{t("virtualkeys.description")}{" "}
						<CopyablePill
							text={`${proxyOrigin}/v1`}
							displayText={`${proxyOrigin}/v1`}
							textClassName="text-(--accent) text-sm font-medium"
							iconClassName="w-3 h-3"
							className="inline-flex"
							tooltip={t("virtualkeys.tooltip.proxyUrl")}
						/>
					</span>
				}
				actions={
					<button
						type="button"
						onClick={() => setShowCreate(true)}
						className="ui-btn ui-btn-primary"
					>
						{t("virtualkeys.createButton")}
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
						label={t("virtualkeys.table.keys")}
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
									label={t("virtualkeys.table.name")}
									field="name"
									sort={sort}
									onSort={handleSort}
									tooltip={t("virtualkeys.tooltip.name")}
								/>
								<StaticHeader tooltip={t("virtualkeys.tooltip.key")}>
									{t("virtualkeys.table.key")}
								</StaticHeader>
								<SortableHeader
									label={t("virtualkeys.table.rps")}
									field="rps"
									sort={sort}
									onSort={handleSort}
									tooltip={t("virtualkeys.tooltip.rps")}
								/>
								<SortableHeader
									label={t("virtualkeys.table.burst")}
									field="burst"
									sort={sort}
									onSort={handleSort}
									tooltip={t("virtualkeys.tooltip.burst")}
								/>
								<SortableHeader
									label={t("virtualkeys.table.created")}
									field="created"
									sort={sort}
									onSort={handleSort}
									tooltip={t("virtualkeys.tooltip.created")}
								/>
								<SortableHeader
									label={t("virtualkeys.table.tokens")}
									field="tokens"
									sort={sort}
									onSort={handleSort}
									tooltip={t("virtualkeys.tooltip.tokens")}
								/>
								<SortableHeader
									label={t("virtualkeys.table.lastUsed")}
									field="last_used"
									sort={sort}
									onSort={handleSort}
									tooltip={t("virtualkeys.tooltip.lastUsed")}
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
													<span
														title={t("virtualkeys.tooltip.providerRestricted")}
													>
														<ShieldCheck
															size={14}
															className="text-(--accent) shrink-0"
														/>
													</span>
												)}
											{vk.strip_reasoning && (
												<span
													title={t("virtualkeys.tooltip.reasoningStripped")}
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
														<title>
															{t("virtualkeys.tooltip.reasoningStripped")}
														</title>
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
											<span className="text-gray-500">
												{t("virtualKeys.global")}
											</span>
										)}
									</td>
									<td className="px-4 py-3 text-sm font-mono">
										{vk.rate_limit_burst != null ? (
											<span className="text-gray-200">
												{vk.rate_limit_burst}
											</span>
										) : (
											<span className="text-gray-500">
												{t("virtualKeys.global")}
											</span>
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
				<EmptyState message={t("virtualkeys.emptyState")} />
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
									{t("virtualkeys.steps.createKey")}
								</h3>
								<p className="text-xs text-gray-400 mt-1">
									{t("virtualkeys.stepDescriptions.createKey")}
								</p>
							</div>
						</div>
						<div className="flex items-start gap-3 p-4 ui-card">
							<div className="flex items-center justify-center w-7 h-7 rounded-lg bg-(--accent-light) text-(--accent) text-sm font-bold shrink-0">
								2
							</div>
							<div>
								<h3 className="text-sm font-medium text-gray-200">
									{t("virtualkeys.steps.copyKey")}
								</h3>
								<p className="text-xs text-gray-400 mt-1">
									{t("virtualkeys.stepDescriptions.copyKey")}
								</p>
							</div>
						</div>
						<div className="flex items-start gap-3 p-4 ui-card">
							<div className="flex items-center justify-center w-7 h-7 rounded-lg bg-(--accent-light) text-(--accent) text-sm font-bold shrink-0">
								3
							</div>
							<div>
								<h3 className="text-sm font-medium text-gray-200">
									{t("virtualkeys.steps.makeRequests")}
								</h3>
								<p className="text-xs text-gray-400 mt-1">
									{t("virtualkeys.stepDescriptions.makeRequests")}
								</p>
							</div>
						</div>
					</div>

					<div className="space-y-4">
						<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
							<TerminalPreview
								variant="code"
								title={t("virtualKeys.snippet.curl")}
								icon="curl"
								copyText={snippetBashText({ origin: proxyOrigin })}
							>
								{snippetBash({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title={t("virtualKeys.snippet.powershell")}
								icon="powershell"
								copyText={snippetPowershellText({ origin: proxyOrigin })}
							>
								{snippetPowershell({ origin: proxyOrigin })}
							</TerminalPreview>
						</div>

						<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
							<TerminalPreview
								variant="code"
								title={t("virtualKeys.snippet.python")}
								icon="python"
								copyText={snippetPythonText({ origin: proxyOrigin })}
							>
								{snippetPython({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title={t("virtualKeys.snippet.openclaw")}
								icon="openclaw"
								copyText={snippetOpenClawText({ origin: proxyOrigin })}
							>
								{snippetOpenClaw({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title={t("virtualKeys.snippet.javascript")}
								icon="javascript"
								copyText={snippetJSText({ origin: proxyOrigin })}
							>
								{snippetJS({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title={t("virtualKeys.snippet.librechat")}
								icon="librechat"
								copyText={snippetLibreChatText({ origin: proxyOrigin })}
							>
								{snippetLibreChat({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title={t("virtualKeys.snippet.claudeCode")}
								icon="claude"
								copyText={snippetClaudeCodeText({ origin: proxyOrigin })}
							>
								{snippetClaudeCode({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title={t("virtualKeys.snippet.zed")}
								icon="zed"
								copyText={snippetZedVKText({ origin: proxyOrigin })}
							>
								{snippetZedVK({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title={t("virtualKeys.snippet.hermes")}
								icon="hermes"
								copyText={snippetHermesText({ origin: proxyOrigin })}
							>
								{snippetHermes({ origin: proxyOrigin })}
							</TerminalPreview>

							<TerminalPreview
								variant="code"
								title={t("virtualKeys.snippet.opencode")}
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
							{t("virtualkeys.note.text")}
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
