import {
	keepPreviousData,
	useInfiniteQuery,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { History } from "@/lib/icons";
import { api } from "../../api/client";
import type { AuditEntry } from "../../api/types";
import { AuditDetailModal } from "../../components/AuditDetailModal";
import {
	auditMethodVariant,
	auditStatusVariant,
} from "../../components/auditUtils";
import { Badge } from "../../components/Badge";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { PaginationBar, Row, StaticHeader } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { FilterDropdown } from "../../components/FilterDropdown";
import { FilterInput } from "../../components/FilterInput";
import { LoadingSpinner } from "../../components/LoadingSpinner";
import { ViewModeToggle } from "../../components/logs/ViewModeToggle";
import { PageHeader } from "../../components/PageHeader";
import { useToast } from "../../context/ToastContext";
import { useDebounce } from "../../hooks/useDebounce";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import { formatRelativeTime } from "../../utils/format";

const METHODS = ["POST", "PUT", "PATCH", "DELETE"] as const;
const PAGE_SIZE = 50;

/**
 * Admin-only audit trail: who did what on the dashboard API, newest first, with
 * actor/method filters. Two viewing modes matching the Logs pages: infinite
 * scroll (default) that appends pages as you reach the bottom, and a paginated
 * static table. The server records mutations only and never stores request
 * bodies.
 */
export function Audit() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const [actor, setActor] = useState("");
	const [method, setMethod] = useState("");
	const [confirmPurge, setConfirmPurge] = useState(false);
	const [selected, setSelected] = useState<AuditEntry | null>(null);
	const [viewMode, setViewMode] = useLocalStorage<"paginate" | "scroll">(
		"auditViewMode",
		"scroll",
	);
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useState(PAGE_SIZE);
	const debouncedActor = useDebounce(actor, 300);
	const isScroll = viewMode === "scroll";

	const filters = {
		actor: debouncedActor || undefined,
		method: method || undefined,
	};

	// Infinite-scroll mode: keyset-cursor pagination, one page per fetch, appended.
	// The cursor (last row's created_at+id) is stable under inserts, so a new audit
	// row landing at the top mid-scroll never shifts the window - unlike offset,
	// which would duplicate or skip rows on this newest-first, actively-written log.
	const scroll = useInfiniteQuery({
		queryKey: ["audit", "scroll", debouncedActor, method],
		queryFn: ({ pageParam }) =>
			api.audit.list({
				...filters,
				limit: PAGE_SIZE,
				cursor: pageParam || undefined,
			}),
		initialPageParam: "",
		getNextPageParam: (lastPage) =>
			lastPage.has_more ? (lastPage.next_cursor ?? undefined) : undefined,
		enabled: isScroll,
	});

	// Pagination mode: a single offset page, keeping the previous page visible
	// while the next one loads so the table does not blank out on navigation.
	const paginated = useQuery({
		queryKey: ["audit", "page", debouncedActor, method, page, pageSize],
		queryFn: () =>
			api.audit.list({
				...filters,
				limit: pageSize,
				offset: (page - 1) * pageSize,
			}),
		enabled: !isScroll,
		placeholderData: keepPreviousData,
	});

	const entries: AuditEntry[] = isScroll
		? (scroll.data?.pages.flatMap((p) => p.entries) ?? [])
		: (paginated.data?.entries ?? []);
	const total = isScroll
		? (scroll.data?.pages[0]?.total ?? 0)
		: (paginated.data?.total ?? 0);
	const isLoading = isScroll ? scroll.isLoading : paginated.isLoading;
	const totalPages = Math.max(1, Math.ceil(total / pageSize));

	// Any filter change restarts both views: the scroll query re-keys itself, and
	// the paginated view jumps back to page one.
	const resetPaging = () => setPage(1);

	// Sentinel at the list foot: when it scrolls into view (scroll mode only),
	// pull the next page. Re-armed whenever the fetch state changes so it keeps
	// firing down a long list.
	const scrollRef = useRef<HTMLDivElement>(null);
	const sentinelRef = useRef<HTMLDivElement>(null);
	const { hasNextPage, isFetchingNextPage, fetchNextPage } = scroll;
	useEffect(() => {
		if (!isScroll) return;
		const el = sentinelRef.current;
		if (!el) return;
		// Root is the table's own scroll box, not the viewport: the page is pinned
		// to the viewport height and only the table body scrolls, so intersection
		// must be measured against that inner scroller. rootMargin pulls the next
		// page in a little before the foot is actually reached.
		const observer = new IntersectionObserver(
			(observed) => {
				if (observed[0]?.isIntersecting && hasNextPage && !isFetchingNextPage) {
					fetchNextPage();
				}
			},
			{ root: scrollRef.current, rootMargin: "300px" },
		);
		observer.observe(el);
		return () => observer.disconnect();
	}, [isScroll, hasNextPage, isFetchingNextPage, fetchNextPage]);

	const handlePurge = async () => {
		setConfirmPurge(false);
		try {
			await api.audit.purge("all");
			resetPaging();
			queryClient.invalidateQueries({ queryKey: ["audit"] });
			toast(t("audit.toast.purged"), "success");
		} catch {
			toast(t("audit.toast.purgeFailed"), "error");
		}
	};

	if (isLoading) {
		return <LoadingSpinner />;
	}

	return (
		<div
			className={`space-y-6 flex flex-col ${
				isScroll ? "h-[calc(100dvh-1rem)] overflow-hidden" : "flex-1 min-h-0"
			}`}
		>
			<PageHeader
				icon={History}
				title={t("audit.title")}
				description={t("audit.description")}
				actions={
					<div className="flex items-center gap-2">
						<FilterInput
							value={actor}
							onChange={(v) => {
								setActor(v);
								resetPaging();
							}}
							placeholder={t("audit.filters.actorPlaceholder")}
							className="w-44"
						/>
						<FilterDropdown
							value={method}
							onChange={(v) => {
								setMethod(v);
								resetPaging();
							}}
							placeholder={t("audit.filters.method")}
							allLabel={t("audit.filters.allMethods")}
							options={METHODS.map((m) => ({ value: m, label: m }))}
							className="w-32"
						/>
						<ViewModeToggle viewMode={viewMode} onChange={setViewMode} />
						<button
							type="button"
							onClick={() => setConfirmPurge(true)}
							className="ui-btn ui-btn-danger"
							data-testid="audit-purge-button"
						>
							{t("audit.purgeButton")}
						</button>
					</div>
				}
			/>

			{entries.length > 0 ? (
				<>
					<div
						ref={scrollRef}
						className="ui-card overflow-y-auto flex-1 min-h-0"
					>
						<table className="w-full table-fixed ui-table">
							<colgroup>
								<col className="w-[9%]" />
								<col className="w-[13%]" />
								<col className="w-[7%]" />
								<col className="w-[27%]" />
								<col className="w-[24%]" />
								<col className="w-[13%]" />
								<col className="w-[7%]" />
							</colgroup>
							<thead className="sticky top-0 z-10">
								<tr>
									<StaticHeader>{t("audit.table.time")}</StaticHeader>
									<StaticHeader>{t("audit.table.actor")}</StaticHeader>
									<StaticHeader>{t("audit.table.method")}</StaticHeader>
									<StaticHeader>{t("audit.table.action")}</StaticHeader>
									<StaticHeader>{t("audit.table.entity")}</StaticHeader>
									<StaticHeader>{t("audit.table.remote")}</StaticHeader>
									<StaticHeader>{t("audit.table.status")}</StaticHeader>
								</tr>
							</thead>
							<tbody>
								{entries.map((e) => (
									<Row key={e.id} onClick={() => setSelected(e)}>
										<td
											className="px-4 py-3 text-sm text-gray-400 whitespace-nowrap"
											title={new Date(e.created_at).toLocaleString()}
										>
											{formatRelativeTime(e.created_at)}
										</td>
										<td className="px-4 py-3 text-sm text-gray-200 truncate">
											<span title={e.actor}>{e.actor}</span>
											{e.actor_role === "admin" && (
												<span className="ml-1.5 text-xs text-gray-500">
													{t("users.role.admin")}
												</span>
											)}
										</td>
										<td className="px-4 py-3">
											<Badge variant={auditMethodVariant(e.method)}>
												{e.method}
											</Badge>
										</td>
										<td
											className="px-4 py-3 text-sm text-gray-300 font-mono truncate"
											title={e.path}
										>
											{e.route}
										</td>
										{/* Resolved name when the entity still exists; its UUID
										   (truncated) as the fallback trace when it does not. */}
										<td className="px-4 py-3 text-sm text-gray-400 truncate">
											{e.entity_name ? (
												<span title={e.entity_id}>{e.entity_name}</span>
											) : e.entity_id ? (
												<span className="font-mono" title={e.entity_id}>
													{e.entity_id.slice(0, 8)}…
												</span>
											) : (
												"—"
											)}
										</td>
										<td
											className="px-4 py-3 text-sm text-gray-400 font-mono truncate"
											title={e.remote_addr}
										>
											{e.remote_addr}
										</td>
										<td className="px-4 py-3">
											<Badge variant={auditStatusVariant(e.status_code)}>
												{e.status_code}
											</Badge>
										</td>
									</Row>
								))}
							</tbody>
						</table>
						{/* Foot marker the observer watches (scroll mode). It lives inside
						   the scroll box so it enters view as the table body scrolls, not
						   the page. */}
						{isScroll && (
							<div ref={sentinelRef} aria-hidden="true" className="h-px" />
						)}
					</div>

					<div className="flex items-center justify-between text-sm text-gray-500 shrink-0">
						<span>{t("audit.showing", { count: entries.length, total })}</span>
						{isScroll ? (
							isFetchingNextPage && (
								<span
									role="status"
									aria-label={t("common.loading")}
									className="animate-spin rounded-full h-4 w-4 border-b-2 border-(--accent)"
								/>
							)
						) : (
							<PaginationBar
								page={page}
								totalPages={totalPages}
								totalItems={total}
								pageSize={pageSize}
								onPageChange={setPage}
								onPageSizeChange={(s) => {
									setPageSize(s);
									setPage(1);
								}}
								hideCount
							/>
						)}
					</div>
				</>
			) : (
				<EmptyState message={t("audit.emptyState")} />
			)}

			<AuditDetailModal entry={selected} onClose={() => setSelected(null)} />

			{confirmPurge && (
				<ConfirmDialog
					title={t("audit.purgeConfirmTitle")}
					message={t("audit.purgeConfirmMessage")}
					fields={[]}
					onConfirm={handlePurge}
					onCancel={() => setConfirmPurge(false)}
					confirmTestId="audit-purge-confirm"
				/>
			)}
		</div>
	);
}
