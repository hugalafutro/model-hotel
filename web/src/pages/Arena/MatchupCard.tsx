import { useTranslation } from "react-i18next";
import { AlertCircle, Bot, Trophy } from "@/lib/icons";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { ParamsTooltip } from "../../components/ParamsTooltip";
import { PresetBar } from "../../components/PresetBar";
import { usePresetText } from "../../components/usePresetText";
import { CHAT_PERSONAS, type PersonaPreset } from "../../data/presets";
import { shortModelName } from "../../utils/model";
import { VoteThumb } from "./shared";
import type { MatchupCardProps } from "./types";

export function MatchupCard({
	slot,
	slotKey,
	roundIdx,
	matchupIdx,
	vote,
	response,
	isRunning,
	phase,
	onPersonaChange,
	onVote,
}: MatchupCardProps) {
	const { t } = useTranslation();
	const persona = usePresetText<PersonaPreset>({
		activeId: slot?.personaId ?? null,
		text: slot?.personaPrompt ?? "",
		textOf: (p) => p.systemPrompt,
		onChange: (id, prompt) =>
			onPersonaChange(roundIdx, matchupIdx, slotKey, id, prompt),
	});

	if (!slot) {
		return (
			<div className="px-4 py-2 rounded-lg bg-(--surface) border border-dashed border-(--border-subtle) text-xs text-(--text-tertiary) min-w-35 text-center">
				{t("arena.tbd")}
			</div>
		);
	}

	const isVotingPhase =
		(phase === "voting" ||
			phase === "next_round_ready" ||
			phase === "finished") &&
		response?.done;
	const isWinner = vote === slotKey;
	const isLoser = vote !== null && vote !== slotKey;

	return (
		<div
			className={`px-3 py-2 rounded-lg border min-w-40 transition-all ${
				isWinner
					? "bg-green-500/10 border-green-500/40 shadow-[0_0_8px_rgba(34,197,94,0.15)]"
					: isLoser
						? "bg-red-500/5 border-red-500/20 opacity-60"
						: "bg-(--surface) border-(--border-subtle)"
			}`}
		>
			<div className="flex items-center gap-2 mb-1">
				<Bot size={12} className="text-(--accent)" />
				<span
					className="text-xs font-medium text-(--text-primary) truncate"
					title={slot.modelId}
				>
					{shortModelName(slot.modelId)}
				</span>
				<ParamsTooltip params={slot.params} />
				{isRunning && !response?.done && (
					<span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse shrink-0" />
				)}
				{response?.error && (
					<AlertCircle size={12} className="text-red-400 shrink-0" />
				)}
				{phase === "finished" && isWinner && (
					<span title={t("arena.winner.title")}>
						<Trophy size={14} className="text-amber-400 shrink-0" />
					</span>
				)}
				{isVotingPhase && phase !== "finished" && (
					<button
						type="button"
						onClick={
							vote === null
								? () => onVote(roundIdx, matchupIdx, slotKey)
								: undefined
						}
						disabled={vote !== null}
						className={`flex items-center text-xs transition-all ${
							vote === null
								? "text-(--text-tertiary) hover:text-(--text-secondary)"
								: "cursor-default"
						} ${isWinner ? "text-green-400" : ""}`}
						title={vote === null ? t("arena.vote.title") : undefined}
					>
						<VoteThumb
							size={14}
							isWinner={isWinner}
							animating={!isWinner && !isLoser}
						/>
					</button>
				)}
			</div>

			{phase === "setup" && roundIdx === 0 && (
				<div className="mt-1">
					<PresetBar
						items={CHAT_PERSONAS}
						activeId={slot.personaId}
						onSelect={persona.select}
						onCustom={persona.custom}
						onRandom={() => persona.random(CHAT_PERSONAS)}
						customLabel="✏️"
					/>
				</div>
			)}

			{persona.pending && (
				<ConfirmDialog
					title={
						persona.isCustomPending
							? t("arena.persona.switchToCustom")
							: t("arena.persona.overwrite")
					}
					fields={[t("arena.persona.fieldLabel")]}
					onConfirm={persona.confirm}
					onCancel={persona.cancel}
				/>
			)}
		</div>
	);
}
