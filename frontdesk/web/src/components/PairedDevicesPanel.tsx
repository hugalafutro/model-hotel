import QRCode from "qrcode";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type { DeviceRole, PairedDevice, PairStart } from "../api/types";
import { useToast } from "../context/ToastContext";
import { formatAbsolute } from "../utils/time";
import { ConfirmModal } from "./ConfirmModal";

// PairedDevicesPanel is Settings -> Paired devices: pair the Bellhop phone app
// (or any device holding a device token) and manage what is paired. Pairing is
// dual-method by design: the one-time code renders as a QR to scan AND as the
// same payload in a copyable pairing string, so an emulator or a phone with a
// broken camera pairs just as well. The code is short-lived and single-use;
// the panel polls the device list while one is showing so a successful pairing
// appears without a reload.

// pairingPayload is the JSON both the QR and the copyable string carry.
function pairingPayload(code: string): string {
	return JSON.stringify({
		fd_url: window.location.origin,
		pairing_code: code,
		fd_name: window.location.host,
	});
}

// pairListPollMs is how often the device list refreshes while a pairing code
// is displayed, so the freshly paired phone shows up on its own.
const PAIR_LIST_POLL_MS = 5000;

export function PairedDevicesPanel() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [devices, setDevices] = useState<PairedDevice[] | null>(null);
	const [role, setRole] = useState<DeviceRole>("operator");
	const [pair, setPair] = useState<PairStart | null>(null);
	const [qrDataUrl, setQrDataUrl] = useState("");
	const [expired, setExpired] = useState(false);
	const [working, setWorking] = useState(false);
	const [revoking, setRevoking] = useState<PairedDevice | null>(null);
	const [revokeBusy, setRevokeBusy] = useState(false);

	// A failed refresh keeps the last known list; a failed initial load leaves
	// devices null, and the panel stays quiet (renders nothing) like the other
	// Settings panels, so the rest of the page still works.
	const refresh = useCallback(() => {
		api
			.getDevices()
			.then(setDevices)
			.catch(() => {});
	}, []);

	useEffect(() => {
		refresh();
	}, [refresh]);

	// While a live code is displayed: poll the list (the new phone appears on
	// its own) and flip to the expired notice when the TTL runs out.
	useEffect(() => {
		if (!pair || expired) return;
		const poll = setInterval(refresh, PAIR_LIST_POLL_MS);
		// Clamp into setTimeout's signed-32-bit delay range: a delay past ~24.8
		// days would overflow and fire immediately.
		const untilExpiry = Math.min(
			Math.max(new Date(pair.expires_at).getTime() - Date.now(), 0),
			2 ** 31 - 1,
		);
		const expiry = setTimeout(() => setExpired(true), untilExpiry);
		return () => {
			clearInterval(poll);
			clearTimeout(expiry);
		};
	}, [pair, expired, refresh]);

	// Render the QR whenever a code is minted. The stale-QR reset happens in
	// generate() (not here) so the effect never sets state synchronously.
	useEffect(() => {
		if (!pair) return;
		let cancelled = false;
		QRCode.toDataURL(pairingPayload(pair.code), { width: 220, margin: 2 })
			.then((url) => {
				if (!cancelled) setQrDataUrl(url);
			})
			.catch(() => {
				if (!cancelled) setQrDataUrl("");
			});
		return () => {
			cancelled = true;
		};
	}, [pair]);

	const generate = async () => {
		setWorking(true);
		try {
			const p = await api.pairStart(role);
			setQrDataUrl(""); // drop the previous code's QR until the new one renders
			setExpired(false);
			setPair(p);
		} catch {
			toast(t("errors.generic"), "error");
		} finally {
			setWorking(false);
		}
	};

	const copyPayload = async () => {
		if (!pair) return;
		try {
			await navigator.clipboard.writeText(pairingPayload(pair.code));
			toast(t("settings.devices.copied"), "success");
		} catch {
			toast(t("errors.generic"), "error");
		}
	};

	const confirmRevoke = async () => {
		if (!revoking) return;
		setRevokeBusy(true);
		try {
			await api.revokeDevice(revoking.id);
			toast(t("settings.devices.revoked"), "success");
			setRevoking(null);
			refresh();
		} catch {
			toast(t("errors.generic"), "error");
		} finally {
			setRevokeBusy(false);
		}
	};

	if (devices === null) return null;

	return (
		<section className="ui-card ui-card-pad">
			<h2 style={{ fontSize: "1rem" }}>{t("settings.devices.title")}</h2>
			<p
				className="fd-faint"
				style={{ fontSize: "0.82rem", margin: "0.3rem 0 1rem" }}
			>
				{t("settings.devices.description")}
			</p>

			<div className="fd-row" style={{ flexWrap: "wrap", gap: "0.8rem" }}>
				{/* Drop the field's stacked-layout bottom margin: in this horizontal
				    row it would push the flex-end button below the select. */}
				<div className="ui-field" style={{ marginBottom: 0 }}>
					<label className="ui-label" htmlFor="pair-role">
						{t("settings.devices.role")}
					</label>
					<select
						id="pair-role"
						className="ui-input"
						value={role}
						onChange={(e) => setRole(e.target.value as DeviceRole)}
					>
						<option value="operator">
							{t("settings.devices.roleOperator")}
						</option>
						<option value="monitor">{t("settings.devices.roleMonitor")}</option>
					</select>
				</div>
				<button
					type="button"
					className="ui-btn ui-btn-primary"
					disabled={working}
					style={{ alignSelf: "flex-end" }}
					onClick={generate}
				>
					{pair
						? t("settings.devices.regenerate")
						: t("settings.devices.generate")}
				</button>
			</div>
			<p
				className="fd-faint"
				style={{ fontSize: "0.78rem", margin: "0.4rem 0" }}
			>
				{role === "operator"
					? t("settings.devices.roleOperatorHint")
					: t("settings.devices.roleMonitorHint")}
			</p>

			{pair && expired && (
				<div
					className="fd-error-text"
					role="alert"
					style={{ margin: "0.8rem 0" }}
				>
					{t("settings.devices.expired")}
				</div>
			)}

			{pair && !expired && (
				<div
					className="fd-row"
					style={{
						alignItems: "flex-start",
						flexWrap: "wrap",
						gap: "1rem",
						margin: "0.8rem 0",
					}}
				>
					{qrDataUrl && (
						<img
							src={qrDataUrl}
							alt={t("settings.devices.qrAlt")}
							width={220}
							height={220}
						/>
					)}
					<div style={{ flex: "1 1 260px" }}>
						<div className="ui-field">
							<label className="ui-label" htmlFor="pairing-string">
								{t("settings.devices.pairingString")}
							</label>
							<textarea
								id="pairing-string"
								className="ui-input"
								readOnly
								rows={4}
								value={pairingPayload(pair.code)}
								onFocus={(e) => e.target.select()}
							/>
						</div>
						<div className="fd-row" style={{ marginTop: "0.5rem" }}>
							<button type="button" className="ui-btn" onClick={copyPayload}>
								{t("settings.devices.copy")}
							</button>
						</div>
						<p
							className="fd-faint"
							style={{ fontSize: "0.78rem", marginTop: "0.5rem" }}
						>
							{t("settings.devices.codeHint", {
								time: formatAbsolute(pair.expires_at),
							})}
						</p>
					</div>
				</div>
			)}

			{devices.length === 0 && (
				<p className="fd-faint" style={{ fontSize: "0.82rem" }}>
					{t("settings.devices.empty")}
				</p>
			)}

			{devices && devices.length > 0 && (
				<div style={{ overflowX: "auto", marginTop: "0.8rem" }}>
					<table className="ui-table ui-table--nowrap">
						<thead>
							<tr>
								<th>{t("settings.devices.colLabel")}</th>
								<th>{t("settings.devices.colRole")}</th>
								<th>{t("settings.devices.colPaired")}</th>
								<th>{t("settings.devices.colLastSeen")}</th>
								<th />
							</tr>
						</thead>
						<tbody>
							{devices.map((d) => (
								<tr key={d.id}>
									<td>{d.label}</td>
									<td>
										<span
											className={`ui-badge ${
												d.role === "operator" ? "ui-badge-info" : ""
											}`}
											data-test-variant={d.role}
										>
											{d.role === "operator"
												? t("settings.devices.roleOperator")
												: t("settings.devices.roleMonitor")}
										</span>
									</td>
									<td>{formatAbsolute(d.created_at)}</td>
									<td>
										{d.last_seen_at
											? formatAbsolute(d.last_seen_at)
											: t("settings.devices.neverSeen")}
									</td>
									<td>
										<button
											type="button"
											className="ui-btn ui-btn-danger"
											onClick={() => setRevoking(d)}
										>
											{t("settings.devices.revoke")}
										</button>
									</td>
								</tr>
							))}
						</tbody>
					</table>
				</div>
			)}

			{revoking && (
				<ConfirmModal
					title={t("settings.devices.revokeTitle")}
					confirmLabel={t("settings.devices.revoke")}
					busy={revokeBusy}
					onConfirm={confirmRevoke}
					onClose={() => setRevoking(null)}
				>
					<p>{t("settings.devices.revokeBody", { label: revoking.label })}</p>
				</ConfirmModal>
			)}
		</section>
	);
}
