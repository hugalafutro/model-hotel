import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../api/client";
import type { MemberView } from "../api/types";
import { useToast } from "../context/ToastContext";
import { reportResults } from "../utils/syncResults";
import { ConfirmModal } from "./ConfirmModal";
import { Modal } from "./Modal";
import { Notice } from "./Notice";

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
	const [masterKey, setMasterKey] = useState("");
	const [revealToken, setRevealToken] = useState<string | null>(null);
	const [copied, setCopied] = useState(false);

	// Closing the confirm dialog must drop the typed MASTER_KEY so it never
	// lingers in state between attempts.
	const closeConfirm = () => {
		setConfirmOpen(false);
		setMasterKey("");
	};

	const doReset = async () => {
		setResetting(true);
		try {
			const res = await api.resetAdminToken(masterKey);
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
			setConfirmOpen(false);
			setMasterKey("");
			onChanged();
		} catch (e) {
			// A wrong MASTER_KEY comes back as 403 with a specific message; keep the
			// dialog open so the operator can retry rather than dropping them out.
			toast(
				e instanceof ApiError && e.status === 403
					? e.message
					: t("errors.generic"),
				"error",
			);
		} finally {
			setResetting(false);
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
					confirmDisabled={resetting || !masterKey.trim()}
					ackLabel={t("settings.resetAck")}
					onConfirm={doReset}
					onClose={closeConfirm}
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
					<div className="ui-field" style={{ marginBottom: "0.6rem" }}>
						<label className="ui-label" htmlFor="reset-master-key">
							{t("settings.resetMasterKeyLabel")}
						</label>
						<input
							id="reset-master-key"
							className="ui-input"
							type="password"
							autoComplete="off"
							value={masterKey}
							onChange={(e) => setMasterKey(e.target.value)}
							onKeyDown={(e) => {
								if (e.key === "Enter" && masterKey.trim() && !resetting)
									doReset();
							}}
						/>
						<div
							className="fd-faint"
							style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
						>
							{t("settings.resetMasterKeyHint")}
						</div>
					</div>
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
