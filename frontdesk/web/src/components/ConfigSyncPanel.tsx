import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type {
	ConfigPreview,
	ConfigPreviewItem,
	MemberView,
} from "../api/types";
import { useToast } from "../context/ToastContext";
import { ConfirmModal } from "./ConfirmModal";

// ConfigDispositionBadge maps a preview disposition to a badge. Unlike the
// admin-token badge it also surfaces the change counts, so the operator sees
// exactly what each member gains, overwrites, or loses before syncing.
function ConfigDispositionBadge({ item }: { item: ConfigPreviewItem }) {
	const { t } = useTranslation();
	if (item.disposition === "matches")
		return (
			<span className="ui-badge ui-badge-ok">
				{t("settings.syncAlreadyMatches")}
			</span>
		);
	if (item.disposition === "blocked")
		return (
			<span className="ui-badge ui-badge-danger">
				{t("settings.syncBlocked")}
			</span>
		);
	// overwrite: show +added ~updated -removed.
	return (
		<span
			className="fd-row"
			style={{ gap: "0.35rem", justifyContent: "flex-end" }}
		>
			{item.added > 0 && (
				<span className="ui-badge ui-badge-ok">+{item.added}</span>
			)}
			{item.updated > 0 && (
				<span className="ui-badge ui-badge-info">~{item.updated}</span>
			)}
			{item.removed > 0 && (
				<span className="ui-badge ui-badge-warn">-{item.removed}</span>
			)}
		</span>
	);
}

// ConfigSyncPanel: pick a primary, preview each member's diff, then double-confirm
// the replace. A separate action from admin-token sync (HA Phase 5): replacing
// config can remove providers/keys on a replica, so it must never ride along with
// a routine token rotation.
export function ConfigSyncPanel({
	members,
	onChanged,
}: {
	members: MemberView[];
	onChanged: () => void;
}) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [primaryId, setPrimaryId] = useState("");
	const [preview, setPreview] = useState<ConfigPreview | null>(null);
	const [previewing, setPreviewing] = useState(false);
	const [confirmOpen, setConfirmOpen] = useState(false);
	const [syncing, setSyncing] = useState(false);

	const nameOf = (id: string) => members.find((m) => m.id === id)?.name ?? id;
	const overwrites =
		preview?.items.filter((i) => i.disposition === "overwrite") ?? [];
	const totalRemoved = overwrites.reduce((n, i) => n + i.removed, 0);

	const runPreview = async (id: string) => {
		setPrimaryId(id);
		setPreview(null);
		if (!id) return;
		setPreviewing(true);
		try {
			setPreview(await api.configPreview(id));
		} catch {
			toast(t("errors.generic"), "error");
		} finally {
			setPreviewing(false);
		}
	};

	const doSync = async () => {
		setSyncing(true);
		try {
			const res = await api.configSync(primaryId);
			const ok = res.results.filter((r) => r.ok).length;
			for (const r of res.results) {
				toast(
					r.ok
						? t("settings.memberResultOk", { name: r.name })
						: t("settings.memberResultFailed", {
								name: r.name,
								error: r.error ?? t("settings.memberResultFailedGeneric"),
							}),
					r.ok ? "success" : "error",
				);
			}
			toast(
				t("settings.configSyncDone", {
					ok,
					total: res.results.length,
					count: res.results.length,
				}),
				ok === res.results.length ? "success" : "error",
			);
			onChanged();
			setPreview(null);
		} catch {
			toast(t("errors.generic"), "error");
		} finally {
			setSyncing(false);
			setConfirmOpen(false);
		}
	};

	return (
		<div className="ui-card ui-card-pad">
			<h2 style={{ fontSize: "1rem" }}>{t("settings.configSyncSection")}</h2>
			<p
				className="fd-muted"
				style={{ fontSize: "0.85rem", margin: "0.4rem 0 1rem" }}
			>
				{t("settings.configSyncIntro")}
			</p>

			<div className="ui-field" style={{ maxWidth: 320 }}>
				<label className="ui-label" htmlFor="config-primary">
					{t("settings.configSyncPrimary")}
				</label>
				<select
					id="config-primary"
					className="ui-select"
					value={primaryId}
					onChange={(e) => runPreview(e.target.value)}
				>
					<option value="">{t("settings.syncSelectPrimary")}</option>
					{members.map((m) => (
						<option key={m.id} value={m.id}>
							{m.name}
						</option>
					))}
				</select>
			</div>

			{previewing && <div className="fd-faint">{t("common.loading")}</div>}

			{preview && (
				<table className="ui-table" style={{ marginTop: "0.5rem" }}>
					<tbody>
						{preview.items.map((it) => (
							<tr key={it.member_id}>
								<td>
									{nameOf(it.member_id)}
									{it.member_id === preview.primary_id && (
										<span className="fd-faint">
											{" "}
											· {t("settings.primaryTag")}
										</span>
									)}
									{it.note && (
										<div className="fd-faint" style={{ fontSize: "0.78rem" }}>
											{it.note}
										</div>
									)}
								</td>
								<td style={{ textAlign: "right" }}>
									<ConfigDispositionBadge item={it} />
								</td>
							</tr>
						))}
					</tbody>
				</table>
			)}

			<div style={{ marginTop: "1rem" }}>
				<button
					type="button"
					className="ui-btn"
					disabled={overwrites.length === 0}
					onClick={() => setConfirmOpen(true)}
				>
					{t("settings.configSyncButton")}
				</button>
			</div>

			{confirmOpen && (
				<ConfirmModal
					title={t("settings.configSyncConfirmTitle", {
						count: overwrites.length,
					})}
					confirmLabel={t("settings.configSyncDo", {
						count: overwrites.length,
					})}
					confirmDisabled={syncing}
					ackLabel={t("settings.configSyncAck")}
					onConfirm={doSync}
					onClose={() => setConfirmOpen(false)}
				>
					<p className="fd-muted">{t("settings.configSyncConfirmBody")}</p>
					{totalRemoved > 0 && (
						<p className="fd-error-text" style={{ margin: "0.5rem 0" }}>
							{t("settings.configSyncRemovalWarning", { count: totalRemoved })}
						</p>
					)}
					<ul style={{ margin: "0.6rem 0" }}>
						{overwrites.map((it) => (
							<li key={it.member_id}>
								{nameOf(it.member_id)}
								<span className="fd-faint">
									{" "}
									(+{it.added} ~{it.updated} -{it.removed})
								</span>
							</li>
						))}
					</ul>
				</ConfirmModal>
			)}
		</div>
	);
}
