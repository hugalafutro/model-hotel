import {
	keepPreviousData,
	useInfiniteQuery,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useState } from "react";
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
import { PageHeader } from "../../components/PageHeader";
import { ScrollTopButton } from "../../components/ScrollTopButton";
import { TableFooter } from "../../components/TableFooter";
import { ViewModeToggle } from "../../components/ViewModeToggle";
import { useToast } from "../../context/ToastContext";
import { useDebounce } from "../../hooks/useDebounce";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import { useModalNav } from "../../hooks/useModalNav";
import { useVirtualRows } from "../../hooks/useVirtualRows";
import { onActivateKey } from "../../utils/a11y";
import { formatRelativeTime } from "../../utils/format";

const METHODS = ["POST", "PUT", "PATCH", "DELETE"] as const;
const PAGE_SIZE = 50;

/**
 * Admin-only audit trail: who did what on the dashboard API, newest first, with
 * actor/method filters. Two viewing modes matching the Logs pages: infinite
 * scroll (default), a virtual table that appends pages as you reach the
 * bottom, and a paginated static table. The server records mutations only and never stores request
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
		// A new filter is a new key: without the previous pages standing in, the
		// page collapses to a spinner and the filter input the user is typing in
		// is unmounted under them.
		placeholderData: keepPreviousData,
		enabled: isScroll,
	});

	// Pagination mode: a single offset page, keeping the previous page visible
	// while the next one loads so the table does not blank out on navigation.
	// The page carries the offset it was fetched at: while the next page loads,
	// `page` has already moved on but the rows on screen are still the old ones.
	const paginated = useQuery({
		queryKey: ["audit", "page", debouncedActor, method, page, pageSize],
		queryFn: async () => {
			const offset = (page - 1) * pageSize;
			const res = await api.audit.list({ ...filters, limit: pageSize, offset });
			return { ...res, offset };
		},
		enabled: !isScroll,
		placeholderData: keepPreviousData,
	});

	const entries: AuditEntry[] = isScroll
		? (scroll.data?.pages.flatMap((p) => p.entries) ?? [])
		: (paginated.data?.entries ?? []);
	const total = isScroll
		? (scroll.data?.pages[0]?.total ?? 0)
		: (paginated.data?.total ?? 0);
	// Where the paged view's rows start: the offset they were fetched at.
	const shownOffset = paginated.data?.offset ?? 0;
	const isLoading = isScroll ? scroll.isLoading : paginated.isLoading;
	const auditNav = useModalNav(entries, selected, setSelected, (e) => e.id);
	const totalPages = Math.max(1, Math.ceil(total / pageSize));

	// Any filter change restarts both views: the scroll query re-keys itself, and
	// the paginated view jumps back to page one.
	const resetPaging = () => setPage(1);

	// Scroll mode renders through the same virtual-row hook as the log tables:
	// only the rows near the viewport mount, the footer names the rows on
	// screen, the back-to-top button appears, and reaching the foot pulls the
	// next page. Placeholder pages belong to the previous filter, so their
	// cursor must not fetch a "next" page for the new one.
	const { hasNextPage, isFetchingNextPage, fetchNextPage, isPlaceholderData } =
		scroll;
	const {
		scrollRef,
		scrollEl,
		virtualizer,
		virtualItems,
		paddingTop,
		paddingBottom,
		handleScroll,
		startIndex,
		endIndex,
	} = useVirtualRows({
		entries: isScroll ? entries : [],
		// A new filter's first page replaces the list: back to the top then.
		listVersion: isPlaceholderData
			? "placeholder"
			: `${debouncedActor}|${method}`,
		hasBefore: false,
		hasAfter: isScroll && !!hasNextPage && !isPlaceholderData,
		isLoadingBefore: false,
		isLoadingAfter: isFetchingNextPage,
		fetchNewer: () => {},
		fetchOlder: () => {
			fetchNextPage();
		},
		estimateSize: 49,
	});

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
			className={`space-y-6 flex flex-col flex-1 min-h-0 ${
				isScroll ? "overflow-hidden" : ""
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
				<div className="relative flex flex-col flex-1 min-h-0">
					<div
						ref={isScroll ? scrollRef : undefined}
						// Focus target for ScrollTopButton, so returning to the top does
						// not drop keyboard focus to <body>.
						tabIndex={isScroll ? -1 : undefined}
						className="ui-card overflow-y-auto flex-1 min-h-0"
						style={isScroll ? { overflowAnchor: "none" } : undefined}
						onScroll={isScroll ? handleScroll : undefined}
					>
						<table
							className={`w-full table-fixed ui-table ${isScroll ? "ui-table-virtual" : ""}`}
							style={
								isScroll
									? { marginTop: paddingTop, marginBottom: paddingBottom }
									: undefined
							}
						>
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
								{isScroll
									? virtualItems.map((vItem) => {
											const e = entries[vItem.index];
											return (
												<tr
													key={vItem.key}
													data-index={vItem.index}
													ref={virtualizer.measureElement}
													className={`hover:bg-(--surface-hover) cursor-pointer ${vItem.index % 2 === 1 ? "ui-row-even" : ""}`}
													tabIndex={0}
													onClick={() => setSelected(e)}
													onKeyDown={onActivateKey(() => setSelected(e))}
												>
													<AuditCells entry={e} />
												</tr>
											);
										})
									: entries.map((e) => (
											<Row key={e.id} onClick={() => setSelected(e)}>
												<AuditCells entry={e} />
											</Row>
										))}
							</tbody>
						</table>
					</div>
					{isScroll && <ScrollTopButton scrollEl={scrollEl} />}

					<TableFooter
						start={isScroll ? startIndex : shownOffset + 1}
						end={isScroll ? endIndex : shownOffset + entries.length}
						total={total}
					>
						{isScroll ? (
							isFetchingNextPage && <LoadingSpinner inline />
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
					</TableFooter>
				</div>
			) : (
				<EmptyState message={t("audit.emptyState")} />
			)}

			<AuditDetailModal
				entry={selected}
				nav={auditNav}
				onClose={() => setSelected(null)}
			/>

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

/** One audit row's cells, shared by the virtual (scroll) and paged tables. */
function AuditCells({ entry: e }: { entry: AuditEntry }) {
	const { t } = useTranslation();
	return (
		<>
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
				<Badge variant={auditMethodVariant(e.method)}>{e.method}</Badge>
			</td>
			<td
				className="px-4 py-3 text-sm text-gray-300 font-mono truncate"
				title={e.path}
			>
				{e.route}
			</td>
			{/* Resolved name when the entity still exists; its full UUID as the
			   fallback trace when it does not. The cell clips with an ellipsis
			   only once the column runs out of room. */}
			<td className="px-4 py-3 text-sm text-gray-400 truncate">
				{e.entity_name ? (
					<span title={e.entity_id}>{e.entity_name}</span>
				) : e.entity_id ? (
					<span className="font-mono" title={e.entity_id}>
						{e.entity_id}
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
		</>
	);
}
