import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { FleetFailoverGroup } from "../../api/types";
import { ConfirmModal } from "../../components/ConfirmModal";
import { useToast } from "../../context/ToastContext";

// The fleet-wide circuit-breaker reset: one failover group's circuits, cleared
// on every member with a token. Group-scoped by design: clearing every circuit
// on the fleet stays API-only, for the same reason the member-side reset-all
// has no button (it discards the breaker's evidence about every other
// provider). A mutation across the fleet, so it confirms first. Its own file
// because MembersPage sits at the size ceiling.
export function FleetCircuitReset({ primaryId }: { primaryId: string }) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [open, setOpen] = useState(false);
	const [busy, setBusy] = useState(false);
	const [groups, setGroups] = useState<FleetFailoverGroup[] | null>(null);
	const [groupId, setGroupId] = useState("");

	const openModal = async () => {
		setOpen(true);
		setGroups(null);
		setGroupId("");
		try {
			setGroups(await api.fleetFailoverGroups(primaryId));
		} catch {
			setGroups([]);
			toast(t("errors.generic"), "error");
		}
	};

	const confirm = async () => {
		if (!groupId) return;
		setBusy(true);
		try {
			const res = await api.fleetCircuitReset(groupId);
			if (res.failed > 0) {
				toast(
					t("members.resetCircuitsPartial", {
						failed: res.failed,
						members: res.members.length,
					}),
					"error",
				);
			} else {
				toast(
					t("members.resetCircuitsDone", {
						members: res.members.length,
						cleared: res.cleared,
						recovered: res.recovered,
					}),
					"info",
				);
			}
			setOpen(false);
		} catch {
			toast(t("errors.generic"), "error");
		} finally {
			setBusy(false);
		}
	};

	return (
		<>
			<button
				type="button"
				className="ui-btn ui-btn-sm"
				style={{ marginLeft: "auto" }}
				data-testid="fleet-reset-circuits"
				title={t("members.resetCircuitsTip")}
				onClick={openModal}
			>
				{t("members.resetCircuits")}
			</button>
			{open && (
				<ConfirmModal
					title={t("members.resetCircuitsTitle")}
					confirmLabel={t("members.resetCircuitsConfirm")}
					confirmDisabled={!groupId}
					busy={busy}
					confirmTestId="fleet-reset-circuits-confirm"
					onConfirm={confirm}
					onClose={() => setOpen(false)}
				>
					<p className="fd-muted">{t("members.resetCircuitsBody")}</p>
					{groups === null ? (
						<div className="fd-muted">{t("common.loading")}</div>
					) : groups.length === 0 ? (
						<div className="fd-muted">{t("members.resetCircuitsNoGroups")}</div>
					) : (
						<label className="fd-field" style={{ marginTop: "0.6rem" }}>
							<span>{t("members.resetCircuitsGroupLabel")}</span>
							<select
								className="ui-input"
								data-testid="fleet-reset-circuits-group"
								value={groupId}
								onChange={(e) => setGroupId(e.target.value)}
							>
								<option value="">
									{t("members.resetCircuitsGroupPlaceholder")}
								</option>
								{groups.map((g) => (
									<option key={g.id} value={g.id}>
										hotel/{g.display_model}
										{g.display_name ? ` (${g.display_name})` : ""} · {g.entries}
									</option>
								))}
							</select>
						</label>
					)}
				</ConfirmModal>
			)}
		</>
	);
}
