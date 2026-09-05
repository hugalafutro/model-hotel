import { useTranslation } from "react-i18next";
import {
	Bot,
	CircleStop,
	Eraser,
	Gauge,
	Image as ImageIcon,
	Mic,
	RotateCcw,
	Send,
	Timer,
	X,
} from "@/lib/icons";
import { ActionIconButton } from "../../components/ActionIconButton";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { formatTokens } from "../../utils/format";

import type { ChatRefs, ChatView } from "./useChat";

/** The chat-mode input bar and the conversation-mode stats panel below the messages. */
export function ChatInputArea({
	chat,
	lastPromptRef,
	imageInputRef,
	audioInputRef,
}: {
	chat: ChatView;
	lastPromptRef: ChatRefs["lastPromptRef"];
	imageInputRef: ChatRefs["imageInputRef"];
	audioInputRef: ChatRefs["audioInputRef"];
}) {
	const { t } = useTranslation();
	return (
		<>
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
								className={`ui-btn shrink-0 ${
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
		</>
	);
}
