import { useTranslation } from "react-i18next";
import type { DashboardUser } from "../../api/types";
import { budgetText } from "../../components/budget";
import { InfoHint } from "../../components/InfoHint";
import { DetailItem } from "../../components/LogDetailItem";
import { formatDateTime } from "../../utils/format";

/** The read-only card a user opens as: what the row holds, before Edit. */
export function UserDetailCard({
	user,
	managed,
	providers,
}: {
	user: DashboardUser;
	managed: boolean;
	/** Resolves a capped account's provider ids to names. */
	providers: { id: string; name: string }[];
}) {
	const { t } = useTranslation();
	/** A per-user cap, or "No cap" when none is set. */
	const capText = (n: number | null | undefined) =>
		n != null ? String(n) : t("users.modal.noCap");
	const providerAccessText = () => {
		const cap = user.allowed_providers;
		if (cap == null) return t("users.providerAccessAll");
		if (cap.length === 0) return t("users.providerAccessNone");
		return cap
			.map((id) => providers.find((p) => p.id === id)?.name ?? id)
			.join(", ");
	};

	return (
		<div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
			<DetailItem
				label={t("users.modal.displayName")}
				value={user.display_name}
			/>
			<DetailItem label={t("users.table.email")} value={user.email} />
			<DetailItem
				label={t("users.modal.role")}
				value={
					user.role === "admin" ? t("users.role.admin") : t("users.role.user")
				}
			/>
			<DetailItem
				label={t("users.table.status")}
				value={user.enabled ? t("common.enabled") : t("common.disabled")}
			/>
			{user.role === "user" && (
				<DetailItem
					label={t("users.modal.grants")}
					className="sm:col-span-2"
					value={
						user.grants.length === 0
							? t("users.table.noGrants")
							: user.grants
									.map((g) => t(`users.grant.${g}`, { defaultValue: g }))
									.join(", ")
					}
				/>
			)}
			<DetailItem
				label={t("users.providerAccess")}
				className="sm:col-span-2"
				value={providerAccessText()}
			/>
			<DetailItem
				label={t("users.modal.twoFactor")}
				value={user.totp_enabled ? t("common.enabled") : t("common.disabled")}
			/>
			<DetailItem
				label={t("users.table.lastLogin")}
				value={
					user.last_login_at
						? formatDateTime(user.last_login_at)
						: t("common.never")
				}
			/>
			<DetailItem
				label={t("users.modal.created")}
				value={formatDateTime(user.created_at)}
			/>
			<DetailItem
				label={t("users.modal.lastEdited")}
				value={formatDateTime(user.updated_at)}
			/>
			<div className="sm:col-span-2 grid grid-cols-1 sm:grid-cols-3 gap-2">
				<DetailItem
					emphasis="stat"
					label={t("users.modal.limitRps")}
					labelExtra={<InfoHint tooltip={t("users.modal.limitsHint")} />}
					value={capText(user.rate_limit_rps)}
					mono
				/>
				<DetailItem
					emphasis="stat"
					label={t("users.modal.limitBurst")}
					labelExtra={<InfoHint tooltip={t("users.modal.limitsHint")} />}
					value={capText(user.rate_limit_burst)}
					mono
				/>
				<DetailItem
					emphasis="stat"
					label={t("users.modal.limitTpm")}
					labelExtra={<InfoHint tooltip={t("users.modal.limitsHint")} />}
					value={capText(user.rate_limit_tpm)}
					mono
				/>
			</div>
			<DetailItem
				emphasis="stat"
				label={t("budget.label")}
				labelExtra={<InfoHint tooltip={t("budget.tooltip")} />}
				className="sm:col-span-2"
				value={budgetText(t, user)}
				mono
			/>
			{managed && (
				<p data-testid="managed-note" className="sm:col-span-2 ui-hint">
					{t("settings.managed.sectionNote")}
				</p>
			)}
		</div>
	);
}
