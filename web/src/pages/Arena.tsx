import {
	Columns3,
	Eraser,
	History,
	Play,
	RotateCcw,
	Swords,
	X,
} from "lucide-react";
import { useEffect } from "react";
import { ActionIconButton } from "../components/ActionIconButton";
import { ArenaHistoryModal } from "../components/ArenaHistoryModal";
import { CollapsibleToggle } from "../components/CollapsibleToggle";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ModelPicker } from "../components/ModelPicker";
import { PageHeader } from "../components/PageHeader";
import { PersonaPicker } from "../components/PersonaPicker";
import { PromptPicker } from "../components/PromptPicker";
import { SubModeToggle } from "../components/SubModeToggle";
import { ARENA_PROMPTS, CHAT_PERSONAS } from "../data/presets";
import { parseCapabilities } from "../utils/model";
import { MatchupCard } from "./Arena/MatchupCard";
import { ParamEditorModal } from "./Arena/ParamEditorModal";
import { ResponseCard } from "./Arena/ResponseCard";
import { SwapPicker } from "./Arena/SwapPicker";
import { BracketPreviewPill } from "./Arena/shared";
import { useArena } from "./Arena/useArena";
import { WinnerSummaryModal } from "./Arena/WinnerSummaryModal";

export function Arena() {
	const arena = useArena();

	// Auto-scroll the page viewport during streaming so response cards stay visible.
	// Uses instant scroll because Firefox cancels in-progress smooth scrolls
	// when scrollTo is called again rapidly during streaming.
	const streamingContentLen = arena.rounds.reduce(
		(sum, round) =>
			sum +
			round.matchups.reduce((s, mu) => {
				if (mu.responseA) s += (mu.responseA.content || "").length;
				if (mu.responseB) s += (mu.responseB.content || "").length;
				return s;
			}, 0),
		0,
	);
	// biome-ignore lint/correctness/useExhaustiveDependencies: streamingContentLen triggers re-scroll on streaming updates
	useEffect(() => {
		if (!arena.isRunning) return;
		const nearBottom =
			document.documentElement.scrollHeight -
				window.scrollY -
				window.innerHeight <
			200;
		if (nearBottom) {
			window.scrollTo({
				top: document.documentElement.scrollHeight,
				behavior: "instant",
			});
		}
	}, [streamingContentLen, arena.isRunning]);

	return (
		<div className="flex flex-col gap-6 min-h-full">
			{/* Header */}
			<PageHeader
				icon={arena.arenaIcon}
				title={arena.arenaMode === "competition" ? "Arena" : "Compare"}
				description={
					arena.arenaMode === "competition"
						? "Bracket tournament - models compete head-to-head"
						: "Side-by-side - compare model outputs on the same prompt"
				}
			/>

			{/* Controls */}
			<div className="ui-card p-4">
				<div className="flex items-center justify-between">
					<div className="flex items-center gap-3">
						<span className="text-sm font-semibold text-(--text-primary)">
							Controls
						</span>
						<SubModeToggle
							options={[
								{ value: "competition" as const, label: "Arena", icon: Swords },
								{ value: "compare" as const, label: "Compare", icon: Columns3 },
							]}
							value={arena.arenaMode}
							onChange={(v) => {
								if (arena.phase === "setup") arena.setArenaMode(v);
							}}
							disabled={arena.phase !== "setup"}
						/>
					</div>
					<div className="flex items-center gap-1">
						<button
							type="button"
							onClick={() => arena.setShowHistoryModal(true)}
							className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)]"
							title="Match history"
							aria-label="Match history"
						>
							<History size={14} />
						</button>
						{(arena.phase !== "setup" ||
							(arena.arenaMode === "competition"
								? arena.bracketModels.length > 0
								: arena.compareModels.length > 0) ||
							!!arena.activePromptId ||
							!!arena.prompt.trim() ||
							!!arena.comparePersonaId ||
							!!arena.comparePersonaPrompt.trim()) && (
							<>
								{/* Light reset: clear results only, keep models/prompt/persona */}
								{arena.phase !== "setup" && (
									<ActionIconButton
										icon={Eraser}
										onClick={() => {
											for (const [, ctrl] of arena.abortMapRef.current) {
												ctrl.abort();
											}
											arena.abortMapRef.current.clear();
											arena.setRounds([]);
											arena.setCurrentRound(0);
											arena.setPhase("setup");
											arena.setRunningModels(new Set());
											arena.setWinnerModal(null);
											arena.setDisabledModels(new Set());
											arena.toast("Arena cleared", "info");
										}}
										title="Clear results (keep models & prompt)"
										color="amber"
										pulse={
											arena.phase === "finished" || arena.phase === "voting"
										}
									/>
								)}
								{/* Full reset: clear everything */}
								<ActionIconButton
									icon={RotateCcw}
									onClick={() => arena.setPendingFullReset(true)}
									title="Reset all (clear models & prompt)"
									color="red"
								/>
							</>
						)}
						<CollapsibleToggle
							collapsed={arena.arenaCollapsed}
							onToggle={() => arena.setArenaCollapsed((c: boolean) => !c)}
						/>
					</div>
				</div>
				<div
					className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
						arena.arenaCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
					}`}
				>
					<div className="overflow-hidden">
						<div className="space-y-4 pt-4">
							{arena.phase === "setup" && arena.arenaMode === "competition" && (
								<div>
									<label
										htmlFor="bracket-models-picker"
										className="text-sm font-semibold text-(--accent) mb-2 block"
									>
										Models ({arena.bracketModels.length}/8)
										<span className="text-(--text-tertiary)">
											{" "}
											Pick 2, 4, or 8 for a bracket
										</span>
									</label>
									<ModelPicker
										id="bracket-models-picker"
										models={arena.enabledModels}
										selected={arena.bracketModels}
										onChange={arena.setBracketModels}
										multi={true}
										maxSelections={8}
										align="left"
										slotParams={arena.modelParams}
										onConfigureParams={arena.setParamEditorModel}
										onRandom={arena.handleRandomBracketModel}
										paramsReadonly={arena.phase !== "setup"}
									/>
								</div>
							)}
							{arena.phase === "setup" && arena.arenaMode === "compare" && (
								<div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
									<div>
										<label
											htmlFor="compare-models-picker"
											className="text-sm font-semibold text-(--accent) mb-2 block"
										>
											Models ({arena.compareModels.length}/6)
										</label>
										<ModelPicker
											id="compare-models-picker"
											models={arena.enabledModels}
											selected={arena.compareModels}
											onChange={arena.setCompareModels}
											multi={true}
											maxSelections={6}
											align="left"
											slotParams={arena.modelParams}
											onConfigureParams={arena.setParamEditorModel}
											onRandom={arena.handleRandomCompareModel}
											paramsReadonly={arena.phase !== "setup"}
										/>
									</div>
									<div>
										<PersonaPicker
											personas={CHAT_PERSONAS}
											activePersonaId={arena.comparePersonaId}
											systemPrompt={arena.comparePersonaPrompt}
											onActivePersonaChange={arena.setComparePersonaId}
											onSystemPromptChange={arena.setComparePersonaPrompt}
											onRandom={arena.handleRandomComparePersona}
											textareaPlaceholder="Optional system prompt applied to all models…"
										/>
									</div>
								</div>
							)}

							{/* Prompt */}
							<PromptPicker
								prompts={ARENA_PROMPTS}
								activePromptId={arena.activePromptId}
								prompt={
									arena.phase === "setup" || arena.phase === "finished"
										? arena.prompt
										: arena.savedPrompt
								}
								onActivePromptIdChange={arena.setActivePromptId}
								onPromptChange={arena.setPrompt}
								showPresetBar={arena.phase === "setup"}
								autoFocus
								disabled={arena.phase !== "setup" && arena.phase !== "finished"}
							/>
						</div>
					</div>
				</div>
			</div>

			{/* Bracket + Run Bar */}
			<div className="ui-card p-4 shrink-0">
				<div className="flex items-center gap-4 flex-wrap">
					{/* Bracket Pills */}
					{/* Setup preview: show selected models and matchups before running */}
					{arena.previewPairs && (
						<div className="flex flex-col gap-2 flex-1 min-w-0">
							<div className="flex items-center gap-2">
								<div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider whitespace-nowrap">
									First Round
								</div>
								<div className="flex items-center gap-2 flex-wrap">
									{arena.previewPairs.map((p, i) => (
										<div
											// biome-ignore lint/suspicious/noArrayIndexKey: preview position is stable for the static preview
											key={`preview-mu-${i}`}
											className="flex items-center gap-2"
										>
											<BracketPreviewPill modelId={p.a} isTbd={p.a === ""} />
											<span className="text-(--accent) font-bold text-xs px-1">
												VS
											</span>
											<BracketPreviewPill modelId={p.b} isTbd={p.b === ""} />
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
										// biome-ignore lint/suspicious/noArrayIndexKey: preview list order matches model order
										<BracketPreviewPill key={`preview-cmp-${i}`} modelId={m} />
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
											(
												roundIdx > arena.currentRound &&
													arena.phase === "voting"
											)
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
																VS
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

					{/* Run Button */}
					<div className="flex flex-col">
						{arena.buttonLabel && (
							<button
								type="button"
								onClick={
									arena.isRunning ? arena.handleStopAll : arena.handleRunArena
								}
								disabled={arena.phase === "setup" && !arena.canRun}
								className={`ui-btn flex items-center gap-2 shrink-0 ${
									arena.isRunning ? "ui-btn-danger" : "ui-btn-primary"
								} disabled:opacity-40`}
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
						)}
						{arena.phase === "setup" &&
							!arena.canRun &&
							arena.disabledReason && (
								<p className="text-xs text-amber-400 mt-1.5">
									{arena.disabledReason}
								</p>
							)}
						{arena.phase === "running" && (
							<p className="text-xs text-(--text-muted) mt-1.5">
								<span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse inline-block mr-1.5 align-middle" />
								Models are generating - click Stop to cancel
							</p>
						)}
						{arena.phase === "voting" && (
							<p className="text-xs text-amber-400 mt-1.5">
								Vote on all matchups to continue to the next round
							</p>
						)}
						{arena.phase === "next_round_ready" && !arena.canRun && (
							<p className="text-xs text-amber-400 mt-1.5">
								{arena.disabledReason || "Start the next round when ready"}
							</p>
						)}
					</div>
				</div>

				{/* Mode Description */}
				<p className="text-xs text-(--text-tertiary) leading-snug line-clamp-3 mt-3">
					{arena.arenaMode === "competition"
						? "Models compete in a single-elimination bracket. Pick 2, 4, or 8 models - each round, pairs face the same prompt and you vote for the better response. Winners advance until one model remains."
						: "Pick models and run the same prompt through them simultaneously. No voting, no bracket - just pure side-by-side output comparison to evaluate which model best fits your needs."}
				</p>
			</div>

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
									? "Responses"
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
														alreadyUsed={round.matchups.flatMap((m, mi) => {
															if (mi === matchupIdx) return [];
															const ids: string[] = [];
															if (m.slotA) ids.push(m.slotA.modelId);
															return ids;
														})}
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
													Match {matchupIdx + 1}
												</div>
											)}
											<div className="grid grid-cols-1 md:grid-cols-2 gap-4 flex-1 min-h-0">
												{mu.slotA === null &&
												roundIdx === arena.currentRound ? (
													<SwapPicker
														enabledModels={arena.enabledModels}
														disabledModels={arena.disabledModels}
														alreadyUsed={[
															...round.matchups.flatMap((m, mi) => {
																if (mi === matchupIdx) return [];
																const ids: string[] = [];
																if (m.slotA) ids.push(m.slotA.modelId);
																if (m.slotB) ids.push(m.slotB.modelId);
																return ids;
															}),
															...(mu.slotB ? [mu.slotB.modelId] : []),
														]}
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
														alreadyUsed={[
															...round.matchups.flatMap((m, mi) => {
																if (mi === matchupIdx) return [];
																const ids: string[] = [];
																if (m.slotA) ids.push(m.slotA.modelId);
																if (m.slotB) ids.push(m.slotB.modelId);
																return ids;
															}),
															...(mu.slotA ? [mu.slotA.modelId] : []),
														]}
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
					title="Reset All"
					message="This will clear all models, prompts, personas, and any in-progress results. Continue?"
					fields={[]}
					confirmLabel="Reset All"
					onConfirm={() => {
						for (const [, ctrl] of arena.abortMapRef.current) {
							ctrl.abort();
						}
						arena.abortMapRef.current.clear();
						arena.setCompareModels([]);
						arena.setBracketModels([]);
						arena.setCompetitionPrompt("");
						arena.setComparePrompt("");
						arena.setSavedPrompt("");
						arena.setCompetitionActivePromptId(null);
						arena.setCompareActivePromptId(null);
						arena.setComparePersonaId(null);
						arena.setComparePersonaPrompt("");
						arena.setRounds([]);
						arena.setCurrentRound(0);
						arena.setPhase("setup");
						arena.setRunningModels(new Set());
						arena.setWinnerModal(null);
						arena.setDisabledModels(new Set());
						arena.setModelParams({});
						arena.setPendingFullReset(false);
						try {
							localStorage.removeItem("arenaCompetitionPrompt");
							localStorage.removeItem("arenaComparePrompt");
							localStorage.removeItem("arenaCompetitionActivePromptId");
							localStorage.removeItem("arenaCompareActivePromptId");
							localStorage.removeItem("arenaComparePersonaId");
							localStorage.removeItem("arenaComparePersonaPrompt");
						} catch {
							/* ignore */
						}
						arena.toast("Reset", "info");
					}}
					onCancel={() => arena.setPendingFullReset(false)}
				/>
			)}

			{/* Winner Modal */}
			{arena.winnerModal && (
				<WinnerSummaryModal
					winner={arena.winnerModal.winner}
					rounds={arena.winnerModal.rounds}
					onClose={() => arena.setWinnerModal(null)}
				/>
			)}

			{/* Inline Param Editor */}
			{arena.paramEditorModel && (
				<ParamEditorModal
					modelId={arena.paramEditorModel}
					params={arena.modelParams[arena.paramEditorModel] ?? {}}
					onChange={(params) => {
						const model = arena.paramEditorModel;
						if (model) {
							arena.setModelParams((prev) => ({
								...prev,
								[model]: params,
							}));
						}
					}}
					onClose={() => arena.setParamEditorModel(null)}
					knownProviders={arena.enabledModels.map((m) => m.provider_name)}
					reasoning={(() => {
						const model = arena.enabledModels.find(
							(m) =>
								`${m.provider_name}/${m.model_id}` === arena.paramEditorModel,
						);
						return model
							? (parseCapabilities(model.capabilities).reasoning ?? false)
							: false;
					})()}
				/>
			)}

			{/* Match History Modal */}
			{arena.showHistoryModal && (
				<ArenaHistoryModal onClose={() => arena.setShowHistoryModal(false)} />
			)}
		</div>
	);
}
