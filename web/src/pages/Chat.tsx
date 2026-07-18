import { useTranslation } from "react-i18next";
import {
	Bot,
	CircleStop,
	Eraser,
	Gauge,
	Image as ImageIcon,
	MessageSquare,
	Mic,
	RotateCcw,
	Send,
	Timer,
	Users,
	X,
} from "@/lib/icons";
import { ActionIconButton } from "../components/ActionIconButton";
import { CollapsibleToggle } from "../components/CollapsibleToggle";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ConversationConfig } from "../components/ConversationConfig";
import { ModelDetailPanel } from "../components/ModelDetailPanel";
import { ModelPicker } from "../components/ModelPicker";
import { PageHeader } from "../components/PageHeader";
import { PersonaPicker } from "../components/PersonaPicker";
import { SubModeToggle } from "../components/SubModeToggle";
import { CHAT_PERSONAS } from "../data/presets";
import { formatTokens } from "../utils/format";
import { ChatMessageList } from "./Chat/ChatMessageList";

import { useChat } from "./Chat/useChat";

export function Chat() {
	const { t } = useTranslation();
	// Refs are split off so the ref-free `chat` rest object can be read freely
	// in the JSX without tripping the react-hooks/refs lint.
	const {
		refs: { lastPromptRef, messagesContainerRef, imageInputRef, audioInputRef },
		...chat
	} = useChat();

	return (
		<div
			className={`flex flex-col gap-6 ${chat.chatSubMode === "conversation" ? "min-h-full" : "h-full overflow-hidden"}`}
		>
			{/* Header */}
			<PageHeader
				icon={chat.chatIcon}
				title={
					chat.chatSubMode === "chat"
						? t("chat.misc.titleChat")
						: t("chat.misc.titleConversation")
				}
				description={
					chat.chatSubMode === "chat"
						? t("chat.misc.descriptionChat")
						: t("chat.misc.descriptionConversation")
				}
			/>

			{/* Controls */}
			<div className="ui-card p-4 shrink-0">
				<div className="flex items-center justify-between">
					<div className="flex items-center gap-3">
						<span className="text-sm font-semibold text-(--text-primary)">
							{t("chat.controls.title")}
						</span>
						<SubModeToggle
							options={[
								{
									value: "chat" as const,
									label: t("chat.chatWithAi"),
									icon: MessageSquare,
								},
								{
									value: "conversation" as const,
									label: t("chat.aiConversation"),
									icon: Users,
								},
							]}
							value={chat.chatSubMode}
							onChange={chat.setChatSubMode}
						/>
					</div>
					<div className="flex items-center gap-1">
						{(chat.messages.length > 0 ||
							(chat.chatSubMode === "conversation" &&
								(chat.conversationState === "completed" ||
									chat.conversationState === "paused" ||
									chat.conversationState === "error")) ||
							chat.selectedModel ||
							(chat.chatSubMode === "conversation" && chat.selectedModelB) ||
							!!chat.activePersonaId ||
							!!chat.systemPrompt.trim() ||
							(chat.chatSubMode === "conversation" &&
								(!!chat.activePersonaIdB || !!chat.systemPromptB.trim()))) && (
							<>
								{chat.isStreaming && chat.chatSubMode === "chat" && (
									<ActionIconButton
										icon={CircleStop}
										onClick={() => {
											chat.setControlsCollapsed(false);
											chat.handleStop();
										}}
										title={t("chat.controls.stop")}
										color="red"
									/>
								)}
								{/* Light reset: clear messages/results only, keep model/persona/params */}
								{chat.messages.length > 0 && (
									<ActionIconButton
										icon={Eraser}
										onClick={() => {
											if (chat.chatSubMode === "conversation") {
												chat.clearConversationAbort();
											}
											chat.setMessages([]);
											chat.setInput(lastPromptRef.current);
											chat.setConversationState("idle");
											chat.setCurrentTurn(0);
											chat.setTurnCountdown(0);
											chat.setIsStreaming(false);
											chat.toast(
												chat.chatSubMode === "chat"
													? t("chat.toast.chatCleared")
													: t("chat.toast.conversationCleared"),
												"info",
											);
										}}
										title={t("chat.controls.clearMessages")}
										color="amber"
										pulse={
											chat.chatSubMode === "conversation" &&
											(chat.conversationState === "completed" ||
												chat.conversationState === "paused" ||
												chat.conversationState === "error")
										}
									/>
								)}
								<ActionIconButton
									icon={RotateCcw}
									onClick={() => chat.setPendingFullReset(true)}
									title={t("chat.controls.resetAll")}
									color="red"
								/>
							</>
						)}
						<CollapsibleToggle
							collapsed={chat.controlsCollapsed}
							onToggle={() => chat.setControlsCollapsed((c) => !c)}
						/>
					</div>
				</div>
				<div
					className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
						chat.controlsCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
					}`}
				>
					<div className="overflow-hidden">
						<div className="space-y-4 pt-4">
							{chat.chatSubMode === "chat" ? (
								<>
									<ModelPicker
										models={chat.enabledModels}
										selected={chat.selectedModel}
										onChange={chat.setSelectedModel}
										multi={false}
										onRandom={chat.handleRandomModel}
									/>
									<PersonaPicker
										personas={CHAT_PERSONAS}
										activePersonaId={chat.activePersonaId}
										systemPrompt={chat.systemPrompt}
										onActivePersonaChange={chat.setActivePersonaId}
										onSystemPromptChange={chat.setSystemPrompt}
										onRandom={chat.handleRandomPersona}
									/>
								</>
							) : (
								<div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
									<div>
										<label
											htmlFor="model-a-picker"
											className="text-sm font-semibold text-(--accent) mb-2 block"
										>
											{t("chat.controls.modelA")}
										</label>
										<ModelPicker
											id="model-a-picker"
											models={chat.enabledModels}
											selected={chat.selectedModel}
											onChange={chat.setSelectedModel}
											multi={false}
											onRandom={chat.handleRandomModel}
											disabled={chat.conversationState === "running"}
										/>
										<div className="mt-3">
											<PersonaPicker
												personas={CHAT_PERSONAS}
												activePersonaId={chat.activePersonaId}
												systemPrompt={chat.systemPrompt}
												onActivePersonaChange={chat.setActivePersonaId}
												onSystemPromptChange={chat.setSystemPrompt}
												onRandom={chat.handleRandomPersona}
												label={t("chat.controls.personaA")}
												disabled={chat.conversationState === "running"}
											/>
										</div>
									</div>
									<div>
										<label
											htmlFor="model-b-picker"
											className="text-sm font-semibold text-(--accent) mb-2 block"
										>
											{t("chat.controls.modelB")}
										</label>
										<ModelPicker
											id="model-b-picker"
											models={chat.enabledModels}
											selected={chat.selectedModelB}
											onChange={chat.setSelectedModelB}
											multi={false}
											onRandom={chat.handleRandomModelB}
											disabled={chat.conversationState === "running"}
										/>
										<div className="mt-3">
											<PersonaPicker
												personas={CHAT_PERSONAS}
												activePersonaId={chat.activePersonaIdB}
												systemPrompt={chat.systemPromptB}
												onActivePersonaChange={chat.setActivePersonaIdB}
												onSystemPromptChange={chat.setSystemPromptB}
												onRandom={chat.handleRandomPersonaB}
												label={t("chat.controls.personaB")}
												disabled={chat.conversationState === "running"}
											/>
										</div>
									</div>
								</div>
							)}
						</div>
					</div>
				</div>
			</div>

			{/* Conversation Config */}
			{chat.chatSubMode === "conversation" && (
				<ConversationConfig
					maxTurns={chat.maxTurns}
					onMaxTurnsChange={chat.setMaxTurns}
					turnDelayMs={chat.turnDelayMs}
					onTurnDelayMsChange={chat.setTurnDelayMs}
					conversationState={chat.conversationState}
					currentTurn={chat.currentTurn}
					turnCountdown={chat.turnCountdown}
					configCollapsed={chat.configCollapsed}
					onToggleCollapsed={() => chat.setConfigCollapsed((c) => !c)}
					input={chat.input}
					onInputChange={chat.setInput}
					onStart={() => {
						chat.setControlsCollapsed(true);
						chat.runConversation(false);
					}}
					onContinue={() => {
						chat.setControlsCollapsed(true);
						chat.runConversation(true);
					}}
					onRetry={() => {
						chat.setControlsCollapsed(true);
						chat.handleRetryConversation();
					}}
					onStop={() => {
						chat.setControlsCollapsed(false);
						chat.handleStopConversation();
					}}
					canStart={chat.canStartConversation}
					disabledReason={chat.conversationDisabledReason}
					selectedModel={chat.selectedModel}
					selectedModelB={chat.selectedModelB}
					failedModel={chat.failedConversationModel}
				/>
			)}

			{/* Chat Area: Model Details + Messages */}
			<div
				className={`flex gap-4 flex-1 ${chat.chatSubMode === "conversation" ? "overflow-visible" : "min-h-0 overflow-hidden"}`}
			>
				{/* Sidebar */}
				<div
					className={`shrink-0 flex flex-col ${
						chat.chatSubMode === "conversation"
							? "w-1/3 gap-3 overflow-visible"
							: "min-h-0 overflow-y-auto w-1/4"
					}`}
				>
					{chat.chatSubMode === "chat" ? (
						chat.selectedModelObj ? (
							<ModelDetailPanel
								model={chat.selectedModelObj}
								params={chat.messageParams}
								onParamsChange={chat.setMessageParams}
								pulseBorder={
									chat.isStreaming &&
									chat.chatSubMode === "chat" &&
									chat.messages.length > 0 &&
									chat.messages[chat.messages.length - 1].role ===
										"assistant" &&
									chat.messages[chat.messages.length - 1].model ===
										chat.chatSelectedModel
								}
							/>
						) : (
							<div className="ui-card p-4 flex flex-col items-center justify-center text-(--text-tertiary) text-xs">
								<Bot size={32} strokeWidth={1} className="mb-2 opacity-40" />
								<p>{t("chat.placeholder.selectModel")}</p>
							</div>
						)
					) : (
						<>
							{chat.selectedModelObj ? (
								<ModelDetailPanel
									model={chat.selectedModelObj}
									params={chat.messageParams}
									onParamsChange={chat.setMessageParams}
									collapsible
									tint="default"
									pulseBorder={
										chat.isStreaming &&
										chat.messages.length > 0 &&
										chat.messages[chat.messages.length - 1].role ===
											"assistant" &&
										chat.messages[chat.messages.length - 1].model ===
											chat.selectedModel
									}
								/>
							) : (
								<div className="ui-card p-3 flex items-center justify-center text-(--text-tertiary) text-xs">
									<Bot size={20} className="mr-2 opacity-40" />
									{t("chat.placeholder.selectModelA")}
								</div>
							)}
							{chat.selectedModelObjB ? (
								<ModelDetailPanel
									model={chat.selectedModelObjB}
									params={chat.messageParamsB}
									onParamsChange={chat.setMessageParamsB}
									collapsible
									tint="blue"
									pulseBorder={
										chat.isStreaming &&
										chat.messages.length > 0 &&
										chat.messages[chat.messages.length - 1].role ===
											"assistant" &&
										chat.messages[chat.messages.length - 1].model ===
											chat.selectedModelB
									}
								/>
							) : (
								<div className="ui-card p-3 flex items-center justify-center text-(--text-tertiary) text-xs">
									<Bot size={20} className="mr-2 opacity-40" />
									{t("chat.placeholder.selectModelB")}
								</div>
							)}
						</>
					)}
				</div>

				{/* Messages */}
				<div
					ref={messagesContainerRef}
					className={`flex-1 pr-1 space-y-4 ${
						chat.chatSubMode === "conversation"
							? "overflow-visible"
							: "min-h-0 overflow-y-auto pb-4"
					}`}
				>
					{chat.messages.length === 0 && (
						<div className="flex flex-col items-center justify-center py-20 text-(--text-tertiary)">
							{chat.chatSubMode === "chat" ? (
								<Bot size={48} strokeWidth={1} className="mb-4 opacity-40" />
							) : (
								<div className="relative mb-4 w-20 h-12 flex items-center justify-center">
									<Bot
										size={48}
										strokeWidth={1}
										className="opacity-40 absolute left-0"
									/>
									<Bot
										size={48}
										strokeWidth={1}
										className="opacity-40 absolute right-0 scale-x-[-1]"
									/>
								</div>
							)}
							<p>
								{chat.chatSubMode === "chat"
									? t("chat.message.emptyChat")
									: t("chat.message.emptyConversation")}
							</p>
						</div>
					)}

					<ChatMessageList
						messages={chat.messages}
						chatSubMode={chat.chatSubMode}
						isStreaming={chat.isStreaming}
						selectedModelB={chat.selectedModelB}
						enabledModels={chat.enabledModels}
						onStopConversation={chat.handleStopConversation}
						onStop={chat.handleStop}
						onRegenerate={chat.handleRegenerate}
						onDeleteMessage={chat.handleDeleteMessage}
						activePersonaIdB={chat.activePersonaIdB}
						conversationActivePersonaIdA={chat.conversationActivePersonaIdA}
						chatActivePersonaId={chat.chatActivePersonaId}
					/>
				</div>
			</div>

			{/* Input / Stats Area - chat mode input bar + conversation stats when active */}
			{chat.chatSubMode === "chat" && (
				<div className="ui-card p-4 shrink-0">
					<div className="space-y-2">
						{/* Attachment preview row */}
						{(chat.pendingImage || chat.pendingAudio) && (
							<div className="flex items-center gap-2 flex-wrap">
								{chat.pendingImage && (
									<div className="relative group inline-block">
										<img
											src={chat.pendingImage.dataUrl}
											alt={chat.pendingImage.name}
											className="h-16 w-16 object-cover rounded-lg border border-(--border)"
										/>
										<button
											type="button"
											onClick={() => chat.setPendingImage(null)}
											className="absolute -top-1.5 -right-1.5 bg-red-500/90 hover:bg-red-400 text-white rounded-full w-4 h-4 flex items-center justify-center text-[10px] leading-none"
											title={t("chat.aria.removeImage")}
											aria-label={t("chat.aria.removeImage")}
										>
											×
										</button>
									</div>
								)}
								{chat.pendingAudio && (
									<div className="flex items-center gap-1.5 px-2 py-1 rounded-lg bg-(--surface) border border-(--border) text-xs text-(--text-secondary)">
										<Mic size={12} />
										<span
											className="max-w-[120px] truncate"
											title={chat.pendingAudio.name}
										>
											{chat.pendingAudio.name}
										</span>
										<button
											type="button"
											onClick={() => chat.setPendingAudio(null)}
											className="text-red-400 hover:text-red-300 ml-0.5"
											title={t("chat.aria.removeAudio")}
											aria-label={t("chat.aria.removeAudio")}
										>
											×
										</button>
									</div>
								)}
							</div>
						)}
						<div className="flex items-center gap-3">
							{/* Attachment buttons */}
							{chat.selectedModel && !chat.isStreaming && (
								<div className="flex items-center gap-1 shrink-0">
									{chat.hasVision && (
										<>
											<input
												ref={imageInputRef}
												type="file"
												accept="image/*"
												className="hidden"
												onChange={chat.handleImageSelect}
												aria-label={t("chat.aria.uploadImage")}
											/>
											<button
												type="button"
												onClick={() => imageInputRef.current?.click()}
												className={`p-2 rounded-(--radius-button) transition-colors ${
													chat.pendingImage
														? "bg-(--accent)/20 text-(--accent)"
														: "text-(--text-tertiary) hover:text-(--text-secondary) hover:bg-(--surface)"
												}`}
												title={t("chat.aria.attachImage")}
												aria-label={t("chat.aria.attachImage")}
											>
												<ImageIcon size={18} />
											</button>
										</>
									)}
									{chat.hasAudioInput && (
										<>
											<input
												ref={audioInputRef}
												type="file"
												accept="audio/*"
												className="hidden"
												onChange={chat.handleAudioSelect}
												aria-label={t("chat.aria.uploadAudio")}
											/>
											<button
												type="button"
												onClick={() => audioInputRef.current?.click()}
												className={`p-2 rounded-(--radius-button) transition-colors ${
													chat.pendingAudio
														? "bg-(--accent)/20 text-(--accent)"
														: "text-(--text-tertiary) hover:text-(--text-secondary) hover:bg-(--surface)"
												}`}
												title={t("chat.aria.attachAudio")}
												aria-label={t("chat.aria.attachAudio")}
											>
												<Mic size={18} />
											</button>
										</>
									)}
								</div>
							)}
							<textarea
								value={chat.input}
								onChange={(e) => {
									chat.setInput(e.target.value);
									e.target.style.height = "auto";
									const el = e.target;
									requestAnimationFrame(() => {
										el.style.height = `${el.scrollHeight}px`;
									});
								}}
								onKeyDown={chat.handleKeyDown}
								onPaste={chat.handlePaste}
								placeholder={
									!chat.selectedModel
										? t("chat.placeholder.selectModelFirst")
										: chat.hasVision
											? t("chat.placeholder.messageWithImage")
											: t("chat.placeholder.message")
								}
								disabled={!chat.selectedModel || chat.isStreaming}
								title={
									!chat.selectedModel
										? t("chat.placeholder.selectModelFirst")
										: chat.isStreaming
											? t("chat.controls.generating")
											: undefined
								}
								aria-label={t("chat.aria.messageInput")}
								rows={1}
								maxLength={32000}
								className="flex-1 ui-input resize-none max-h-32 min-h-11 overflow-y-auto"
								style={{ height: "auto" }}
							/>
							<button
								type="button"
								onClick={
									chat.isStreaming
										? () => {
												chat.setControlsCollapsed(false);
												chat.handleStop();
											}
										: () => {
												chat.setControlsCollapsed(true);
												chat.handleSend();
											}
								}
								disabled={!chat.selectedModel}
								title={
									!chat.selectedModel
										? t("chat.placeholder.selectModelFirst")
										: chat.isStreaming
											? ""
											: t("chat.controls.sendMessage")
								}
								className={`ui-btn flex items-center gap-2 shrink-0 ${
									chat.isStreaming ? "ui-btn-danger" : "ui-btn-primary"
								}`}
							>
								{chat.isStreaming ? (
									<>
										<X size={16} />
										{t("chat.controls.stop")}
									</>
								) : (
									<>
										<Send size={16} />
										{t("chat.controls.send")}
									</>
								)}
							</button>
						</div>
						{!chat.selectedModel && !chat.isStreaming ? (
							<p className="text-xs text-amber-400">
								{t("chat.misc.selectModelToStart")}
							</p>
						) : chat.lastChatError ? (
							<p className="text-xs text-red-400">
								{chat.lastChatError.model
									? t("chat.modelError", {
											model: chat.lastChatError.model.split("/").pop(),
											error: chat.lastChatError.error,
										})
									: t("chat.generalError", {
											error: chat.lastChatError.error,
										})}
							</p>
						) : (
							<p className="text-xs text-(--text-muted)">
								{t("chat.misc.keyboardHint")}
							</p>
						)}
					</div>
				</div>
			)}
			{chat.chatSubMode === "conversation" &&
				(chat.conversationState === "running" ||
					chat.conversationState === "paused" ||
					chat.conversationState === "completed" ||
					chat.conversationState === "error") && (
					<div className="ui-card p-4 shrink-0">
						<div className="space-y-3">
							<div className="flex items-center justify-between flex-wrap gap-2">
								<div className="flex items-center gap-4 text-sm text-(--text-secondary)">
									<span className="flex items-center gap-1.5">
										<Gauge size={14} />
										{t("chat.misc.turnCount", {
											current: Math.ceil(chat.currentTurn / 2),
											max: chat.maxTurns,
										})}
									</span>
									<span className="flex items-center gap-1.5">
										<Timer size={14} />
										{(chat.totalDuration / 1000).toFixed(1)}s
									</span>
									<span className="flex items-center gap-1.5">
										<Bot size={14} />
										{formatTokens(chat.totalTokens)} {t("chat.misc.tokens")}
									</span>
								</div>
								<div className="flex items-center gap-2">
									{chat.isStreaming && (
										<ActionIconButton
											icon={CircleStop}
											onClick={() => {
												chat.setControlsCollapsed(false);
												chat.handleStopConversation();
											}}
											title={t("chat.controls.stop")}
											color="red"
											size={16}
											label={t("chat.controls.stop")}
											withLabel
										/>
									)}
									{chat.messages.length > 0 && (
										<ActionIconButton
											icon={Eraser}
											onClick={() => {
												chat.clearConversationAbort();
												chat.setMessages([]);
												chat.setInput(lastPromptRef.current);
												chat.setConversationState("idle");
												chat.setCurrentTurn(0);
												chat.setTurnCountdown(0);
												chat.setIsStreaming(false);
												chat.toast(t("chat.toast.conversationCleared"), "info");
											}}
											title={t("chat.clearLabel")}
											color="amber"
											size={16}
											label={t("chat.clearLabel")}
											withLabel
										/>
									)}
									<ActionIconButton
										icon={RotateCcw}
										onClick={() => chat.setPendingFullReset(true)}
										title={t("chat.controls.resetAll")}
										color="red"
										size={16}
										label={t("chat.controls.resetAll")}
										withLabel
									/>
								</div>
							</div>
							{chat.conversationState === "running" && (
								<div className="flex items-center gap-2 text-xs text-(--text-muted)">
									<span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse" />
									{chat.isStreaming
										? t("chat.misc.modelGenerating")
										: t("chat.misc.waitingNextTurn")}
								</div>
							)}
							{chat.conversationState === "error" && (
								<div className="flex items-center gap-2 text-xs text-red-400">
									<span className="w-1.5 h-1.5 rounded-full bg-red-400 shrink-0" />
									{(() => {
										const lastErr = [...chat.messages]
											.reverse()
											.find((m) => m.error);
										const modelPart = lastErr?.model
											? lastErr.model.split("/").pop()
											: "";
										return modelPart
											? t("chat.misc.generationFailed", {
													model: modelPart,
												})
											: t("chat.misc.generationFailedNoModel");
									})()}
								</div>
							)}
						</div>
					</div>
				)}

			{chat.pendingFullReset && (
				<ConfirmDialog
					title={
						chat.chatSubMode === "chat"
							? t("chat.misc.resetChatTitle")
							: t("chat.misc.resetConversationTitle")
					}
					message={
						chat.chatSubMode === "chat"
							? t("chat.misc.resetChatMessage")
							: t("chat.misc.resetConversationMessage")
					}
					fields={[]}
					confirmLabel={t("chat.misc.resetAllConfirm")}
					onConfirm={() => {
						// Abort any running conversation
						chat.clearConversationAbort();
						chat.setControlsCollapsed(false);
						chat.setMessages([]);
						chat.setInput("");
						chat.setConversationState("idle");
						chat.setCurrentTurn(0);
						chat.setTurnCountdown(0);
						chat.setIsStreaming(false);
						if (chat.chatSubMode === "chat") {
							chat.setChatSelectedModel("");
							chat.setChatSystemPrompt("");
							chat.setChatActivePersonaId(null);
							chat.setChatMessageParams({});
						} else {
							// conversation mode: also clear both models, personas, and params
							chat.setConversationModelA("");
							chat.setSelectedModelB("");
							chat.setConversationSystemPromptA("");
							chat.setSystemPromptB("");
							chat.setConversationActivePersonaIdA(null);
							chat.setActivePersonaIdB(null);
							chat.setConversationParamsA({});
							chat.setMessageParamsB({});
						}
						chat.setPendingFullReset(false);
						chat.toast(
							chat.chatSubMode === "chat"
								? t("chat.toast.chatReset")
								: t("chat.toast.conversationReset"),
							"info",
						);
					}}
					onCancel={() => chat.setPendingFullReset(false)}
				/>
			)}
		</div>
	);
}
