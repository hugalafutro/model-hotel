import { useTranslation } from "react-i18next";
import { Trophy } from "@/lib/icons";
import { Modal } from "../../components/Modal";
import { shortModelName } from "../../utils/model";
import { getRoundLabel } from "./builders";
import type { WinnerSummaryModalProps } from "./types";

export function WinnerSummaryModal({
	winner,
	rounds,
	onClose,
}: WinnerSummaryModalProps) {
	const { t } = useTranslation();
	return (
		<Modal
			header={
				<div className="flex items-center gap-3 mb-0">
					<Trophy size={28} className="text-amber-400" />
					<h2 className="text-xl font-bold text-white">
						{t("arena.winnerModal.title")}
					</h2>
				</div>
			}
			onClose={onClose}
			maxWidth="max-w-lg"
			scrollable
		>
			<div className="flex items-center gap-2 px-4 py-3 rounded-lg bg-amber-500/10 border border-amber-500/30 mb-4">
				<Trophy size={18} className="text-amber-400" />
				<span className="text-sm font-bold text-amber-300">
					{shortModelName(winner)}
				</span>
				<span className="text-sm text-amber-400/70">
					{t("arena.winnerModal.wins")}
				</span>
			</div>

			<div className="space-y-3">
				{rounds.map((round, roundIdx) => (
					// biome-ignore lint/suspicious/noArrayIndexKey: round index is the stable identifier in summary
					<div key={`winner-round-${roundIdx}`}>
						<div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider mb-1">
							{getRoundLabel(roundIdx, rounds.length, "competition")}
						</div>
						{round.matchups.map((mu, mi) => (
							<div // biome-ignore lint/suspicious/noArrayIndexKey: match position is the stable identifier in the summary
								key={`winner-match-${roundIdx}-${mi}`}
								className="flex items-center gap-2 text-sm"
							>
								<span
									className={
										mu.vote === "A"
											? "text-green-400 font-medium"
											: "text-(--text-secondary)"
									}
								>
									{mu.slotA ? shortModelName(mu.slotA.modelId) : t("arena.tbd")}
								</span>
								<span className="text-(--text-tertiary)">{t("arena.vs")}</span>
								<span
									className={
										mu.vote === "B"
											? "text-green-400 font-medium"
											: "text-(--text-secondary)"
									}
								>
									{mu.slotB ? shortModelName(mu.slotB.modelId) : t("arena.tbd")}
								</span>
								{mu.vote && (
									<span className="text-xs text-(--accent)">
										←{" "}
										{shortModelName(
											(mu.vote === "A" ? mu.slotA : mu.slotB)?.modelId ?? "",
										)}{" "}
										{t("arena.winnerModal.wins")}
									</span>
								)}
							</div>
						))}
					</div>
				))}
			</div>

			<div className="flex justify-end mt-4">
				<button
					type="button"
					onClick={onClose}
					className="ui-btn ui-btn-primary"
				>
					{t("arena.winnerModal.close")}
				</button>
			</div>
		</Modal>
	);
}
