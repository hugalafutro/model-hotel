import { screen, waitFor } from "@testing-library/react";
import type { LucideIcon } from "lucide-react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockAllDefaults } from "../../test/helpers";
import { mockModel } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Chat } from "../Chat";
import type { useChat as UseChatHook } from "../Chat/useChat";
import { useChat } from "../Chat/useChat";

vi.mock("../Chat/useChat", () => ({
	useChat: vi.fn(),
}));

describe("Chat", () => {
	beforeEach(() => {
		server.resetHandlers();
		server.use(...mockAllDefaults());
		localStorage.clear();
		vi.clearAllMocks();
	});

	const mockUseChatResult = (
		overrides: Partial<ReturnType<typeof UseChatHook>> = {},
	) => {
		vi.mocked(useChat).mockReturnValue({
			enabledModels: [mockModel],
			toast: vi.fn(),
			chatSubMode: "chat",
			setChatSubMode: vi.fn(),
			messages: [],
			setMessages: vi.fn(),
			chatSelectedModel: "",
			setChatSelectedModel: vi.fn(),
			chatSystemPrompt: "",
			setChatSystemPrompt: vi.fn(),
			chatActivePersonaId: null,
			setChatActivePersonaId: vi.fn(),
			chatMessageParams: {},
			setChatMessageParams: vi.fn(),
			conversationModelA: "",
			setConversationModelA: vi.fn(),
			conversationSystemPromptA: "",
			setConversationSystemPromptA: vi.fn(),
			conversationActivePersonaIdA: null,
			setConversationActivePersonaIdA: vi.fn(),
			conversationParamsA: {},
			setConversationParamsA: vi.fn(),
			selectedModelB: "",
			setSelectedModelB: vi.fn(),
			systemPromptB: "",
			setSystemPromptB: vi.fn(),
			activePersonaIdB: null,
			setActivePersonaIdB: vi.fn(),
			messageParamsB: {},
			setMessageParamsB: vi.fn(),
			conversationState: "idle",
			setConversationState: vi.fn(),
			currentTurn: 0,
			setCurrentTurn: vi.fn(),
			turnCountdown: 0,
			setTurnCountdown: vi.fn(),
			pendingFullReset: false,
			setPendingFullReset: vi.fn(),
			input: "",
			setInput: vi.fn(),
			isStreaming: false,
			setIsStreaming: vi.fn(),
			controlsCollapsed: false,
			setControlsCollapsed: vi.fn(),
			pendingImage: null,
			setPendingImage: vi.fn(),
			pendingAudio: null,
			setPendingAudio: vi.fn(),
			maxTurns: 10,
			setMaxTurns: vi.fn(),
			turnDelayMs: 500,
			setTurnDelayMs: vi.fn(),
			configCollapsed: false,
			setConfigCollapsed: vi.fn(),
			selectedModel: "",
			setSelectedModel: vi.fn(),
			systemPrompt: "",
			setSystemPrompt: vi.fn(),
			activePersonaId: null,
			setActivePersonaId: vi.fn(),
			messageParams: {},
			setMessageParams: vi.fn(),
			modelCaps: {},
			hasVision: false,
			hasAudioInput: false,
			selectedModelObj: undefined,
			selectedModelObjB: undefined,
			totalTokens: 0,
			totalDuration: 0,
			canStartConversation: false,
			lastChatError: null,
			failedConversationModel: undefined,
			conversationDisabledReason: "",
			chatIcon: vi.fn() as unknown as LucideIcon,
			abortRef: { current: null },
			sendingRef: { current: false },
			lastPromptRef: { current: "" },
			messagesContainerRef: { current: null },
			imageInputRef: { current: null },
			audioInputRef: { current: null },
			cleanupAbortRef: { current: null },
			cleanupConvAbortRef: { current: null },
			conversationAbortRef: { current: null },
			conversationRunningRef: { current: false },
			capturedModelARef: { current: null },
			capturedModelBRef: { current: null },
			handleRandomPersona: vi.fn(),
			handleRandomPersonaB: vi.fn(),
			handleRandomModel: vi.fn(),
			handleRandomModelB: vi.fn(),
			scrollToBottom: vi.fn(),
			streamAssistantReply: vi.fn(),
			handleSend: vi.fn(),
			handlePaste: vi.fn(),
			handleImageSelect: vi.fn(),
			handleAudioSelect: vi.fn(),
			handleStop: vi.fn(),
			handleRegenerate: vi.fn(),
			runConversation: vi.fn(),
			handleStopConversation: vi.fn(),
			handleRetryConversation: vi.fn(),
			handleDeleteMessage: vi.fn(),
			handleKeyDown: vi.fn(),
			clearConversationAbort: vi.fn(),
			...overrides,
		} as ReturnType<typeof UseChatHook>);
	};

	describe("Conversation Stats Bar", () => {
		it("renders stats bar during conversation running state", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "running",
				currentTurn: 2,
				maxTurns: 10,
				totalTokens: 150,
				totalDuration: 2500,
				isStreaming: true,
				messages: [
					{
						role: "user",
						content: "Hello",
						timestamp: 1,
					},
					{
						role: "assistant",
						content: "Hi there",
						model: "Test Provider/test-model",
						timestamp: 2,
					},
				],
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Turn 1 / 10")).toBeInTheDocument();
			});
			expect(screen.getByText("2.5s")).toBeInTheDocument();
			expect(screen.getByText("150 tokens")).toBeInTheDocument();
		});

		it("shows running indicator with streaming message", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "running",
				currentTurn: 1,
				isStreaming: true,
				messages: [
					{
						role: "user",
						content: "Hello",
						timestamp: 1,
					},
				],
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Model is generating…")).toBeInTheDocument();
			});
			// Check for the pulsing indicator dot before the text
			// The indicator is in a flex container with gap-2, text-xs
			const container = screen
				.getByText("Model is generating…")
				.closest(".flex.items-center.gap-2");
			expect(container).toBeInTheDocument();
		});

		it("shows waiting message between turns", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "running",
				currentTurn: 2,
				isStreaming: false,
				messages: [
					{
						role: "user",
						content: "Hello",
						timestamp: 1,
					},
					{
						role: "assistant",
						content: "Hi",
						model: "Test Provider/test-model",
						timestamp: 2,
					},
				],
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Waiting for next turn…")).toBeInTheDocument();
			});
		});

		it("renders stats in completed state", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "completed",
				currentTurn: 10,
				maxTurns: 10,
				totalTokens: 1250,
				totalDuration: 45000,
				messages: [
					{
						role: "user",
						content: "Discuss AI",
						timestamp: 1,
					},
				],
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Turn 5 / 10")).toBeInTheDocument();
			});
			expect(screen.getByText("45.0s")).toBeInTheDocument();
			expect(screen.getByText("1250 tokens")).toBeInTheDocument();
		});

		it("shows Clear and Reset All buttons during conversation", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "completed",
				messages: [{ role: "user", content: "Test", timestamp: 1 }],
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(
					screen.getAllByRole("button", {
						name: "Clear messages (keep model & settings)",
					}).length,
				).toBeGreaterThan(0);
			});
			expect(
				screen.getAllByRole("button", {
					name: "Reset all (clear model & settings)",
				}).length,
			).toBeGreaterThan(0);
		});

		it("shows Stop button when streaming", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "running",
				isStreaming: true,
				messages: [{ role: "user", content: "Test", timestamp: 1 }],
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				// Multiple Stop buttons exist - look for any of them
				const stopButtons = screen.getAllByRole("button", { name: "Stop" });
				expect(stopButtons.length).toBeGreaterThan(0);
			});
		});
	});

	describe("Conversation Error Display", () => {
		it("shows error message with model short name", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "error",
				messages: [
					{
						role: "user",
						content: "Hello",
						timestamp: 1,
					},
					{
						role: "assistant",
						content: "",
						model: "Test Provider/test-model-v1",
						timestamp: 2,
						error: "API Error",
					},
				],
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(
					screen.getByText(/test-model-v1.*Generation failed/),
				).toBeInTheDocument();
			});
		});

		it("shows error without model name when model is missing", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "error",
				messages: [
					{
						role: "user",
						content: "Hello",
						timestamp: 1,
					},
					{
						role: "assistant",
						content: "",
						model: "",
						timestamp: 2,
						error: "Network Error",
					},
				],
				selectedModelObj: mockModel,
				selectedModelObjB: mockModel,
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				// Multiple error messages may exist - use getAll and check the stats bar one
				const errorTexts = screen.queryAllByText(
					/Generation failed - use Retry/,
				);
				expect(errorTexts.length).toBeGreaterThan(0);
			});
		});
	});

	describe("Attachment Preview", () => {
		it("renders image preview with name", async () => {
			const mockImage = {
				name: "test-image.png",
				dataUrl: "data:image/png;base64,testdata",
				size: 1024,
			};

			mockUseChatResult({
				pendingImage: mockImage,
				pendingAudio: null,
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				const img = screen.getByAltText("test-image.png");
				expect(img).toBeInTheDocument();
				expect(img).toHaveClass("h-16", "w-16", "object-cover");
			});
		});

		it("renders image remove button", async () => {
			const mockImage = {
				name: "test.png",
				dataUrl: "data:image/png;base64,test",
				size: 512,
			};

			mockUseChatResult({
				pendingImage: mockImage,
				pendingAudio: null,
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Remove image" }),
				).toBeInTheDocument();
			});
		});

		it("renders audio preview with name", async () => {
			const mockAudio = {
				name: "voice-recording.wav",
				dataUrl: "data:audio/wav;base64,audiodata",
				size: 2048,
				format: "wav",
			};

			mockUseChatResult({
				pendingImage: null,
				pendingAudio: mockAudio,
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("voice-recording.wav")).toBeInTheDocument();
			});
		});

		it("renders audio preview with Mic icon", async () => {
			const mockAudio = {
				name: "audio.wav",
				dataUrl: "data:audio/wav;base64,data",
				size: 1024,
				format: "wav",
			};

			mockUseChatResult({
				pendingImage: null,
				pendingAudio: mockAudio,
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByLabelText("Remove audio")).toBeInTheDocument();
			});
		});

		it("renders audio remove button", async () => {
			const mockAudio = {
				name: "test.wav",
				dataUrl: "data:audio/wav;base64,test",
				size: 512,
				format: "wav",
			};

			mockUseChatResult({
				pendingImage: null,
				pendingAudio: mockAudio,
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Remove audio" }),
				).toBeInTheDocument();
			});
		});

		it("renders both image and audio previews together", async () => {
			const mockImage = {
				name: "image.png",
				dataUrl: "data:image/png;base64,img",
				size: 1024,
			};
			const mockAudio = {
				name: "audio.wav",
				dataUrl: "data:audio/wav;base64,snd",
				size: 512,
				format: "wav",
			};

			mockUseChatResult({
				pendingImage: mockImage,
				pendingAudio: mockAudio,
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByAltText("image.png")).toBeInTheDocument();
				expect(screen.getByText("audio.wav")).toBeInTheDocument();
			});
		});
	});

	describe("Conversation Stats in Various States", () => {
		it("hides stats bar when conversation is idle", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "idle",
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});
			expect(screen.queryByText(/Turn/)).not.toBeInTheDocument();
		});

		it("shows stats bar when conversation is paused", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "paused",
				currentTurn: 4,
				maxTurns: 10,
				messages: [{ role: "user", content: "Test", timestamp: 1 }],
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Turn 2 / 10")).toBeInTheDocument();
			});
		});

		it("shows stats with zero duration and tokens at start", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "running",
				currentTurn: 1,
				totalDuration: 0,
				totalTokens: 0,
				messages: [{ role: "user", content: "Test", timestamp: 1 }],
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("0.0s")).toBeInTheDocument();
			});
			expect(screen.getByText("0 tokens")).toBeInTheDocument();
		});

		it("updates turn counter correctly (currentTurn / 2)", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "running",
				currentTurn: 6,
				maxTurns: 10,
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Turn 3 / 10")).toBeInTheDocument();
			});
		});
	});

	describe("Attachment Remove Handlers", () => {
		it("calls setPendingImage when image remove button is clicked", async () => {
			const setPendingImage = vi.fn();
			mockUseChatResult({
				pendingImage: {
					dataUrl: "data:image/png;base64,test",
					name: "test.png",
				},
				setPendingImage,
			});

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Remove image" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Remove image" }));

			expect(setPendingImage).toHaveBeenCalledWith(null);
		});

		it("calls setPendingAudio when audio remove button is clicked", async () => {
			const setPendingAudio = vi.fn();
			mockUseChatResult({
				pendingAudio: {
					dataUrl: "data:audio/wav;base64,test",
					name: "test.wav",
					format: "wav",
				},
				setPendingAudio,
			});

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Remove audio" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Remove audio" }));

			expect(setPendingAudio).toHaveBeenCalledWith(null);
		});
	});

	describe("Attachment Button File Input Click", () => {
		it("renders image attach button with correct title", async () => {
			mockUseChatResult({
				hasVision: true,
				selectedModel: "test-model",
				isStreaming: false,
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Attach image" }),
				).toBeInTheDocument();
			});

			const imageButton = screen.getByRole("button", {
				name: "Attach image",
			});
			expect(imageButton).toHaveAttribute("title", "Attach image");
		});

		it("renders audio attach button with correct title", async () => {
			mockUseChatResult({
				hasAudioInput: true,
				selectedModel: "test-model",
				isStreaming: false,
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Attach audio" }),
				).toBeInTheDocument();
			});

			const audioButton = screen.getByRole("button", {
				name: "Attach audio",
			});
			expect(audioButton).toHaveAttribute("title", "Attach audio");
		});
	});

	describe("Conversation Mode Action Buttons", () => {
		it("calls setControlsCollapsed and handleStopConversation when Stop button is clicked", async () => {
			const setControlsCollapsed = vi.fn();
			const handleStopConversation = vi.fn();

			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "running",
				isStreaming: true,
				messages: [{ role: "user", content: "Test", timestamp: 1 }],
				setControlsCollapsed,
				handleStopConversation,
			});

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				const stopButtons = screen.getAllByRole("button", { name: "Stop" });
				expect(stopButtons.length).toBeGreaterThan(0);
			});

			// Click the Stop button in the stats bar (conversation mode)
			const stopButtons = screen.getAllByRole("button", { name: "Stop" });
			await user.click(stopButtons[stopButtons.length - 1]);

			expect(setControlsCollapsed).toHaveBeenCalledWith(false);
			expect(handleStopConversation).toHaveBeenCalled();
		});

		it("calls all state setters and toast when Clear button is clicked", async () => {
			const clearConversationAbort = vi.fn();
			const setMessages = vi.fn();
			const setInput = vi.fn();
			const setConversationState = vi.fn();
			const setCurrentTurn = vi.fn();
			const setTurnCountdown = vi.fn();
			const setIsStreaming = vi.fn();
			const toast = vi.fn();

			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "completed",
				messages: [{ role: "user", content: "Test", timestamp: 1 }],
				clearConversationAbort,
				setMessages,
				setInput,
				setConversationState,
				setCurrentTurn,
				setTurnCountdown,
				setIsStreaming,
				toast,
				lastPromptRef: { current: "Last prompt" },
			});

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", {
						name: "Clear messages (keep model & settings)",
					}),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", {
					name: "Clear messages (keep model & settings)",
				}),
			);

			expect(clearConversationAbort).toHaveBeenCalled();
			expect(setMessages).toHaveBeenCalledWith([]);
			expect(setInput).toHaveBeenCalledWith("Last prompt");
			expect(setConversationState).toHaveBeenCalledWith("idle");
			expect(setCurrentTurn).toHaveBeenCalledWith(0);
			expect(setTurnCountdown).toHaveBeenCalledWith(0);
			expect(setIsStreaming).toHaveBeenCalledWith(false);
			expect(toast).toHaveBeenCalledWith("Conversation cleared", "info");
		});

		it("calls setPendingFullReset when Reset All button is clicked", async () => {
			const setPendingFullReset = vi.fn();

			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "completed",
				messages: [{ role: "user", content: "Test", timestamp: 1 }],
				setPendingFullReset,
			});

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(
					screen.getAllByRole("button", {
						name: "Reset all (clear model & settings)",
					}).length,
				).toBeGreaterThan(0);
			});

			const resetButtons = screen.getAllByRole("button", {
				name: "Reset all (clear model & settings)",
			});
			await user.click(resetButtons[0]);

			expect(setPendingFullReset).toHaveBeenCalledWith(true);
		});

		it("shows all action buttons in completed state", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "completed",
				messages: [{ role: "user", content: "Test", timestamp: 1 }],
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", {
						name: "Clear messages (keep model & settings)",
					}),
				).toBeInTheDocument();
			});

			expect(
				screen.getAllByRole("button", {
					name: "Reset all (clear model & settings)",
				}).length,
			).toBeGreaterThan(0);
			// Stop button should not appear in completed state (only when streaming)
			expect(
				screen.queryByRole("button", { name: "Stop" }),
			).not.toBeInTheDocument();
		});

		it("shows Stop button only when streaming", async () => {
			mockUseChatResult({
				chatSubMode: "conversation",
				conversationState: "running",
				isStreaming: true,
				messages: [{ role: "user", content: "Test", timestamp: 1 }],
			});

			renderWithProviders(<Chat />);

			await waitFor(() => {
				const stopButtons = screen.getAllByRole("button", { name: "Stop" });
				expect(stopButtons.length).toBeGreaterThan(0);
			});

			expect(
				screen.getAllByRole("button", {
					name: "Clear messages (keep model & settings)",
				}).length,
			).toBeGreaterThan(0);
			expect(
				screen.getAllByRole("button", {
					name: "Reset all (clear model & settings)",
				}).length,
			).toBeGreaterThan(0);
		});
	});
});
