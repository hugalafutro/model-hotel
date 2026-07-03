import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { History } from "@/lib/icons";
import { api } from "../../api/client";
import type { AuditEntry } from "../../api/types";
import { Badge } from "../../components/Badge";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { StaticHeader } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { FilterDropdown } from "../../components/FilterDropdown";
import { FilterInput } from "../../components/FilterInput";
import { LoadingSpinner } from "../../components/LoadingSpinner";
import { PageHeader } from "../../components/PageHeader";
import { useToast } from "../../context/ToastContext";
import { useDebounce } from "../../hooks/useDebounce";
import { formatRelativeTime } from "../../utils/format";

const METHODS = ["POST", "PUT", "PATCH", "DELETE"] as const;

function statusVariant(code: number): "success" | "warning" | "error" {
	if (code >= 500) return "error";
	if (code >= 400) return "warning";
	return "success";
}

/**
 * Admin-only audit trail: who did what on the dashboard API, newest first,
 * with actor/method filters and cursor-based "load more" pagination. The
 * server records mutations only and never stores request bodies.
 */
export function Audit() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const [actor, setActor] = useState("");
	const [method, setMethod] = useState("");
	const [confirmPurge, setConfirmPurge] = useState(false);
	// Pages loaded past the first are appended here; each "Load more" click
	// fetches exactly one page and merges it, rather than replaying cursors.
	const [extra, setExtra] = useState<{
		entries: AuditEntry[];
		hasMore: boolean;
		nextCursor: string;
	} | null>(null);
	const debouncedActor = useDebounce(actor, 300);

	const resetPaging = () => setExtra(null);

	const { data: firstPage, isLoading } = useQuery({
		queryKey: ["audit", debouncedActor, method],
		queryFn: () =>
			api.audit.list({
				actor: debouncedActor || undefined,
				method: method || undefined,
				limit: 50,
			}),
	});

	const entries: AuditEntry[] = [
		...(firstPage?.entries ?? []),
		...(extra?.entries ?? []),
	];
	const hasMore = extra ? extra.hasMore : (firstPage?.has_more ?? false);
	const nextCursor = extra ? extra.nextCursor : (firstPage?.next_cursor ?? "");
	const total = firstPage?.total ?? 0;

	// One fetch per click: append the returned page to the accumulated list.
	const loadMore = useMutation({
		mutationFn: (cursor: string) =>
			api.audit.list({
				actor: debouncedActor || undefined,
				method: method || undefined,
				limit: 50,
				cursor,
			}),
		onSuccess: (page) =>
			setExtra((prev) => ({
				entries: [...(prev?.entries ?? []), ...page.entries],
				hasMore: page.has_more,
				nextCursor: page.next_cursor ?? "",
			})),
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
		<div className="space-y-6 pb-8">
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
					<div className="ui-card overflow-hidden">
						<table className="w-full table-fixed ui-table">
							<colgroup>
								<col className="w-[14%]" />
								<col className="w-[16%]" />
								<col className="w-[10%]" />
								<col className="w-[34%]" />
								<col className="w-[18%]" />
								<col className="w-[8%]" />
							</colgroup>
							<thead>
								<tr>
									<StaticHeader>{t("audit.table.time")}</StaticHeader>
									<StaticHeader>{t("audit.table.actor")}</StaticHeader>
									<StaticHeader>{t("audit.table.method")}</StaticHeader>
									<StaticHeader>{t("audit.table.action")}</StaticHeader>
									<StaticHeader>{t("audit.table.entity")}</StaticHeader>
									<StaticHeader>{t("audit.table.status")}</StaticHeader>
								</tr>
							</thead>
							<tbody>
								{entries.map((e) => (
									<tr key={e.id}>
										<td
											className="px-4 py-3 text-sm text-gray-400"
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
											<Badge variant={e.method === "DELETE" ? "error" : "info"}>
												{e.method}
											</Badge>
										</td>
										<td
											className="px-4 py-3 text-sm text-gray-300 font-mono truncate"
											title={e.path}
										>
											{e.route}
										</td>
										<td className="px-4 py-3 text-sm text-gray-400 font-mono truncate">
											{e.entity_id ? (
												<span title={e.entity_id}>
													{e.entity_id.slice(0, 8)}…
												</span>
											) : (
												"—"
											)}
										</td>
										<td className="px-4 py-3">
											<Badge variant={statusVariant(e.status_code)}>
												{e.status_code}
											</Badge>
										</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>
					<div className="flex items-center justify-between text-sm text-gray-500">
						<span>{t("audit.showing", { count: entries.length, total })}</span>
						{hasMore && nextCursor && (
							<button
								type="button"
								onClick={() => loadMore.mutate(nextCursor)}
								disabled={loadMore.isPending}
								className="ui-btn"
								data-testid="audit-load-more"
							>
								{t("audit.loadMore")}
							</button>
						)}
					</div>
				</>
			) : (
				<EmptyState message={t("audit.emptyState")} />
			)}

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
