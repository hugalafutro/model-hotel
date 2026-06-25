import type { TFunction } from "i18next";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type { MemberView, SyncPreview, SyncResultItem } from "../api/types";
import { useToast } from "../context/ToastContext";
import { ConfirmModal } from "./ConfirmModal";
import { Modal } from "./Modal";
import { Notice } from "./Notice";

// dispositionBadge maps a preview disposition to a badge.
function DispositionBadge({ disposition }: { disposition: string }) {
	const { t } = useTranslation();
	if (disposition === "overwrite")
		return (
			<span className="ui-badge ui-badge-warn">
				{t("settings.syncWillOverwrite")}
			</span>
		);
	if (disposition === "matches")
		return (
			<span className="ui-badge ui-badge-ok">
				{t("settings.syncAlreadyMatches")}
			</span>
		);
	return (
		<span className="ui-badge ui-badge-danger">
			{t("settings.syncBlocked")}
		</span>
	);
}

function reportResults(
	results: SyncResultItem[],
	toast: (m: string, k: "success" | "error") => void,
	t: TFunction,
) {
	for (const r of results) {
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
}

// AdminTokenSyncPanel: pick a primary, preview by name, then double-confirm the
// overwrite. Mirrors Section 5 of the HA plan.
export function AdminTokenSyncPanel({
	members,
	onChanged,
}: {
	members: MemberView[];
	onChanged: () => void;
}) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [primaryId, setPrimaryId] = useState("");
	const [preview, setPreview] = useState<SyncPreview | null>(null);
	const [previewing, setPreviewing] = useState(false);
	const [confirmOpen, setConfirmOpen] = useState(false);
	const [syncing, setSyncing] = useState(false);

	const nameOf = (id: string) => members.find((m) => m.id === id)?.name ?? id;
	const overwrites =
		preview?.items.filter((i) => i.disposition === "overwrite") ?? [];

	const runPreview = async (id: string) => {
		setPrimaryId(id);
		setPreview(null);
		if (!id) return;
		setPreviewing(true);
		try {
			setPreview(await api.syncPreview(id));
		} catch {
			toast(t("errors.generic"), "error");
		} finally {
			setPreviewing(false);
		}
	};

	const doSync = async () => {
		setSyncing(true);
		try {
			const res = await api.syncRun(primaryId);
			const ok = res.results.filter((r) => r.ok).length;
			reportResults(res.results, toast, t);
			toast(
				t("settings.syncDone", {
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
			<h2 style={{ fontSize: "1rem" }}>{t("settings.syncSection")}</h2>
			<p
				className="fd-muted"
				style={{ fontSize: "0.85rem", margin: "0.4rem 0 1rem" }}
			>
				{t("settings.syncIntro")}
			</p>

			<div className="ui-field" style={{ maxWidth: 320 }}>
				<label className="ui-label" htmlFor="sync-primary">
					{t("settings.syncPrimary")}
				</label>
				<select
					id="sync-primary"
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
								</td>
								<td style={{ textAlign: "right" }}>
									<DispositionBadge disposition={it.disposition} />
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
					{t("settings.syncButton")}
				</button>
			</div>

			{confirmOpen && (
				<ConfirmModal
					title={t("settings.syncConfirmTitle", { count: overwrites.length })}
					confirmLabel={t("settings.syncDo", { count: overwrites.length })}
					confirmDisabled={syncing}
					ackLabel={t("settings.syncAck")}
					onConfirm={doSync}
					onClose={() => setConfirmOpen(false)}
				>
					<p className="fd-muted">{t("settings.syncConfirmBody")}</p>
					<ul style={{ margin: "0.6rem 0" }}>
						{overwrites.map((it) => (
							<li key={it.member_id}>{nameOf(it.member_id)}</li>
						))}
					</ul>
				</ConfirmModal>
			)}
		</div>
	);
}

// AdminTokenResetPanel: destructive double-confirm, then reveal-once token.
export function AdminTokenResetPanel({
	members,
	onChanged,
}: {
	members: MemberView[];
	onChanged: () => void;
}) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [confirmOpen, setConfirmOpen] = useState(false);
	const [resetting, setResetting] = useState(false);
	const [revealToken, setRevealToken] = useState<string | null>(null);
	const [copied, setCopied] = useState(false);

	const doReset = async () => {
		setResetting(true);
		try {
			const res = await api.resetAdminToken();
			const ok = res.results.filter((r) => r.ok).length;
			reportResults(res.results, toast, t);
			toast(
				t("settings.resetDone", {
					ok,
					total: res.results.length,
					count: res.results.length,
				}),
				ok > 0 ? "success" : "error",
			);
			setRevealToken(res.token);
			onChanged();
		} catch {
			toast(t("errors.generic"), "error");
		} finally {
			setResetting(false);
			setConfirmOpen(false);
		}
	};

	// Flip the "Copied" label back to "Copy" after a moment so a second copy gives
	// visible feedback. The panel outlives the reveal modal, so clear any pending
	// timer on unmount.
	const copyResetTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
	useEffect(
		() => () => {
			if (copyResetTimer.current) clearTimeout(copyResetTimer.current);
		},
		[],
	);

	const copy = async () => {
		if (!revealToken) return;
		try {
			await navigator.clipboard.writeText(revealToken);
			setCopied(true);
			if (copyResetTimer.current) clearTimeout(copyResetTimer.current);
			copyResetTimer.current = setTimeout(() => setCopied(false), 2000);
		} catch {
			/* clipboard blocked: the token is selectable in the field */
		}
	};

	return (
		<div className="ui-card ui-card-pad">
			<h2 style={{ fontSize: "1rem" }}>{t("settings.resetSection")}</h2>
			<p
				className="fd-muted"
				style={{ fontSize: "0.85rem", margin: "0.4rem 0 1rem" }}
			>
				{t("settings.resetIntro")}
			</p>
			<Notice style={{ margin: "0 0 1rem" }}>
				{t("settings.resetTokenNotice")}
			</Notice>
			<button
				type="button"
				className="ui-btn ui-btn-danger"
				onClick={() => setConfirmOpen(true)}
			>
				{t("settings.resetButton")}
			</button>

			{confirmOpen && (
				<ConfirmModal
					title={t("settings.resetConfirmTitle")}
					confirmLabel={t("settings.resetDo")}
					confirmDisabled={resetting}
					ackLabel={t("settings.resetAck")}
					onConfirm={doReset}
					onClose={() => setConfirmOpen(false)}
				>
					<p className="fd-muted">{t("settings.resetConfirmBody")}</p>
					<p
						className="fd-faint"
						style={{ fontSize: "0.8rem", marginBottom: "0.4rem" }}
					>
						{t("settings.affectedMembers")}:
					</p>
					<ul style={{ margin: "0 0 0.6rem" }}>
						{members.map((m) => (
							<li key={m.id}>{m.name}</li>
						))}
					</ul>
				</ConfirmModal>
			)}

			{revealToken && (
				<Modal
					title={t("settings.resetRevealTitle")}
					onClose={() => {
						setRevealToken(null);
						setCopied(false);
					}}
					actions={
						<button
							type="button"
							className="ui-btn ui-btn-primary"
							onClick={() => {
								setRevealToken(null);
								setCopied(false);
							}}
						>
							{t("settings.resetSavedConfirm")}
						</button>
					}
				>
					<p className="fd-muted">{t("settings.resetRevealBody")}</p>
					<div className="fd-row" style={{ marginTop: "0.7rem" }}>
						<input
							className="ui-input fd-mono"
							readOnly
							value={revealToken}
							onFocus={(e) => e.currentTarget.select()}
						/>
						<button type="button" className="ui-btn" onClick={copy}>
							{copied ? t("common.copied") : t("common.copy")}
						</button>
					</div>
				</Modal>
			)}
		</div>
	);
}
