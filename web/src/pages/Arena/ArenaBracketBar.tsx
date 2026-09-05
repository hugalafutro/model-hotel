import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Play, X } from "@/lib/icons";
import { MatchupCard } from "./MatchupCard";
import { BracketPreviewPill } from "./shared";
import type { ArenaView } from "./useArena";

/** The bracket preview and the run/stop button. */
export function ArenaBracketBar({
	arena,
	displayNameMap,
}: {
	arena: ArenaView;
	displayNameMap: Map<string, string>;
}) {
	const { t } = useTranslation();
	return (
		<div className="ui-card p-4 shrink-0">
			<div className="flex items-center gap-4 flex-wrap">
				{/* Bracket Pills */}
				{/* Setup preview: show selected models and matchups before running */}
				{arena.previewPairs && (
					<div className="flex flex-col gap-2 flex-1 min-w-0">
						<div className="flex items-center gap-2">
							<div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider whitespace-nowrap">
								{t("arena.round.firstRound")}
							</div>
							<div className="flex items-center gap-2 flex-wrap">
								{arena.previewPairs.map((p, i) => (
									<div
										// biome-ignore lint/suspicious/noArrayIndexKey: preview position is stable for the static preview
										key={`preview-mu-${i}`}
										className="flex items-center gap-2"
									>
										<BracketPreviewPill
											modelId={p.a}
											displayName={displayNameMap.get(p.a)}
											isTbd={p.a === ""}
										/>
										<span className="text-(--accent) font-bold text-xs px-1">
											{t("arena.vs")}
										</span>
										<BracketPreviewPill
											modelId={p.b}
											displayName={displayNameMap.get(p.b)}
											isTbd={p.b === ""}
										/>
									</div>
								))}
							</div>
						</div>
					</div>
				)}
				{arena.phase === "setup" &&
					arena.arenaMode === "compare" &&
					arena.compareModels.length > 0 && (
						<div className="flex flex-col gap-2 flex-1 min-w-0">
							<div className="flex items-center gap-2 flex-wrap">
								{arena.compareModels.map((m, i) => (
									<BracketPreviewPill
										// biome-ignore lint/suspicious/noArrayIndexKey: preview list order matches model order
										key={`preview-cmp-${i}`}
										modelId={m}
										displayName={displayNameMap.get(m)}
									/>
								))}
							</div>
						</div>
					)}
				{arena.rounds.length > 0 && (
					<div className="flex flex-col gap-2 flex-1 min-w-0">
						{arena.rounds.map((round, roundIdx) => {
							if (arena.phase !== "setup" && roundIdx < arena.currentRound)
								return null;
							if (
								arena.phase === "finished" &&
								roundIdx < arena.rounds.length - 1
							)
								return null;
							return (
								<div
									// biome-ignore lint/suspicious/noArrayIndexKey: round index is the stable identifier for bracket rounds
									key={`round-${roundIdx}`}
									className={`flex items-center gap-2 transition-opacity duration-500 ${
										roundIdx > arena.currentRound + 1 ||
										(roundIdx > arena.currentRound && arena.phase === "voting")
											? "opacity-30"
											: roundIdx > arena.currentRound
												? "opacity-50"
												: "opacity-100"
									}`}
								>
									<div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider whitespace-nowrap">
										{arena.roundLabel(roundIdx, arena.rounds.length)}
									</div>
									<div className="flex items-center gap-2 flex-wrap">
										{round.matchups.map((mu, matchupIdx) => (
											<div
												// biome-ignore lint/suspicious/noArrayIndexKey: matchup position within a round is the stable identifier
												key={`matchup-${roundIdx}-${matchupIdx}`}
												className="flex items-center gap-2"
											>
												<MatchupCard
													slot={mu.slotA}
													slotKey="A"
													roundIdx={roundIdx}
													matchupIdx={matchupIdx}
													vote={mu.vote}
													response={mu.responseA}
													isRunning={arena.isRunning}
													phase={arena.phase}
													onPersonaChange={arena.handlePersonaChange}
													onVote={arena.handleVote}
												/>
												{mu.slotB !== null && (
													<>
														<span className="text-(--accent) font-bold text-xs px-1">
															{t("arena.vs")}
														</span>
														<MatchupCard
															slot={mu.slotB}
															slotKey="B"
															roundIdx={roundIdx}
															matchupIdx={matchupIdx}
															vote={mu.vote}
															response={mu.responseB}
															isRunning={arena.isRunning}
															phase={arena.phase}
															onPersonaChange={arena.handlePersonaChange}
															onVote={arena.handleVote}
														/>
													</>
												)}
											</div>
										))}
									</div>
								</div>
							);
						})}
					</div>
				)}

				{/* Run/Stop Button — always rendered to reserve space */}
				<div className="flex flex-col min-h-[3.5rem]">
					<button
						type="button"
						onClick={
							arena.isRunning ? arena.handleStopAll : arena.handleRunArena
						}
						disabled={
							!arena.buttonLabel || (arena.phase === "setup" && !arena.canRun)
						}
						className={`ui-btn shrink-0 min-h-8 ${
							arena.isRunning ? "ui-btn-danger" : "ui-btn-primary"
						} ${!arena.buttonLabel ? "invisible pointer-events-none" : ""}`}
						tabIndex={!arena.buttonLabel ? -1 : undefined}
					>
						{arena.isRunning ? (
							<>
								<X size={16} />
								{arena.buttonLabel}
							</>
						) : (
							<>
								<Play size={16} />
								{arena.buttonLabel}
							</>
						)}
					</button>
					{(() => {
						let msg: ReactNode = null;
						let muted = false;
						if (
							arena.phase === "setup" &&
							!arena.canRun &&
							arena.disabledReason
						) {
							msg = arena.disabledReason;
						} else if (arena.phase === "running" && arena.isRunning) {
							muted = true;
							msg = (
								<>
									<span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse inline-block mr-1.5 align-middle" />
									{t("arena.status.generating")}
								</>
							);
						} else if (
							arena.phase === "voting" &&
							!arena.rounds[arena.currentRound]?.matchups.every(
								(m) => m.vote !== null,
							)
						) {
							msg = t("arena.status.voteToContinue");
						} else if (arena.phase === "next_round_ready" && !arena.canRun) {
							msg = arena.disabledReason || t("arena.status.nextRoundReady");
						}
						return (
							<p
								className={`text-xs leading-4 mt-1.5 min-h-4 ${msg ? (muted ? "text-(--text-muted)" : "text-amber-400") : "invisible"}`}
							>
								{msg ?? "\u00A0"}
							</p>
						);
					})()}
				</div>
			</div>

			{/* Mode Description */}
			<p className="text-xs text-(--text-tertiary) leading-snug line-clamp-3 mt-3">
				{arena.arenaMode === "competition"
					? t("arena.modeDescription.arena")
					: t("arena.modeDescription.compare")}
			</p>
		</div>
	);
}
