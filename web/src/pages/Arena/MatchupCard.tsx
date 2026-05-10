import { AlertCircle, Bot, Trophy } from "lucide-react";
import { useState } from "react";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { PresetBar } from "../../components/PresetBar";
import { CHAT_PERSONAS } from "../../data/presets";
import { SlotParamsTooltip, VoteThumb } from "./shared";
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
	const [pendingPersona, setPendingPersona] = useState<
		import("../../data/presets").PersonaPreset | null
	>(null);

	if (!slot) {
		return (
			<div className="px-4 py-2 rounded-lg bg-(--surface) border border-dashed border-(--border-subtle) text-xs text-(--text-tertiary) min-w-35 text-center">
				TBD
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
				<span className="text-xs font-medium text-(--text-primary) truncate">
					{slot.modelId.split("/").pop()}
				</span>
				<SlotParamsTooltip params={slot.params} />
				{isRunning && !response?.done && (
					<span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse shrink-0" />
				)}
				{response?.error && (
					<AlertCircle size={12} className="text-red-400 shrink-0" />
				)}
				{phase === "finished" && isWinner && (
					<span title="Winner">
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
								? "cursor-pointer text-(--text-tertiary) hover:text-(--text-secondary)"
								: "cursor-default"
						} ${isWinner ? "text-green-400" : ""}`}
						title={vote === null ? "Vote for this response" : undefined}
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
						onSelect={(persona) => {
							if (slot.personaPrompt.trim() && slot.personaId === null) {
								setPendingPersona(persona);
								return;
							}
							onPersonaChange(
								roundIdx,
								matchupIdx,
								slotKey,
								persona.id,
								persona.systemPrompt,
							);
						}}
						onCustom={() => {
							if (slot.personaId !== null) {
								setPendingPersona({
									id: "__custom__",
									icon: "✏️",
									label: "Custom",
									systemPrompt: "",
								} as import("../../data/presets").PersonaPreset);
								return;
							}
						}}
						onRandom={() => {
							const available = CHAT_PERSONAS.filter(
								(p) => p.id !== slot.personaId,
							);
							if (available.length === 0) return;
							const pick =
								available[Math.floor(Math.random() * available.length)];
							if (slot.personaPrompt.trim() && slot.personaId === null) {
								setPendingPersona(pick);
								return;
							}
							onPersonaChange(
								roundIdx,
								matchupIdx,
								slotKey,
								pick.id,
								pick.systemPrompt,
							);
						}}
						customLabel="✏️"
					/>
				</div>
			)}

			{pendingPersona && (
				<ConfirmDialog
					title={
						pendingPersona.id === "__custom__"
							? "Switch to Custom"
							: "Overwrite Persona"
					}
					fields={["Persona"]}
					onConfirm={() => {
						if (pendingPersona.id === "__custom__") {
							onPersonaChange(roundIdx, matchupIdx, slotKey, null, "");
						} else {
							onPersonaChange(
								roundIdx,
								matchupIdx,
								slotKey,
								pendingPersona.id,
								pendingPersona.systemPrompt,
							);
						}
						setPendingPersona(null);
					}}
					onCancel={() => setPendingPersona(null)}
				/>
			)}
		</div>
	);
}
