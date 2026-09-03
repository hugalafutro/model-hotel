import { useQuery } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Brain, KeyRound, ShieldCheck, ShieldOff } from "@/lib/icons";
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
import { FilterInput } from "../../components/FilterInput";
import { LoadingSpinner } from "../../components/LoadingSpinner";
import { ManagedBanner } from "../../components/ManagedBanner";
import { PageHeader } from "../../components/PageHeader";
import { useToast } from "../../context/ToastContext";
import { useManaged } from "../../hooks/useManaged";
import { useReadOnly } from "../../hooks/useReadOnly";
import { useWheelPaging } from "../../hooks/useWheelPaging";
import { formatNumber, formatRelativeTime } from "../../utils/format";
import { CreateKeyModal } from "./CreateKeyModal";
import { KeyDetailModal } from "./KeyDetailModal";
import { UsageSnippets } from "./UsageSnippets";

type VKSortField =
	| "name"
	| "rps"
	| "burst"
	| "tpm"
	| "created"
	| "tokens"
	| "last_used";

export function VirtualKeys() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const readOnly = useReadOnly();
	const managed = useManaged();
	const [showCreate, setShowCreate] = useState(false);
	const [selectedKey, setSelectedKey] = useState<VirtualKey | null>(null);
	const [sort, setSort] = useState<SortState<VKSortField>>({
		field: "name",
		dir: "asc",
	});
	const [nameFilter, setNameFilter] = useState("");
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

	const handleFilter = useCallback((value: string) => {
		setNameFilter(value);
		setCurrentPage(1);
	}, []);

	const proxyOrigin =
		typeof window !== "undefined"
			? window.location.origin
			: "http://localhost:8080";

	const sortedKeys = useMemo(() => {
		if (!keys) return [];
		const dir = sort.dir === "asc" ? 1 : -1;
		const needle = nameFilter.trim().toLowerCase();
		const filtered = needle
			? keys.filter((k) => k.name.toLowerCase().includes(needle))
			: keys;
		return [...filtered].sort((a, b) => {
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
				case "tpm": {
					const aT = a.rate_limit_tpm ?? 0;
					const bT = b.rate_limit_tpm ?? 0;
					return dir * (aT - bT);
				}
				default:
					return 0;
			}
		});
	}, [keys, sort, nameFilter]);

	const hasKeys = (keys?.length ?? 0) > 0;
	const totalPages = Math.ceil(sortedKeys.length / pageSize);
	const paginatedKeys = sortedKeys.slice(
		(currentPage - 1) * pageSize,
		currentPage * pageSize,
	);
	const wheelPagingRef = useWheelPaging<HTMLDivElement>({
		enabled: totalPages > 1,
		canPrev: currentPage > 1,
		canNext: currentPage < totalPages,
		onPrev: () => setCurrentPage(currentPage - 1),
		onNext: () => setCurrentPage(currentPage + 1),
	});

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
					!readOnly &&
					!managed && (
						<button
							type="button"
							onClick={() => setShowCreate(true)}
							className="ui-btn ui-btn-primary"
						>
							{t("virtualkeys.createButton")}
						</button>
					)
				}
			/>

			<ManagedBanner />

			{hasKeys && (
				<div className="flex items-center justify-between gap-2">
					<FilterInput
						value={nameFilter}
						onChange={handleFilter}
						placeholder={t("virtualkeys.filterPlaceholder")}
						className="w-[200px]"
					/>
					{sortedKeys.length > 0 && (
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
					)}
				</div>
			)}

			{sortedKeys.length > 0 ? (
				<div ref={wheelPagingRef} className="ui-card overflow-hidden">
					<table className="w-full table-fixed ui-table">
						<colgroup>
							<col className="w-[20%]" />
							<col className="w-[14%]" />
							<col className="w-[8%]" />
							<col className="w-[8%]" />
							<col className="w-[8%]" />
							<col className="w-[16%]" />
							<col className="w-[14%]" />
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
									label={t("virtualkeys.table.tpm")}
									field="tpm"
									sort={sort}
									onSort={handleSort}
									tooltip={t("virtualkeys.tooltip.tpm")}
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
									{/* tooltip lives on the inner name span so it fires on
									    overflow, not on the icon-only regions of the cell */}
									<td className="px-4 py-3 text-sm text-gray-200 truncate overflow-hidden text-ellipsis max-w-0">
										<div className="flex items-center gap-1.5">
											{/* The marker is gated on the LIST'S PRESENCE, never its
											    length. A non-NULL allowed_providers restricts the key to
											    exactly its members, so an EMPTY one denies every provider
											    (effectiveAllowedProviders in internal/proxy). That state is
											    reached by ordinary admin action: deleting the last provider
											    a key was scoped to prunes the stored list to `{}` (see
											    provider.PruneAllowLists). Length-gating it left the single
											    most restricted key on the page looking unrestricted, which
											    is the opposite of what an operator chasing a 403 needs.
											    It gets its own icon rather than the plain shield, matching
											    how the Users page renders the same three-state cap. */}
											{vk.allowed_providers &&
												(vk.allowed_providers.length === 0 ? (
													<span
														title={t("virtualkeys.tooltip.providerDenyAll")}
														data-testid={`vk-provider-access-${vk.id}`}
														data-provider-access="none"
													>
														<ShieldOff
															size={14}
															className="text-red-400 shrink-0"
														/>
													</span>
												) : (
													<span
														title={t("virtualkeys.tooltip.providerRestricted")}
														data-testid={`vk-provider-access-${vk.id}`}
														data-provider-access="selected"
													>
														<ShieldCheck
															size={14}
															className="text-(--accent) shrink-0"
														/>
													</span>
												))}
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
											<span className="truncate" title={vk.name}>
												{vk.name}
											</span>
											{vk.owner_username && (
												<span
													className="ui-badge ui-badge-neutral text-[10px] shrink-0"
													title={t("virtualkeys.tooltip.owner", {
														name: vk.owner_username,
													})}
													data-testid="vk-owner-chip"
												>
													{vk.owner_username}
												</span>
											)}
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
									<td className="px-4 py-3 text-sm font-mono">
										{vk.rate_limit_tpm != null ? (
											<span className="text-gray-200">{vk.rate_limit_tpm}</span>
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
				<EmptyState
					message={t(
						hasKeys ? "virtualkeys.noMatch" : "virtualkeys.emptyState",
					)}
				/>
			)}

			{hasKeys && (
				<>
					<div className="ui-note-pill flex items-start gap-3 p-4 rounded-lg bg-(--accent-light) border border-(--accent-lighter)">
						<div className="w-1.5 h-1.5 rounded-(--radius-pill) bg-(--accent) mt-1.5 shrink-0" />
						<p className="text-xs text-gray-300 leading-relaxed">
							{t("virtualkeys.note.text")}
						</p>
					</div>

					<div className="ui-card p-6 space-y-5">
						<div className="grid grid-cols-1 md:grid-cols-3 gap-3">
							<div className="flex items-start gap-3 p-4 ui-card">
								<div className="flex items-center justify-center w-7 h-7 rounded-(--radius-pill) bg-(--accent)/15 text-(--accent) ring-1 ring-(--accent)/30 text-sm font-bold shrink-0">
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
								<div className="flex items-center justify-center w-7 h-7 rounded-(--radius-pill) bg-(--accent)/15 text-(--accent) ring-1 ring-(--accent)/30 text-sm font-bold shrink-0">
									2
								</div>
								<div>
									<h3 className="text-sm font-medium text-gray-200">
										{t("virtualkeys.steps.copyKey")}
									</h3>
									<p className="text-xs text-(--warning-text) mt-1">
										{t("virtualkeys.stepDescriptions.copyKey")}
									</p>
								</div>
							</div>
							<div className="flex items-start gap-3 p-4 ui-card">
								<div className="flex items-center justify-center w-7 h-7 rounded-(--radius-pill) bg-(--accent)/15 text-(--accent) ring-1 ring-(--accent)/30 text-sm font-bold shrink-0">
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

						<UsageSnippets />
					</div>
				</>
			)}

			{showCreate && (
				<CreateKeyModal onClose={() => setShowCreate(false)} onToast={toast} />
			)}

			{selectedKey && (
				<KeyDetailModal
					vk={selectedKey}
					managed={managed}
					onClose={() => setSelectedKey(null)}
					onToast={toast}
				/>
			)}
		</div>
	);
}
