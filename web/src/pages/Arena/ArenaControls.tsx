import { useTranslation } from "react-i18next";
import { Columns3, Eraser, History, RotateCcw, Swords } from "@/lib/icons";
import { ActionIconButton } from "../../components/ActionIconButton";
import { CollapsibleToggle } from "../../components/CollapsibleToggle";
import { ModelPicker } from "../../components/ModelPicker";
import { PersonaPicker } from "../../components/PersonaPicker";
import { PromptPicker } from "../../components/PromptPicker";
import { SubModeToggle } from "../../components/SubModeToggle";
import { ARENA_PROMPTS, CHAT_PERSONAS } from "../../data/presets";
import type { ArenaRefs, ArenaView } from "./useArena";

/** The controls card: sub-mode, prompt, persona and model selection. */
export function ArenaControls({
	arena,
	abortMapRef,
}: {
	arena: ArenaView;
	abortMapRef: ArenaRefs["abortMapRef"];
}) {
	const { t } = useTranslation();
	return (
		<>
			{/* Controls */}
			<div className="ui-card p-4">
				<div className="flex items-center justify-between">
					<div className="flex items-center gap-3">
						<span className="text-sm font-semibold text-(--text-primary)">
							{t("arena.controls.label")}
						</span>
						<SubModeToggle
							options={[
								{
									value: "competition" as const,
									label: t("arena.mode.arena"),
									icon: Swords,
								},
								{
									value: "compare" as const,
									label: t("arena.mode.compare"),
									icon: Columns3,
								},
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
							className="ui-icon-btn p-1.5 rounded-md"
							title={t("arena.matchHistory.title")}
							aria-label={t("arena.matchHistory.title")}
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
											for (const [, ctrl] of abortMapRef.current) {
												ctrl.abort();
											}
											abortMapRef.current.clear();
											arena.setRounds([]);
											arena.setCurrentRound(0);
											arena.setPhase("setup");
											arena.setRunningModels(new Set());
											arena.setWinnerModal(null);
											arena.setDisabledModels(new Set());
											arena.toast(t("arena.toast.cleared"), "info");
										}}
										title={t("arena.clearResults.title")}
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
									title={t("arena.resetAll.title")}
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
										{t("arena.models.bracketCount", {
											count: arena.bracketModels.length,
										})}
										<span className="text-(--text-tertiary)">
											{" "}
											{t("arena.models.bracketHint")}
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
											{t("arena.models.compareCount", {
												count: arena.compareModels.length,
											})}
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
											textareaPlaceholder={t(
												"arena.persona.textareaPlaceholder",
											)}
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
		</>
	);
}
