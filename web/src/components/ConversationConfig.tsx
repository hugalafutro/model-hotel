import { useTranslation } from "react-i18next";
import {
	FastForward,
	Gauge,
	Play,
	RotateCcw,
	SlidersHorizontal,
	Square,
	Timer,
} from "@/lib/icons";
import { CollapseBody, CollapsibleToggle } from "./CollapsibleToggle";

interface ConversationConfigProps {
	maxTurns: number;
	onMaxTurnsChange: (v: number) => void;
	turnDelayMs: number;
	onTurnDelayMsChange: (v: number) => void;
	conversationState: string;
	currentTurn: number;
	turnCountdown: number;
	configCollapsed: boolean;
	onToggleCollapsed: () => void;
	input: string;
	onInputChange: (value: string) => void;
	onStart: () => void;
	/** Called when resuming a paused conversation */
	onContinue?: () => void;
	/** Called when retrying from an error state */
	onRetry?: () => void;
	/** Called to abort a running conversation */
	onStop?: () => void;
	canStart: boolean;
	disabledReason?: string;
	selectedModel: string;
	selectedModelB: string;
	/** Short name of the model that failed, e.g. "gpt-4o" */
	failedModel?: string;
}

/**
 * A number input that reports only values inside [min, max]. Typing clamps, so
 * the value handed up is always in range and no blur pass is needed.
 */
function ClampedNumberInput({
	id,
	value,
	min,
	max,
	step,
	onChange,
	disabled,
	className,
}: {
	id: string;
	value: number;
	min: number;
	max: number;
	step?: number;
	onChange: (value: number) => void;
	disabled?: boolean;
	className?: string;
}) {
	return (
		<input
			id={id}
			type="number"
			value={value}
			onChange={(e) => {
				const v = parseInt(e.target.value, 10);
				if (!Number.isNaN(v)) onChange(Math.max(min, Math.min(max, v)));
			}}
			onFocus={(e) => e.target.select()}
			min={min}
			max={max}
			step={step}
			className={className}
			disabled={disabled}
		/>
	);
}

export function ConversationConfig({
	maxTurns,
	onMaxTurnsChange,
	onTurnDelayMsChange,
	turnDelayMs,
	conversationState,
	currentTurn,
	turnCountdown,
	configCollapsed,
	onToggleCollapsed,
	input,
	onInputChange,
	onStart,
	onContinue,
	onRetry,
	onStop,
	canStart,
	disabledReason,
	selectedModel,
	selectedModelB,
	failedModel,
}: ConversationConfigProps) {
	const { t } = useTranslation();
	const isPaused = conversationState === "paused";
	const isIdle = conversationState === "idle";
	const isError = conversationState === "error";
	const showStartArea = isIdle || isPaused || isError;
	const isContinue = isPaused || (isIdle && currentTurn > 0);

	return (
		<div className="ui-card p-4 shrink-0">
			{/* Header */}
			<div className="flex items-center justify-between">
				<div className="flex items-center gap-2">
					<SlidersHorizontal size={14} className="text-(--accent)" />
					<span className="text-sm font-semibold text-(--accent)">
						{t("components.conversationConfig.title")}
					</span>
				</div>
				<div className="flex items-center gap-3">
					{/* Collapsed preview: values */}
					{configCollapsed && (
						<span className="text-xs text-(--text-muted) flex items-center gap-3">
							<span>
								{t("components.conversationConfig.rounds", {
									rounds: maxTurns,
								})}
							</span>
							<span>
								{t("components.conversationConfig.delay", {
									delay: turnDelayMs,
								})}
								ms
							</span>
						</span>
					)}
					{/* Round counter (when active) - each round = both models respond */}
					{conversationState !== "idle" && conversationState !== "paused" && (
						<span className="text-xs text-(--text-secondary) flex items-center gap-1.5">
							<Gauge size={12} />
							{t("components.conversationConfig.round", {
								current: Math.ceil(currentTurn / 2),
								max: maxTurns,
							})}
							{turnCountdown > 0 && (
								<span className="text-(--accent) ml-1">
									{t("components.conversationConfig.nextIn", {
										seconds: turnCountdown,
									})}
								</span>
							)}
						</span>
					)}
					{/* Status */}
					<span className="text-xs text-(--text-secondary) flex items-center gap-1.5">
						<Timer size={12} />
						{t("components.conversationConfig.status", {
							state: conversationState,
						})}
						<span
							className={`capitalize ${
								isError ? "text-red-400" : "text-(--text-primary)"
							}`}
						>
							{conversationState}
						</span>
					</span>
					<CollapsibleToggle
						collapsed={configCollapsed}
						onToggle={onToggleCollapsed}
						iconStyle="double"
						expandTitle={t("components.conversationConfig.expandConfig")}
						collapseTitle={t("components.conversationConfig.collapseConfig")}
					/>
				</div>
			</div>

			{/* Expandable content */}
			<CollapseBody collapsed={configCollapsed}>
				{/* Error banner */}
				{isError && (
					<div className="flex items-center gap-2 mt-3 px-3 py-2 rounded-md bg-red-500/10 border border-red-500/20 text-xs text-red-400">
						<span className="w-1.5 h-1.5 rounded-full bg-red-400 shrink-0" />
						{failedModel
							? t("components.conversationConfig.generationFailedWithModel", {
									model: failedModel,
								})
							: t("components.conversationConfig.generationFailed")}
					</div>
				)}

				{/* Compact row: Rounds + Delay + (Prompt area or Continue/Retry) */}
				<div className="flex items-end gap-3 pt-4">
					{/* Max Turns */}
					<div className="flex flex-col">
						<label
							htmlFor="cc-rounds"
							className="text-xs text-(--text-secondary) mb-1"
						>
							{t("components.conversationConfig.maxTurns")}
						</label>
						<ClampedNumberInput
							id="cc-rounds"
							value={maxTurns}
							min={1}
							max={50}
							onChange={onMaxTurnsChange}
							className="ui-input w-16 text-sm text-center"
							disabled={conversationState !== "idle"}
						/>
					</div>

					{/* Turn Delay */}
					<div className="flex flex-col">
						<label
							htmlFor="cc-delay"
							className="text-xs text-(--text-secondary) mb-1"
						>
							{t("components.conversationConfig.turnDelay")}
						</label>
						<ClampedNumberInput
							id="cc-delay"
							value={turnDelayMs}
							min={0}
							max={5000}
							step={100}
							onChange={onTurnDelayMsChange}
							className="ui-input w-20 text-sm text-center"
							disabled={conversationState !== "idle"}
						/>
					</div>

					{/* Prompt + Start/Continue/Retry */}
					{showStartArea && (
						<div className="flex items-end gap-2 flex-1 min-w-0">
							{isIdle && (
								<>
									<div className="flex flex-col flex-1 min-w-0">
										<label
											htmlFor="cc-prompt"
											className="text-xs text-(--text-secondary) mb-1"
										>
											{t("components.conversationConfig.prompt")}
										</label>
										<textarea
											id="cc-prompt"
											value={input}
											onChange={(e) => onInputChange(e.target.value)}
											placeholder={
												!selectedModel || !selectedModelB
													? t(
															"components.conversationConfig.selectBothModelsFirst",
														)
													: t(
															"components.conversationConfig.enterTopicOrQuestion",
														)
											}
											className="flex-1 ui-input resize-none overflow-y-auto text-sm min-h-9"
											style={{ height: "auto" }}
											disabled={!selectedModel || !selectedModelB}
											rows={1}
											maxLength={8000}
										/>
									</div>
									<button
										type="button"
										onClick={onStart}
										disabled={!canStart}
										className="ui-btn ui-btn-primary shrink-0"
									>
										<Play size={16} />
										{isContinue
											? t("components.conversationConfig.continue")
											: t("components.conversationConfig.start")}
									</button>
								</>
							)}
							{isPaused && (
								<button
									type="button"
									onClick={onContinue}
									className="ui-btn ui-btn-primary shrink-0"
								>
									<FastForward size={16} />
									{t("components.conversationConfig.continue")}
								</button>
							)}
							{isError &&
								(currentTurn === 0 ? (
									<>
										{/* First turn failed: show prompt input so user can re-enter or edit. The parent restores the prompt via lastPromptRef. */}
										<div className="flex flex-col flex-1 min-w-0">
											<label
												htmlFor="cc-prompt-retry"
												className="text-xs text-(--text-secondary) mb-1"
											>
												{t("components.conversationConfig.prompt")}
											</label>
											<textarea
												id="cc-prompt-retry"
												value={input}
												onChange={(e) => onInputChange(e.target.value)}
												placeholder={t(
													"components.conversationConfig.reenterOrEditPrompt",
												)}
												className="flex-1 ui-input resize-none overflow-y-auto text-sm min-h-9"
												style={{
													height: "auto",
												}}
												rows={1}
											/>
										</div>
										<button
											type="button"
											onClick={onRetry}
											disabled={!input.trim()}
											className="ui-btn ui-btn-primary shrink-0"
										>
											<RotateCcw size={16} />
											{t("components.conversationConfig.retry")}
										</button>
									</>
								) : (
									/* Later turn failed - retry from last successful turn */
									<button
										type="button"
										onClick={onRetry}
										className="ui-btn ui-btn-primary shrink-0"
									>
										<RotateCcw size={16} />
										{t("components.conversationConfig.retryFromTurn", {
											turn: Math.ceil(currentTurn / 2),
										})}
									</button>
								))}
						</div>
					)}
					{/* Stop button - visible while conversation is running */}
					{conversationState === "running" && onStop && (
						<button
							type="button"
							onClick={onStop}
							className="ui-btn ui-btn-danger shrink-0"
						>
							<Square size={16} />
							{t("components.conversationConfig.stop")}
						</button>
					)}
				</div>
				{/* Disabled reason hint */}
				{isIdle && !canStart && disabledReason && (
					<p className="text-xs text-amber-400 mt-2">{disabledReason}</p>
				)}
			</CollapseBody>
		</div>
	);
}
