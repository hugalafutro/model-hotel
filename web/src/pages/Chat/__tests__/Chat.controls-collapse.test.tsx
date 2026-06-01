import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockAllDefaults, mockChatStream } from "../../../test/helpers";
import { mockModel } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Chat } from "../../Chat";

describe("Chat controls collapse/expand", () => {
	beforeEach(() => {
		server.use(...mockAllDefaults());
	});

	const getControlsSection = () => {
		// The Controls section is a ui-card containing the "Controls" label
		const controlsSection = screen.getByText("Controls").closest(".ui-card");
		if (!controlsSection) {
			throw new Error("Controls section not found");
		}
		return controlsSection as HTMLElement;
	};

	const getControlsGrid = () => {
		// The collapsible grid is inside the Controls ui-card
		// It has grid-rows-[0fr] when collapsed, grid-rows-[1fr] when expanded
		const controlsSection = getControlsSection();
		const grid = controlsSection.querySelector(
			".grid.transition-\\[grid-template-rows\\]",
		);
		if (!grid) {
			throw new Error("Controls grid not found");
		}
		return grid as HTMLElement;
	};

	const isControlsExpanded = () => {
		const grid = getControlsGrid();
		return grid.classList.contains("grid-rows-[1fr]");
	};

	const isControlsCollapsed = () => {
		const grid = getControlsGrid();
		return grid.classList.contains("grid-rows-[0fr]");
	};

	const getControlsCollapseToggle = () => {
		// Get the collapse toggle specifically from the Controls section
		const controlsSection = getControlsSection();
		const toggle = controlsSection.querySelector(
			'button[aria-label="Collapse"], button[aria-label="Expand"]',
		);
		if (!toggle) {
			throw new Error("Controls collapse toggle not found");
		}
		return toggle as HTMLElement;
	};

	const getControlsStopButton = () => {
		// Get the Stop button specifically from the Controls header (ActionIconButton with CircleStop)
		const controlsSection = getControlsSection();
		const stopButton = controlsSection.querySelector(
			'button[aria-label="Stop"]',
		);
		if (!stopButton) {
			throw new Error("Controls Stop button not found");
		}
		return stopButton as HTMLElement;
	};

	const selectModel = async (
		user: ReturnType<typeof renderWithProviders>["user"],
		displayName: string,
	) => {
		// Open the model picker by clicking the filter input
		const filterInput = screen.getByPlaceholderText("Filter models…");
		await user.click(filterInput);

		// Click the model option
		const modelOption = screen.getByText(displayName);
		await user.click(modelOption);
	};

	describe("Chat mode", () => {
		beforeEach(() => {
			localStorage.clear();
		});

		it("controls start expanded", async () => {
			renderWithProviders(<Chat />);

			// Wait for models to load
			await waitFor(() => {
				expect(
					screen.getByPlaceholderText("Filter models…"),
				).toBeInTheDocument();
			});

			expect(isControlsExpanded()).toBe(true);
		});

		it("pressing Send (not streaming) collapses controls", async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Hello" }, index: 0 }] },
				{ choices: [{ delta: { content: " world" }, index: 0 }] },
			];
			server.use(...mockChatStream(chunks));

			const { user } = renderWithProviders(<Chat />);

			// Wait for models to load
			await waitFor(() => {
				expect(
					screen.getByPlaceholderText("Filter models…"),
				).toBeInTheDocument();
			});

			// Select a model
			await selectModel(user, mockModel.display_name);

			// Type a message
			const input = screen.getByLabelText("Chat message input");
			await user.type(input, "Hello");

			// Verify controls are expanded initially
			expect(isControlsExpanded()).toBe(true);

			// Click Send button
			const sendButton = screen.getByRole("button", { name: "Send" });
			await user.click(sendButton);

			// Controls should collapse after sending
			await waitFor(() => {
				expect(isControlsCollapsed()).toBe(true);
			});
		});

		it("pressing Stop (during streaming) expands controls", async () => {
			// Use a slow stream so we can click stop before it finishes
			const chunks = Array.from({ length: 20 }, (_, i) => ({
				choices: [{ delta: { content: `chunk${i} ` }, index: 0 }],
			}));
			server.use(...mockChatStream(chunks, { delay: 100 }));

			const { user } = renderWithProviders(<Chat />);

			// Wait for models to load
			await waitFor(() => {
				expect(
					screen.getByPlaceholderText("Filter models…"),
				).toBeInTheDocument();
			});

			// Select a model
			await selectModel(user, mockModel.display_name);

			// Type a message
			const input = screen.getByLabelText("Chat message input");
			await user.type(input, "Hello");

			// Click Send button
			const sendButton = screen.getByRole("button", { name: "Send" });
			await user.click(sendButton);

			// Wait for streaming to start and controls to collapse
			await waitFor(
				() => {
					expect(isControlsCollapsed()).toBe(true);
				},
				{ timeout: 5000 },
			);

			// Click Stop button from Controls header (not the input bar button)
			const stopButton = getControlsStopButton();
			await user.click(stopButton);

			// Controls should expand after stopping
			await waitFor(
				() => {
					expect(isControlsExpanded()).toBe(true);
				},
				{ timeout: 5000 },
			);
		});

		it("pressing Enter key while not streaming collapses controls", async () => {
			const chunks = [{ choices: [{ delta: { content: "Hello" }, index: 0 }] }];
			server.use(...mockChatStream(chunks));

			const { user } = renderWithProviders(<Chat />);

			// Wait for models to load
			await waitFor(() => {
				expect(
					screen.getByPlaceholderText("Filter models…"),
				).toBeInTheDocument();
			});

			// Select a model
			await selectModel(user, mockModel.display_name);

			// Type a message
			const input = screen.getByLabelText("Chat message input");
			await user.type(input, "Hello");

			// Verify controls are expanded initially
			expect(isControlsExpanded()).toBe(true);

			// Press Enter key
			await user.keyboard("{Enter}");

			// Controls should collapse
			await waitFor(() => {
				expect(isControlsCollapsed()).toBe(true);
			});
		});

		it("pressing Enter key during streaming expands controls", async () => {
			// Use a slow stream so we can press Enter
			const chunks = [
				{ choices: [{ delta: { content: "Hello" }, index: 0 }] },
				{ choices: [{ delta: { content: " world" }, index: 0 }] },
			];
			server.use(...mockChatStream(chunks, { delay: 50 }));

			const { user } = renderWithProviders(<Chat />);

			// Wait for models to load
			await waitFor(() => {
				expect(
					screen.getByPlaceholderText("Filter models…"),
				).toBeInTheDocument();
			});

			// Select a model
			await selectModel(user, mockModel.display_name);

			// Type a message and send
			const input = screen.getByLabelText("Chat message input");
			await user.type(input, "Hello");
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for streaming to start and controls to collapse
			await waitFor(() => {
				expect(isControlsCollapsed()).toBe(true);
			});

			// Press Enter key during streaming (should stop and expand)
			await user.keyboard("{Enter}");

			// Controls should expand
			await waitFor(() => {
				expect(isControlsExpanded()).toBe(true);
			});
		});

		it("clicking the CollapsibleToggle still works", async () => {
			const { user } = renderWithProviders(<Chat />);

			// Wait for models to load
			await waitFor(() => {
				expect(
					screen.getByPlaceholderText("Filter models…"),
				).toBeInTheDocument();
			});

			// Controls should start expanded
			expect(isControlsExpanded()).toBe(true);

			// Click the collapse toggle from Controls section
			const collapseToggle = getControlsCollapseToggle();
			expect(collapseToggle).toHaveAttribute("aria-label", "Collapse");
			await user.click(collapseToggle);

			// Controls should collapse
			await waitFor(() => {
				expect(isControlsCollapsed()).toBe(true);
			});

			// Click the expand toggle
			const expandToggle = getControlsCollapseToggle();
			expect(expandToggle).toHaveAttribute("aria-label", "Expand");
			await user.click(expandToggle);

			// Controls should expand
			await waitFor(() => {
				expect(isControlsExpanded()).toBe(true);
			});
		});

		it("soft reset (Eraser) does NOT expand controls", async () => {
			const { user } = renderWithProviders(<Chat />);

			// Wait for models to load
			await waitFor(() => {
				expect(
					screen.getByPlaceholderText("Filter models…"),
				).toBeInTheDocument();
			});

			// Select a model
			await selectModel(user, mockModel.display_name);

			// Type and send a message to create chat history
			const chunks = [
				{ choices: [{ delta: { content: "Hi there" }, index: 0 }] },
			];
			server.use(...mockChatStream(chunks));

			const input = screen.getByLabelText("Chat message input");
			await user.type(input, "Hello");
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for assistant response to appear
			await waitFor(() => {
				expect(screen.getByText("Hi there")).toBeInTheDocument();
			});

			// Controls should be collapsed after sending
			await waitFor(() => {
				expect(isControlsCollapsed()).toBe(true);
			});

			// Click soft reset (Eraser) - use aria-label to find it
			const softResetButton = screen.getByRole("button", {
				name: "Clear messages (keep model & settings)",
			});
			await user.click(softResetButton);

			// Controls should stay collapsed
			await waitFor(
				() => {
					expect(isControlsCollapsed()).toBe(true);
				},
				{ timeout: 3000 },
			);
		});

		it("model error does NOT expand controls", async () => {
			// Mock an error response
			server.use(
				http.post("/api/chat/chat", () => {
					return HttpResponse.json(
						{ error: "Model unavailable" },
						{ status: 500 },
					);
				}),
			);

			const { user } = renderWithProviders(<Chat />);

			// Wait for models to load
			await waitFor(() => {
				expect(
					screen.getByPlaceholderText("Filter models…"),
				).toBeInTheDocument();
			});

			// Select a model
			await selectModel(user, mockModel.display_name);

			// Type and send a message
			const input = screen.getByLabelText("Chat message input");
			await user.type(input, "Hello");
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for controls to collapse after sending
			await waitFor(() => {
				expect(isControlsCollapsed()).toBe(true);
			});

			await waitFor(() => {
				expect(screen.getByText(/try Regenerate/i)).toBeInTheDocument();
			});

			// Controls should stay collapsed (error state doesn't auto-expand)
			expect(isControlsCollapsed()).toBe(true);
		});
	});

	describe("Conversation mode", () => {
		const mockModelB = {
			...mockModel,
			id: "model-002",
			model_id: "model-b-v1",
			name: "Test Model B",
			display_name: "Test Model v2",
			provider_id: "provider-002",
			provider_name: "Other Provider",
		};

		beforeEach(() => {
			localStorage.clear();
		});

		const selectModelA = async (
			user: ReturnType<typeof renderWithProviders>["user"],
			displayName: string,
		) => {
			// Find the Model A section by its label
			const modelASection = screen
				.getByLabelText("Model A")
				.closest(".space-y-3") as HTMLElement;
			if (!modelASection) {
				throw new Error("Model A section not found");
			}

			// Click the model button within Model A's container
			const modelButton = within(modelASection).getByRole("button", {
				name: displayName,
			});
			await user.click(modelButton);
		};

		const selectModelB = async (
			user: ReturnType<typeof renderWithProviders>["user"],
			displayName: string,
		) => {
			// Find the Model B section by its label
			const modelBSection = screen
				.getByLabelText("Model B")
				.closest(".space-y-3") as HTMLElement;
			if (!modelBSection) {
				throw new Error("Model B section not found");
			}

			// Click the model button within Model B's container
			const modelButton = within(modelBSection).getByRole("button", {
				name: displayName,
			});
			await user.click(modelButton);
		};

		it("Pressing Start in conversation mode collapses controls", async () => {
			// Setup two different models
			server.use(...mockAllDefaults({ models: [mockModel, mockModelB] }));

			const chunks = [
				{ choices: [{ delta: { content: "Hello from A" }, index: 0 }] },
				{ choices: [{ delta: { content: "Hello from B" }, index: 0 }] },
			];
			server.use(...mockChatStream(chunks, { delay: 10 }));

			const { user } = renderWithProviders(<Chat />);

			// Wait for models to load
			await waitFor(() => {
				expect(
					screen.getAllByPlaceholderText("Filter models…")[0],
				).toBeInTheDocument();
			});

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			// Select Model A
			await selectModelA(user, mockModel.display_name);

			// Select Model B
			await selectModelB(user, mockModelB.display_name);

			// Type a prompt
			const promptInput = screen.getByLabelText("Prompt");
			await user.type(promptInput, "Compare these models");

			// Verify controls are expanded initially
			expect(isControlsExpanded()).toBe(true);

			// Click Start
			await user.click(screen.getByRole("button", { name: "Start" }));

			// Controls should collapse after starting
			await waitFor(
				() => {
					expect(isControlsCollapsed()).toBe(true);
				},
				{ timeout: 5000 },
			);
		});

		it("Pressing Stop (ConversationConfig) in conversation mode expands controls", async () => {
			// Setup two different models
			server.use(...mockAllDefaults({ models: [mockModel, mockModelB] }));

			const chunks = [
				{ choices: [{ delta: { content: "Hello from A" }, index: 0 }] },
				{ choices: [{ delta: { content: "Hello from B" }, index: 0 }] },
			];
			server.use(...mockChatStream(chunks, { delay: 10 }));

			const { user } = renderWithProviders(<Chat />);

			// Wait for models to load
			await waitFor(() => {
				expect(
					screen.getAllByPlaceholderText("Filter models…")[0],
				).toBeInTheDocument();
			});

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			// Select Model A
			await selectModelA(user, mockModel.display_name);

			// Select Model B
			await selectModelB(user, mockModelB.display_name);

			// Type a prompt
			const promptInput = screen.getByLabelText("Prompt");
			await user.type(promptInput, "Compare these models");

			// Click Start
			await user.click(screen.getByRole("button", { name: "Start" }));

			// Wait for controls to collapse
			await waitFor(
				() => {
					expect(isControlsCollapsed()).toBe(true);
				},
				{ timeout: 5000 },
			);

			// Find ConversationConfig section and click Stop within it
			const conversationConfig = screen
				.getByText("Conversation Config")
				.closest(".ui-card");
			if (!conversationConfig) {
				throw new Error("ConversationConfig not found");
			}
			const stopButton = within(conversationConfig as HTMLElement).getByRole(
				"button",
				{
					name: "Stop",
				},
			);
			await user.click(stopButton);

			// Controls should expand after stopping
			await waitFor(
				() => {
					expect(isControlsExpanded()).toBe(true);
				},
				{ timeout: 5000 },
			);
		});

		it("Pressing Reset All expands controls", async () => {
			// Setup two different models
			server.use(...mockAllDefaults({ models: [mockModel, mockModelB] }));

			const { user } = renderWithProviders(<Chat />);

			// Wait for models to load
			await waitFor(() => {
				expect(
					screen.getAllByPlaceholderText("Filter models…")[0],
				).toBeInTheDocument();
			});

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			// Select Model A
			await selectModelA(user, mockModel.display_name);

			// Select Model B
			await selectModelB(user, mockModelB.display_name);

			// Manually collapse controls first
			const collapseToggle = getControlsCollapseToggle();
			await user.click(collapseToggle);

			// Verify controls are collapsed
			await waitFor(() => {
				expect(isControlsCollapsed()).toBe(true);
			});

			// Click Reset All in the conversation stats bar
			await user.click(
				screen.getByRole("button", {
					name: "Reset all (clear model & settings)",
				}),
			);

			// Confirm in dialog
			await user.click(screen.getByRole("button", { name: "Reset All" }));

			// Controls should expand after reset
			await waitFor(() => {
				expect(isControlsExpanded()).toBe(true);
			});
		});

		it("Soft reset (Eraser/Clear) does NOT expand controls in conversation mode", async () => {
			// Setup two different models
			server.use(...mockAllDefaults({ models: [mockModel, mockModelB] }));

			const chunks = [
				{ choices: [{ delta: { content: "Response from A" }, index: 0 }] },
				{ choices: [{ delta: { content: "Response from B" }, index: 0 }] },
			];
			server.use(...mockChatStream(chunks, { delay: 10 }));

			const { user } = renderWithProviders(<Chat />);

			// Wait for models to load
			await waitFor(() => {
				expect(
					screen.getAllByPlaceholderText("Filter models…")[0],
				).toBeInTheDocument();
			});

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			// Select Model A
			await selectModelA(user, mockModel.display_name);

			// Select Model B
			await selectModelB(user, mockModelB.display_name);

			// Type a prompt
			const promptInput = screen.getByLabelText("Prompt");
			await user.type(promptInput, "Compare these models");

			// Click Start
			await user.click(screen.getByRole("button", { name: "Start" }));

			// Wait for controls to collapse and response to appear
			await waitFor(
				() => {
					expect(isControlsCollapsed()).toBe(true);
				},
				{ timeout: 5000 },
			);

			// Wait for some response content
			await waitFor(
				() => {
					expect(screen.getByText("Response from A")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);

			// Click soft reset (Eraser) in the conversation stats bar
			const softResetButton = screen.getByRole("button", {
				name: "Clear messages (keep model & settings)",
			});
			await user.click(softResetButton);

			// Controls should stay collapsed
			await waitFor(
				() => {
					expect(isControlsCollapsed()).toBe(true);
				},
				{ timeout: 3000 },
			);
		});
	});
});
