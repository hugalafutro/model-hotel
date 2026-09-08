import { useTranslation } from "react-i18next";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { ResponseCard } from "./ResponseCard";
import { SwapPicker } from "./SwapPicker";
import type { ArenaView } from "./useArena";
import { usedModelIds } from "./utils";

/** The per-round response cards (matchups in competition mode, one card per model in compare mode) and the full-reset confirm dialog. */
export function ArenaResponseGrid({ arena }: { arena: ArenaView }) {
	const { t } = useTranslation();
	return (
		<>
			{/* Response Grid */}
			{arena.showResponseGrid &&
				arena.rounds.map((round, roundIdx) => {
					const hasActualResponse = round.matchups.some(
						(mu) => mu.responseA || mu.responseB,
					);
					if (!hasActualResponse) return null;
					// Once a later round has responses, skip earlier rounds
					const laterRoundHasResponses = arena.rounds.some(
						(r, ri) =>
							ri > roundIdx &&
							r.matchups.some((mu) => mu.responseA || mu.responseB),
					);
					if (laterRoundHasResponses) return null;
					const isCompare =
						arena.arenaMode === "compare" &&
						round.matchups.every((m) => m.slotB === null);
					return (
						// biome-ignore lint/suspicious/noArrayIndexKey: round index is the stable identifier
						<div key={`resp-round-${roundIdx}`}>
							<div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider mb-2">
								{isCompare
									? t("arena.responses.label")
									: arena.roundLabel(roundIdx, arena.rounds.length)}
							</div>
							<div
								className={`${
									isCompare
										? "flex flex-wrap justify-center gap-4 [&>*]:w-full [&>*]:md:w-[calc(50%-0.5rem)] [&>*]:xl:w-[calc(33.333%-0.67rem)]"
										: "space-y-4"
								} transition-opacity duration-500 ${
									roundIdx <= arena.currentRound ? "opacity-100" : "opacity-20"
								}`}
							>
								{round.matchups.map((mu, matchupIdx) => {
									// Compare mode: flat grid of individual cards
									if (isCompare) {
										return (
											<div
												// biome-ignore lint/suspicious/noArrayIndexKey: matchup position is the stable identifier in compare mode
												key={`compare-${roundIdx}-${matchupIdx}`}
												className="rounded-xl border border-(--border-subtle) bg-(--surface)/50 p-4 h-[29rem] overflow-hidden"
											>
												{mu.slotA === null &&
												roundIdx === arena.currentRound ? (
													<SwapPicker
														enabledModels={arena.enabledModels}
														disabledModels={arena.disabledModels}
														alreadyUsed={usedModelIds(round, matchupIdx, "A")}
														onSelect={(modelId) =>
															arena.handleSwapCompleteAndUpdate(
																roundIdx,
																matchupIdx,
																"A",
																modelId,
															)
														}
													/>
												) : (
													mu.responseA && (
														<ResponseCard
															response={mu.responseA}
															vote={mu.vote}
															slotKey="A"
															roundIdx={roundIdx}
															matchupIdx={matchupIdx}
															onVote={arena.handleVote}
															onRetry={arena.handleRetrySlot}
															onSwapModel={arena.handleSwapModel}
															onCancelSlot={arena.handleCancelSlot}
															enabledModels={arena.enabledModels}
															showVote={false}
															params={mu.slotA?.params}
														/>
													)
												)}
											</div>
										);
									}

									// Competition mode: A-vs-B pairs
									return (
										<div
											// biome-ignore lint/suspicious/noArrayIndexKey: matchup position is the stable identifier in competition mode
											key={`comp-${roundIdx}-${matchupIdx}`}
											className="rounded-xl border border-(--border-subtle) bg-(--surface)/50 p-4 h-[31rem] overflow-hidden flex flex-col"
										>
											{round.matchups.length > 1 && (
												<div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider mb-3 shrink-0">
													{t("arena.match.label", { num: matchupIdx + 1 })}
												</div>
											)}
											<div className="grid grid-cols-1 md:grid-cols-2 grid-rows-[minmax(0,1fr)] auto-rows-[minmax(0,1fr)] gap-4 flex-1 min-h-0">
												{mu.slotA === null &&
												roundIdx === arena.currentRound ? (
													<SwapPicker
														enabledModels={arena.enabledModels}
														disabledModels={arena.disabledModels}
														alreadyUsed={usedModelIds(round, matchupIdx, "A")}
														onSelect={(modelId) =>
															arena.handleSwapCompleteAndUpdate(
																roundIdx,
																matchupIdx,
																"A",
																modelId,
															)
														}
													/>
												) : (
													mu.responseA && (
														<ResponseCard
															response={mu.responseA}
															vote={mu.vote}
															slotKey="A"
															roundIdx={roundIdx}
															matchupIdx={matchupIdx}
															onVote={arena.handleVote}
															onRetry={arena.handleRetrySlot}
															onSwapModel={arena.handleSwapModel}
															onCancelSlot={arena.handleCancelSlot}
															enabledModels={arena.enabledModels}
															showVote={
																roundIdx <= arena.currentRound &&
																mu.responseA.done &&
																(!mu.responseB || mu.responseB.done)
															}
															params={mu.slotA?.params}
														/>
													)
												)}
												{mu.slotB === null &&
												roundIdx === arena.currentRound ? (
													<SwapPicker
														enabledModels={arena.enabledModels}
														disabledModels={arena.disabledModels}
														alreadyUsed={usedModelIds(round, matchupIdx, "B")}
														onSelect={(modelId) =>
															arena.handleSwapCompleteAndUpdate(
																roundIdx,
																matchupIdx,
																"B",
																modelId,
															)
														}
													/>
												) : (
													mu.responseB && (
														<ResponseCard
															response={mu.responseB}
															vote={mu.vote}
															slotKey="B"
															roundIdx={roundIdx}
															matchupIdx={matchupIdx}
															onVote={arena.handleVote}
															onRetry={arena.handleRetrySlot}
															onSwapModel={arena.handleSwapModel}
															onCancelSlot={arena.handleCancelSlot}
															enabledModels={arena.enabledModels}
															showVote={
																roundIdx <= arena.currentRound &&
																mu.responseB.done &&
																(!mu.responseA || mu.responseA.done)
															}
															params={mu.slotB?.params}
														/>
													)
												)}
											</div>
										</div>
									);
								})}
							</div>
						</div>
					);
				})}

			{arena.pendingFullReset && (
				<ConfirmDialog
					title={t("arena.confirmReset.title")}
					message={t("arena.confirmReset.message")}
					fields={[]}
					confirmLabel={t("arena.confirmReset.confirmLabel")}
					onConfirm={() => {
						arena.resetAll();
						arena.setPendingFullReset(false);
						arena.toast(t("arena.toast.reset"), "info");
					}}
					onCancel={() => arena.setPendingFullReset(false)}
				/>
			)}
		</>
	);
}
