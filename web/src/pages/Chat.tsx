/* eslint-disable react-hooks/refs */
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
} from "lucide-react";
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
import { ChatMessageList } from "./Chat/ChatMessageList";

import { useChat } from "./Chat/useChat";

export function Chat() {
	const chat = useChat();

	return (
		<div
			className={`flex flex-col gap-6 ${chat.chatSubMode === "conversation" ? "min-h-full" : "h-full overflow-hidden"}`}
		>
			{/* Header */}
			<PageHeader
				icon={chat.chatIcon}
				title={chat.chatSubMode === "chat" ? "Chat" : "Conversation"}
				description={
					chat.chatSubMode === "chat"
						? "Test enabled models in temporary chat"
						: "Watch two models converse with each other"
				}
			/>

			{/* Controls */}
			<div className="ui-card p-4 shrink-0">
				<div className="flex items-center justify-between">
					<div className="flex items-center gap-3">
						<span className="text-sm font-semibold text-(--text-primary)">
							Controls
						</span>
						<SubModeToggle
							options={[
								{
									value: "chat" as const,
									label: "Chat with AI",
									icon: MessageSquare,
								},
								{
									value: "conversation" as const,
									label: "AI Conversation",
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
										onClick={chat.handleStop}
										title="Stop"
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
											chat.setInput(chat.lastPromptRef.current);
											chat.setConversationState("idle");
											chat.setCurrentTurn(0);
											chat.setTurnCountdown(0);
											chat.setIsStreaming(false);
											chat.toast(
												chat.chatSubMode === "chat"
													? "Chat cleared"
													: "Conversation cleared",
												"info",
											);
										}}
										title="Clear messages (keep model & settings)"
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
									title="Reset all (clear model & settings)"
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
											Model A
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
												label="Persona A"
												disabled={chat.conversationState === "running"}
											/>
										</div>
									</div>
									<div>
										<label
											htmlFor="model-b-picker"
											className="text-sm font-semibold text-(--accent) mb-2 block"
										>
											Model B
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
												label="Persona B"
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
					onStart={() => chat.runConversation(false)}
					onContinue={() => chat.runConversation(true)}
					onRetry={chat.handleRetryConversation}
					onStop={chat.handleStopConversation}
					canStart={chat.canStartConversation}
					disabledReason={chat.conversationDisabledReason}
					selectedModel={chat.selectedModel}
					selectedModelB={chat.selectedModelB}
					failedModel={chat.failedConversationModel}
				/>
			)}

			{/* Chat Area: Model Details + Messages */}
			<div
				className={`flex gap-4 flex-1 ${chat.chatSubMode === "conversation" ? "overflow-visible" : "min-h-0 overflow-hidden p-1.5"}`}
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
								<p>Select a model</p>
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
									Select Model A
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
									Select Model B
								</div>
							)}
						</>
					)}
				</div>

				{/* Messages */}
				<div
					ref={chat.messagesContainerRef}
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
									? "Chat will appear here"
									: "Conversation will appear here"}
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
											className="absolute -top-1.5 -right-1.5 bg-red-500/90 hover:bg-red-400 text-white rounded-full w-4 h-4 flex items-center justify-center text-[10px] leading-none cursor-pointer"
											title="Remove image"
											aria-label="Remove image"
										>
											×
										</button>
									</div>
								)}
								{chat.pendingAudio && (
									<div className="flex items-center gap-1.5 px-2 py-1 rounded-lg bg-(--surface) border border-(--border) text-xs text-(--text-secondary)">
										<Mic size={12} />
										<span className="max-w-[120px] truncate">
											{chat.pendingAudio.name}
										</span>
										<button
											type="button"
											onClick={() => chat.setPendingAudio(null)}
											className="text-red-400 hover:text-red-300 cursor-pointer ml-0.5"
											title="Remove audio"
											aria-label="Remove audio"
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
												ref={chat.imageInputRef}
												type="file"
												accept="image/*"
												className="hidden"
												onChange={chat.handleImageSelect}
												aria-label="Upload image"
											/>
											<button
												type="button"
												onClick={() => chat.imageInputRef.current?.click()}
												className={`p-2 rounded-lg cursor-pointer transition-colors ${
													chat.pendingImage
														? "bg-(--accent)/20 text-(--accent)"
														: "text-(--text-tertiary) hover:text-(--text-secondary) hover:bg-(--surface)"
												}`}
												title="Attach image"
												aria-label="Attach image"
											>
												<ImageIcon size={18} />
											</button>
										</>
									)}
									{chat.hasAudioInput && (
										<>
											<input
												ref={chat.audioInputRef}
												type="file"
												accept="audio/*"
												className="hidden"
												onChange={chat.handleAudioSelect}
												aria-label="Upload audio"
											/>
											<button
												type="button"
												onClick={() => chat.audioInputRef.current?.click()}
												className={`p-2 rounded-lg cursor-pointer transition-colors ${
													chat.pendingAudio
														? "bg-(--accent)/20 text-(--accent)"
														: "text-(--text-tertiary) hover:text-(--text-secondary) hover:bg-(--surface)"
												}`}
												title="Attach audio"
												aria-label="Attach audio"
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
										? "Select a model first"
										: chat.hasVision
											? "Type a message (or paste an image)…"
											: "Type a message…"
								}
								disabled={!chat.selectedModel || chat.isStreaming}
								title={
									!chat.selectedModel
										? "Select a model first"
										: chat.isStreaming
											? "Generating…"
											: undefined
								}
								aria-label="Chat message input"
								rows={1}
								maxLength={32000}
								className="flex-1 ui-input resize-none max-h-32 min-h-11 overflow-y-auto"
								style={{ height: "auto" }}
							/>
							<button
								type="button"
								onClick={chat.isStreaming ? chat.handleStop : chat.handleSend}
								disabled={!chat.selectedModel}
								title={
									!chat.selectedModel
										? "Select a model first"
										: chat.isStreaming
											? ""
											: "Send message"
								}
								className={`ui-btn flex items-center gap-2 shrink-0 ${
									chat.isStreaming ? "ui-btn-danger" : "ui-btn-primary"
								}`}
							>
								{chat.isStreaming ? (
									<>
										<X size={16} />
										Stop
									</>
								) : (
									<>
										<Send size={16} />
										Send
									</>
								)}
							</button>
						</div>
						{!chat.selectedModel && !chat.isStreaming ? (
							<p className="text-xs text-amber-400">
								Select a model to start chatting
							</p>
						) : chat.lastChatError ? (
							<p className="text-xs text-red-400">
								{chat.lastChatError.model
									? `${chat.lastChatError.model.split("/").pop()}: ${chat.lastChatError.error} - try Regenerate`
									: `${chat.lastChatError.error} - try Regenerate or pick a model`}
							</p>
						) : (
							<p className="text-xs text-(--text-muted)">
								Press Enter to send, Shift+Enter for newline
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
										Turn {Math.ceil(chat.currentTurn / 2)} / {chat.maxTurns}
									</span>
									<span className="flex items-center gap-1.5">
										<Timer size={14} />
										{(chat.totalDuration / 1000).toFixed(1)}s
									</span>
									<span className="flex items-center gap-1.5">
										<Bot size={14} />
										{chat.totalTokens} tokens
									</span>
								</div>
								<div className="flex items-center gap-2">
									{chat.isStreaming && (
										<ActionIconButton
											icon={CircleStop}
											onClick={chat.handleStopConversation}
											title="Stop"
											color="red"
											size={16}
											label="Stop"
											withLabel
										/>
									)}
									{chat.messages.length > 0 && (
										<ActionIconButton
											icon={Eraser}
											onClick={() => {
												chat.clearConversationAbort();
												chat.setMessages([]);
												chat.setInput(chat.lastPromptRef.current);
												chat.setConversationState("idle");
												chat.setCurrentTurn(0);
												chat.setTurnCountdown(0);
												chat.setIsStreaming(false);
												chat.toast("Conversation cleared", "info");
											}}
											title="Clear"
											color="amber"
											size={16}
											label="Clear"
											withLabel
										/>
									)}
									<ActionIconButton
										icon={RotateCcw}
										onClick={() => chat.setPendingFullReset(true)}
										title="Reset All"
										color="red"
										size={16}
										label="Reset All"
										withLabel
									/>
								</div>
							</div>
							{chat.conversationState === "running" && (
								<div className="flex items-center gap-2 text-xs text-(--text-muted)">
									<span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse" />
									{chat.isStreaming
										? "Model is generating…"
										: "Waiting for next turn…"}
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
											? `${lastErr.model.split("/").pop()}: `
											: "";
										return `${modelPart}Generation failed - use Retry in config above, or Clear/Reset below`;
									})()}
								</div>
							)}
						</div>
					</div>
				)}

			{chat.pendingFullReset && (
				<ConfirmDialog
					title={
						chat.chatSubMode === "chat" ? "Reset Chat" : "Reset Conversation"
					}
					message={
						chat.chatSubMode === "chat"
							? "This will clear all messages, reset model selection, persona, and parameters. Continue?"
							: "This will clear the conversation and reset both models, personas, and parameters. Continue?"
					}
					fields={[]}
					confirmLabel="Reset All"
					onConfirm={() => {
						// Abort any running conversation
						chat.clearConversationAbort();
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
							chat.chatSubMode === "chat" ? "Chat reset" : "Conversation reset",
							"info",
						);
					}}
					onCancel={() => chat.setPendingFullReset(false)}
				/>
			)}
		</div>
	);
}
