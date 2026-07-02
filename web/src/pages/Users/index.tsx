import { useQuery } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Users as UsersIcon } from "@/lib/icons";
import { api } from "../../api/client";
import type { DashboardUser } from "../../api/types";
import { Badge } from "../../components/Badge";
import type { SortState } from "../../components/DataTable";
import { Row, SortableHeader, StaticHeader } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { LoadingSpinner } from "../../components/LoadingSpinner";
import { ManagedBanner } from "../../components/ManagedBanner";
import { PageHeader } from "../../components/PageHeader";
import { useToast } from "../../context/ToastContext";
import { useManaged } from "../../hooks/useManaged";
import { useReadOnly } from "../../hooks/useReadOnly";
import { formatRelativeTime } from "../../utils/format";
import { UserModal } from "./UserModal";

type UserSortField = "username" | "role" | "enabled" | "last_login";

export function Users() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const readOnly = useReadOnly();
	const managed = useManaged();
	const [showCreate, setShowCreate] = useState(false);
	const [selected, setSelected] = useState<DashboardUser | null>(null);
	const [sort, setSort] = useState<SortState<UserSortField>>({
		field: "username",
		dir: "asc",
	});

	const { data: users, isLoading } = useQuery({
		queryKey: ["users"],
		queryFn: () => api.users.list(),
	});

	const handleSort = useCallback((field: UserSortField) => {
		setSort((prev) => ({
			field,
			dir: prev.field === field && prev.dir === "asc" ? "desc" : "asc",
		}));
	}, []);

	const sorted = useMemo(() => {
		if (!users) return [];
		const dir = sort.dir === "asc" ? 1 : -1;
		return [...users].sort((a, b) => {
			switch (sort.field) {
				case "username":
					return dir * a.username.localeCompare(b.username);
				case "role":
					return dir * a.role.localeCompare(b.role);
				case "enabled":
					return dir * (Number(a.enabled) - Number(b.enabled));
				case "last_login": {
					const aT = a.last_login_at ? new Date(a.last_login_at).getTime() : 0;
					const bT = b.last_login_at ? new Date(b.last_login_at).getTime() : 0;
					return dir * (aT - bT);
				}
				default:
					return 0;
			}
		});
	}, [users, sort]);

	if (isLoading) {
		return <LoadingSpinner />;
	}

	return (
		<div className="space-y-6 pb-8">
			<PageHeader
				icon={UsersIcon}
				title={t("users.title")}
				description={t("users.description")}
				actions={
					!readOnly &&
					!managed && (
						<button
							type="button"
							onClick={() => setShowCreate(true)}
							className="ui-btn ui-btn-primary"
							data-testid="add-user-button"
						>
							{t("users.addButton")}
						</button>
					)
				}
			/>
			<ManagedBanner />

			{sorted.length > 0 ? (
				<div className="ui-card overflow-hidden">
					<table className="w-full table-fixed ui-table">
						<colgroup>
							<col className="w-[18%]" />
							<col className="w-[18%]" />
							<col className="w-[22%]" />
							<col className="w-[10%]" />
							<col className="w-[14%]" />
							<col className="w-[8%]" />
							<col className="w-[10%]" />
						</colgroup>
						<thead>
							<tr>
								<SortableHeader
									label={t("users.table.username")}
									field="username"
									sort={sort}
									onSort={handleSort}
								/>
								<StaticHeader>{t("users.table.displayName")}</StaticHeader>
								<StaticHeader tooltip={t("users.table.emailTooltip")}>
									{t("users.table.email")}
								</StaticHeader>
								<SortableHeader
									label={t("users.table.role")}
									field="role"
									sort={sort}
									onSort={handleSort}
								/>
								<StaticHeader tooltip={t("users.table.grantsTooltip")}>
									{t("users.table.grants")}
								</StaticHeader>
								<SortableHeader
									label={t("users.table.status")}
									field="enabled"
									sort={sort}
									onSort={handleSort}
								/>
								<SortableHeader
									label={t("users.table.lastLogin")}
									field="last_login"
									sort={sort}
									onSort={handleSort}
								/>
							</tr>
						</thead>
						<tbody>
							{sorted.map((u) => (
								<Row key={u.id} onClick={() => setSelected(u)}>
									<td className="px-4 py-3 text-sm text-gray-200 truncate">
										<span title={u.username}>{u.username}</span>
									</td>
									<td className="px-4 py-3 text-sm text-gray-400 truncate">
										{u.display_name || "—"}
									</td>
									<td className="px-4 py-3 text-sm text-gray-400 truncate">
										{u.email || "—"}
									</td>
									<td className="px-4 py-3">
										<Badge variant={u.role === "admin" ? "warning" : "info"}>
											{u.role === "admin"
												? t("users.role.admin")
												: t("users.role.user")}
										</Badge>
									</td>
									<td className="px-4 py-3 text-sm text-gray-400 truncate">
										{u.role === "admin"
											? t("users.table.allGrants")
											: u.grants.length > 0
												? u.grants
														.map((g) =>
															t(`users.grant.${g}`, { defaultValue: g }),
														)
														.join(", ")
												: t("users.table.noGrants")}
									</td>
									<td className="px-4 py-3">
										<Badge variant={u.enabled ? "success" : "error"}>
											{u.enabled
												? t("users.status.enabled")
												: t("users.status.disabled")}
										</Badge>
									</td>
									<td className="px-4 py-3 text-sm text-gray-400">
										{formatRelativeTime(u.last_login_at)}
									</td>
								</Row>
							))}
						</tbody>
					</table>
				</div>
			) : (
				<EmptyState message={t("users.emptyState")} />
			)}

			{showCreate && (
				<UserModal
					user={null}
					managed={managed}
					onClose={() => setShowCreate(false)}
					onToast={toast}
				/>
			)}
			{selected && (
				<UserModal
					user={selected}
					managed={managed}
					onClose={() => setSelected(null)}
					onToast={toast}
				/>
			)}
		</div>
	);
}
