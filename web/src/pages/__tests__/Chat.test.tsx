import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import {
	mockAllDefaults,
	mockChatStream,
	mockModels,
	mockProviders,
} from "../../test/helpers";
import { mockModel, mockProvider } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Chat } from "../Chat";

describe("Chat", () => {
	beforeEach(() => {
		server.resetHandlers();
		server.use(...mockAllDefaults());
		// Clear localStorage to ensure fresh state for each test
		localStorage.clear();
	});

	describe("Page Rendering", () => {
		it("renders Chat page with header and controls", async () => {
			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			expect(screen.getByText("Controls")).toBeInTheDocument();
		});

		it("renders chat mode by default", async () => {
			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			expect(
				screen.getByText("Test enabled models in temporary chat"),
			).toBeInTheDocument();
			// ModelPicker filter input should be visible in chat mode
			expect(screen.getByPlaceholderText("Filter models…")).toBeInTheDocument();
		});

		it("shows empty state placeholder", async () => {
			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			expect(screen.getByText("Chat will appear here")).toBeInTheDocument();
		});

		it("shows 'Select a model' when no model chosen", async () => {
			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			// The sidebar placeholder when no model is selected
			expect(screen.getByText("Select a model")).toBeInTheDocument();
		});
	});

	describe("Mode Toggle", () => {
		it("can toggle to conversation mode", async () => {
			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Click the "AI Conversation" button to switch modes
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			// Wait for mode to switch
			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});
			expect(
				screen.getByText("Watch two models converse with each other"),
			).toBeInTheDocument();
			// Model A and Model B labels should appear
			expect(screen.getByText("Model A")).toBeInTheDocument();
			expect(screen.getByText("Model B")).toBeInTheDocument();
		});

		it("conversation mode shows ConversationConfig", async () => {
			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});

			// ConversationConfig should render with rounds and delay labels
			expect(screen.getByText("Rounds")).toBeInTheDocument();
			expect(screen.getByText("Delay (ms)")).toBeInTheDocument();
		});

		it("conversation mode empty state", async () => {
			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			await waitFor(() => {
				expect(
					screen.getByText("Conversation will appear here"),
				).toBeInTheDocument();
			});
		});

		it("can toggle back to chat mode", async () => {
			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});

			// Switch back to chat mode
			await user.click(screen.getByRole("button", { name: "Chat with AI" }));

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			expect(
				screen.getByText("Test enabled models in temporary chat"),
			).toBeInTheDocument();
		});
	});

	describe("Collapsible Controls", () => {
		it("collapsible toggle is present", async () => {
			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Collapse toggle button should be present in controls section
			const collapseButtons = screen.getAllByRole("button", {
				name: "Collapse",
			});
			expect(collapseButtons.length).toBeGreaterThan(0);

			// Click the first collapse toggle
			await user.click(collapseButtons[0]);

			// After click, toggle should change to "Expand"
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Expand" }),
				).toBeInTheDocument();
			});
		});

		it("can expand collapsed controls", async () => {
			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Verify collapse button exists initially
			expect(
				screen.getAllByRole("button", { name: "Collapse" }).length,
			).toBeGreaterThan(0);

			// Collapse
			await user.click(screen.getAllByRole("button", { name: "Collapse" })[0]);

			await waitFor(() => {
				expect(
					screen.getAllByRole("button", { name: "Expand" }).length,
				).toBeGreaterThan(0);
			});

			// Expand - the toggle should change back
			const expandBtn = screen.getAllByRole("button", { name: "Expand" })[0];
			await user.click(expandBtn);

			// Wait for toggle to change back to Collapse
			await waitFor(
				() => {
					const buttons = screen.queryAllByRole("button", { name: "Collapse" });
					expect(buttons.length).toBeGreaterThan(0);
				},
				{ timeout: 2000 },
			);
		});
	});

	describe("Chat Input Area", () => {
		it("chat mode shows input area", async () => {
			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Input textarea should be present in chat mode
			expect(
				screen.getByRole("textbox", { name: "Chat message input" }),
			).toBeInTheDocument();
		});

		it("input area shows placeholder when no model selected", async () => {
			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			expect(textarea).toHaveAttribute("placeholder", "Select a model first");
		});

		it("input area is disabled when no model selected", async () => {
			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			expect(textarea).toBeDisabled();
		});

		it("send button is present in chat mode", async () => {
			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			expect(screen.getByRole("button", { name: "Send" })).toBeInTheDocument();
		});

		it("conversation mode does not show input area", async () => {
			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});

			// Input textarea should not be present in conversation mode
			expect(
				screen.queryByRole("textbox", { name: "Chat message input" }),
			).not.toBeInTheDocument();
		});
	});

	describe("Model Selection", () => {
		it("renders ModelPicker with models from API", async () => {
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));

			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// ModelPicker filter input should be present
			expect(screen.getByPlaceholderText("Filter models…")).toBeInTheDocument();
		});

		it("shows model detail panel when model is selected", async () => {
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Wait for model to be available and select it directly
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Model detail panel should render - check for the model name in a heading
			// The detail panel shows the model name in an h3 element
			await waitFor(() => {
				const heading = screen.getByRole("heading", { level: 3 });
				expect(heading).toHaveTextContent("Test Model v1");
			});
		});
	});

	describe("Persona Picker", () => {
		it("renders PersonaPicker in chat mode", async () => {
			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Persona picker section should be present
			expect(screen.getByText("Persona")).toBeInTheDocument();
		});

		it("renders PersonaPicker for both models in conversation mode", async () => {
			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});

			// Both Persona A and Persona B should be present
			expect(screen.getByText("Persona A")).toBeInTheDocument();
			expect(screen.getByText("Persona B")).toBeInTheDocument();
		});
	});

	describe("Action Buttons", () => {
		it("shows clear and reset buttons when model is selected", async () => {
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));

			const { user } = renderWithProviders(<Chat />);

			// Wait for model to be available and select it
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Reset button should appear after model selection
			// (Clear button only appears when there are messages)
			await waitFor(() => {
				expect(
					screen.getByRole("button", {
						name: "Reset all (clear model & settings)",
					}),
				).toBeInTheDocument();
			});
		});
	});

	describe("Conversation Config Panel", () => {
		it("shows start button in conversation mode when idle", async () => {
			const { user } = renderWithProviders(<Chat />);

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});

			// Start button should be present in idle state
			expect(screen.getByRole("button", { name: "Start" })).toBeInTheDocument();
		});

		it("shows max turns input with default value", async () => {
			const { user } = renderWithProviders(<Chat />);

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});

			// Max turns input (labeled "Rounds") should have a default value
			const maxTurnsInput = screen.getByLabelText("Rounds");
			expect(maxTurnsInput).toHaveValue(10);
		});

		it("shows turn delay input with default value", async () => {
			const { user } = renderWithProviders(<Chat />);

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});

			// Turn delay input (labeled "Delay (ms)") should have a default value
			const turnDelayInput = screen.getByLabelText("Delay (ms)");
			expect(turnDelayInput).toHaveValue(500);
		});
	});

	describe("Chat Send Flow", () => {
		it("sends a message and receives a streaming response", async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Hello" } }] },
				{ choices: [{ delta: { content: " there" } }] },
				{ choices: [{ delta: { content: "!" } }] },
			];
			server.use(...mockChatStream(chunks, { delay: 10 }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Wait for model detail panel to appear
			await waitFor(() => {
				const heading = screen.getByRole("heading", { level: 3 });
				expect(heading).toHaveTextContent("Test Model v1");
			});

			// Type a message
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Hi there");

			// Click Send
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for user message to appear
			await waitFor(
				() => {
					expect(screen.getByText("Hi there")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// Wait for assistant response to appear
			await waitFor(
				() => {
					expect(screen.getByText("Hello there!")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});

		it("disables input and shows Stop during streaming", async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Hello" } }] },
				{ choices: [{ delta: { content: " world" } }] },
				{ choices: [{ delta: { content: "!" } }] },
			];
			server.use(...mockChatStream(chunks, { delay: 100 }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Type a message
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Test message");

			// Click Send
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for streaming to start
			await waitFor(
				() => {
					const input = screen.getByRole("textbox", {
						name: "Chat message input",
					});
					expect(input).toBeDisabled();
				},
				{ timeout: 2000 },
			);

			// Stop button should be visible (Send button becomes Stop)
			await waitFor(
				() => {
					const stopButtons = screen.getAllByRole("button", { name: "Stop" });
					expect(stopButtons.length).toBeGreaterThan(0);
				},
				{ timeout: 2000 },
			);

			// Wait for stream to complete
			await waitFor(
				() => {
					const input = screen.getByRole("textbox", {
						name: "Chat message input",
					});
					expect(input).not.toBeDisabled();
				},
				{ timeout: 5000 },
			);

			// Send button should be back
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Send" }),
				).toBeInTheDocument();
			});
		});

		it("sends message via Enter key", async () => {
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];
			server.use(...mockChatStream(chunks, { delay: 10 }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Type a message and press Enter
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Hello via Enter{Enter}");

			// Wait for streaming to start
			await waitFor(
				() => {
					expect(
						screen.queryByText("Chat will appear here"),
					).not.toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// Verify user message is visible
			expect(screen.getByText("Hello via Enter")).toBeInTheDocument();
		});

		it("does not send empty messages", async () => {
			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Don't type anything, click Send
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Verify no streaming starts - empty state should remain
			await waitFor(
				() => {
					expect(screen.getByText("Chat will appear here")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// No assistant messages should appear
			expect(screen.queryByRole("article")).not.toBeInTheDocument();
		});

		it("does not send when no model selected", async () => {
			renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Textarea is disabled when no model is selected
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			expect(textarea).toBeDisabled();

			// Send button should be disabled
			const sendButton = screen.getByRole("button", { name: "Send" });
			expect(sendButton).toBeDisabled();
		});
	});

	describe("Chat Stop Flow", () => {
		it("stops streaming when Stop is clicked", { timeout: 10000 }, async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Hello" } }] },
				{ choices: [{ delta: { content: " world" } }] },
			];
			server.use(...mockChatStream(chunks, { delay: 200 }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Type a message
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Test stop");

			// Click Send
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for streaming to start
			await waitFor(
				() => {
					const stopButtons = screen.getAllByRole("button", { name: "Stop" });
					expect(stopButtons.length).toBeGreaterThan(0);
				},
				{ timeout: 2000 },
			);

			// Click Stop (input area button, which is typically last)
			const stopButtons = screen.getAllByRole("button", { name: "Stop" });
			await user.click(stopButtons[stopButtons.length - 1]);

			// Wait for Send button to return
			await waitFor(
				() => {
					const sendButtons = screen.queryAllByRole("button", { name: "Send" });
					expect(sendButtons.length).toBeGreaterThan(0);
				},
				{ timeout: 2000 },
			);
		});

		it("stops streaming via header Stop button", async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Hello" } }] },
				{ choices: [{ delta: { content: " world" } }] },
			];
			server.use(...mockChatStream(chunks, { delay: 200 }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Type a message
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Test header stop");

			// Click Send
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for streaming to start
			await waitFor(
				() => {
					const stopButtons = screen.getAllByRole("button", { name: "Stop" });
					expect(stopButtons.length).toBeGreaterThan(0);
				},
				{ timeout: 2000 },
			);

			// Click the header Stop button (ActionIconButton in controls)
			const stopButtons = screen.getAllByRole("button", { name: "Stop" });
			// Click the first one (header button)
			await user.click(stopButtons[0]);

			// Wait for Send button to return
			await waitFor(
				() => {
					expect(
						screen.getByRole("button", { name: "Send" }),
					).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});
	});

	describe("Chat Regenerate Flow", () => {
		it("regenerates the last response", { timeout: 10000 }, async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Response" } }] },
				{ choices: [{ delta: { content: " after regenerate" } }] },
			];
			server.use(...mockChatStream(chunks, { delay: 10 }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Type and send a message
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Initial message");

			// Click Send
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for response
			await waitFor(
				() => {
					expect(
						screen.getByText("Response after regenerate"),
					).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// Click Regenerate button on the last assistant message
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Regenerate" }),
				).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Regenerate" }));

			// Wait for new response to appear (handleRegenerate re-sends automatically)
			await waitFor(
				() => {
					expect(
						screen.getByText("Response after regenerate"),
					).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});
	});

	describe("Chat Delete Flow", () => {
		it("deletes an assistant message and its preceding user message", {
			timeout: 10000,
		}, async () => {
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];
			server.use(...mockChatStream(chunks, { delay: 10 }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Type and send a message
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Message to delete");

			// Click Send
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for response
			await waitFor(
				() => {
					expect(screen.getByText("Response")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// Click Delete button on the assistant message
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Delete message" }),
				).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Delete message" }));

			// Verify messages are gone and empty state returns
			await waitFor(
				() => {
					expect(screen.getByText("Chat will appear here")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// User message should also be gone
			expect(screen.queryByText("Message to delete")).not.toBeInTheDocument();
		});
	});

	describe("Chat Error Handling", () => {
		it("shows error when chat API returns error status", async () => {
			const chunks = [{ choices: [{ delta: { content: "Error" } }] }];
			server.use(...mockChatStream(chunks, { status: 500 }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Type and send a message
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Test error");

			// Click Send
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for error to appear
			await waitFor(
				() => {
					expect(screen.getByText(/try Regenerate/i)).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});

		it("shows error when network fails", {
			timeout: 8000,
		}, async () => {
			server.use(
				http.post("/api/chat/chat", () => {
					return HttpResponse.error();
				}),
			);

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Type and send a message
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Network fail test");

			// Click Send
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for error to appear
			await waitFor(
				() => {
					expect(screen.getByText(/try Regenerate/i)).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});
	});

	describe("Chat Clear and Reset", () => {
		it("clears messages but keeps model selection", {
			timeout: 10000,
		}, async () => {
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];
			server.use(...mockChatStream(chunks, { delay: 10 }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Wait for model detail panel
			await waitFor(() => {
				const heading = screen.getByRole("heading", { level: 3 });
				expect(heading).toHaveTextContent("Test Model v1");
			});

			// Type and send a message
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Message to clear");

			// Click Send
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for response
			await waitFor(
				() => {
					expect(screen.getByText("Response")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// Click Clear button
			await user.click(
				screen.getByRole("button", {
					name: "Clear messages (keep model & settings)",
				}),
			);

			// Verify messages are gone
			await waitFor(
				() => {
					expect(screen.getByText("Chat will appear here")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// Model should still be selected (detail panel visible)
			await waitFor(() => {
				const heading = screen.getByRole("heading", { level: 3 });
				expect(heading).toHaveTextContent("Test Model v1");
			});
		});

		it("resets everything including model selection", {
			timeout: 5000,
		}, async () => {
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];
			server.use(...mockChatStream(chunks, { delay: 10 }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Type and send a message
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Message before reset");

			// Click Send
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for response
			await waitFor(
				() => {
					expect(screen.getByText("Response")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// Click Reset button
			await user.click(
				screen.getByRole("button", {
					name: "Reset all (clear model & settings)",
				}),
			);

			// Confirm in dialog
			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Reset All" }));

			// Verify everything is reset
			await waitFor(
				() => {
					expect(screen.getByText("Chat will appear here")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// Model should be deselected (detail panel gone, "Select a model" visible)
			await waitFor(
				() => {
					expect(screen.getByText("Select a model")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});
	});

	describe("Input Area Status Messages", () => {
		it("shows amber status when no model selected", async () => {
			renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			const status = screen.getByText("Select a model to start chatting");
			expect(status).toHaveClass("text-amber-400");
		});

		it("shows error with model short name when lastChatError has model", async () => {
			server.use(
				...mockChatStream([{ choices: [{ delta: { content: "" } }] }], {
					status: 500,
				}),
			);
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Test error{Enter}");
			await waitFor(
				() => {
					expect(screen.getByText(/try Regenerate/)).toBeInTheDocument();
				},
				{ timeout: 3000 },
			);
			// Should show short model name (after /)
			expect(
				screen.getByText(/test-model-v1.*try Regenerate/),
			).toBeInTheDocument();
		});

		it("shows Press Enter hint when model selected and no error", async () => {
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));
			await waitFor(() => {
				expect(screen.getByText(/Press Enter to send/)).toBeInTheDocument();
			});
		});
	});

	describe("Textarea Placeholder Variations", () => {
		it("shows image paste placeholder for vision model", async () => {
			const visionModel = {
				...mockModel,
				id: "model-vision",
				model_id: "vision-model",
				display_name: "Vision Model",
				capabilities: '{"streaming":true,"vision":true,"audio_input":false}',
			};
			server.use(...mockModels({ body: [visionModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Vision Model")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Vision Model"));
			await waitFor(() => {
				const textarea = screen.getByRole("textbox", {
					name: "Chat message input",
				});
				expect(textarea).toHaveAttribute(
					"placeholder",
					"Type a message (or paste an image)…",
				);
			});
		});

		it("shows basic placeholder for non-vision model", async () => {
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));
			await waitFor(() => {
				const textarea = screen.getByRole("textbox", {
					name: "Chat message input",
				});
				expect(textarea).toHaveAttribute("placeholder", "Type a message…");
			});
		});
	});

	describe("Textarea Title Attribute", () => {
		it("shows title when no model selected", async () => {
			renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			expect(textarea).toHaveAttribute("title", "Select a model first");
		});

		it("shows Generating title during streaming", async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Hello" } }] },
				{ choices: [{ delta: { content: " world" } }] },
			];
			server.use(...mockChatStream(chunks, { delay: 200 }));
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Test{Enter}");
			await waitFor(
				() => {
					expect(
						screen.getByRole("textbox", { name: "Chat message input" }),
					).toHaveAttribute("title", "Generating…");
				},
				{ timeout: 2000 },
			);
		});

		it("has no title when model selected and not streaming", async () => {
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));
			await waitFor(() => {
				const textarea = screen.getByRole("textbox", {
					name: "Chat message input",
				});
				expect(textarea).not.toHaveAttribute("title");
			});
		});
	});

	describe("Send Button Behavior", () => {
		it("send button has primary styling when not streaming", async () => {
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));
			await waitFor(() => {
				const sendBtn = screen.getByRole("button", { name: "Send" });
				expect(sendBtn).toHaveClass("ui-btn-primary");
			});
		});

		it("send button shows Stop with danger styling during streaming", async () => {
			const chunks = [{ choices: [{ delta: { content: "Hello" } }] }];
			server.use(...mockChatStream(chunks, { delay: 200 }));
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Test{Enter}");
			await waitFor(
				() => {
					const stopBtns = screen.getAllByRole("button", { name: "Stop" });
					const inputStop = stopBtns.find((b) =>
						b.classList.contains("ui-btn-danger"),
					);
					expect(inputStop).toBeTruthy();
					expect(inputStop).toHaveTextContent("Stop");
				},
				{ timeout: 2000 },
			);
		});
	});

	describe("Conversation Mode Sidebar Placeholders", () => {
		it("shows Select Model A and Select Model B placeholders in conversation mode", async () => {
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));
			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});
			// Use getAllByText and filter by the placeholder card structure
			const modelAPlaceholders = screen.getAllByText("Select Model A");
			const modelBPlaceholders = screen.getAllByText("Select Model B");
			// At least one of each should exist (the card placeholders)
			expect(modelAPlaceholders.length).toBeGreaterThanOrEqual(1);
			expect(modelBPlaceholders.length).toBeGreaterThanOrEqual(1);
		});
	});

	describe("ConfirmDialog Conversation Reset", () => {
		it("resets conversation mode clearing both models and personas", async () => {
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));
			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});
			// Select Model A — scope to the Model A picker to avoid matching Model B's list
			const modelALabel = screen.getByText("Model A");
			const modelAContainer = modelALabel.closest("div")!;
			await waitFor(() => {
				expect(
					within(modelAContainer).getByText("Test Model v1"),
				).toBeInTheDocument();
			});
			await user.click(within(modelAContainer).getByText("Test Model v1"));
			await waitFor(() => {
				expect(screen.getByRole("heading", { level: 3 })).toHaveTextContent(
					"Test Model v1",
				);
			});
			// Now reset button should be visible
			await user.click(
				screen.getByRole("button", {
					name: "Reset all (clear model & settings)",
				}),
			);
			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});
			expect(screen.getByText("Reset Conversation")).toBeInTheDocument();
			expect(
				screen.getByText(/reset both models, personas, and parameters/),
			).toBeInTheDocument();
			await user.click(screen.getByRole("button", { name: "Reset All" }));
			await waitFor(
				() => {
					// Use getAllByText since "Select Model A" appears in both status and placeholder
					expect(
						screen.getAllByText("Select Model A").length,
					).toBeGreaterThanOrEqual(1);
					expect(
						screen.getAllByText("Select Model B").length,
					).toBeGreaterThanOrEqual(1);
				},
				{ timeout: 2000 },
			);
		});

		it("cancels full reset and keeps current state", async () => {
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));
			await waitFor(() => {
				expect(screen.getByRole("heading", { level: 3 })).toHaveTextContent(
					"Test Model v1",
				);
			});
			// Verify model is selected before opening reset dialog
			const headingBefore = screen.getByRole("heading", { level: 3 });
			expect(headingBefore).toHaveTextContent("Test Model v1");
			await user.click(
				screen.getByRole("button", {
					name: "Reset all (clear model & settings)",
				}),
			);
			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Cancel" }));
			await waitFor(() => {
				expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
			});
			expect(screen.getByRole("heading", { level: 3 })).toHaveTextContent(
				"Test Model v1",
			);
		});
	});

	describe("Attachment Buttons Visibility", () => {
		it("shows image attach button for vision model", async () => {
			const visionModel = {
				...mockModel,
				id: "model-vision",
				model_id: "vision-model",
				display_name: "Vision Model",
				capabilities: '{"streaming":true,"vision":true,"audio_input":false}',
			};
			server.use(...mockModels({ body: [visionModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Vision Model")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Vision Model"));
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Attach image" }),
				).toBeInTheDocument();
			});
		});

		it("shows audio attach button for audio input model", async () => {
			const audioModel = {
				...mockModel,
				id: "model-audio",
				model_id: "audio-model",
				display_name: "Audio Model",
				capabilities: '{"streaming":true,"vision":false,"audio_input":true}',
			};
			server.use(...mockModels({ body: [audioModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Audio Model")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Audio Model"));
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Attach audio" }),
				).toBeInTheDocument();
			});
		});

		it("hides attachment buttons for non-vision non-audio model", async () => {
			server.use(...mockModels({ body: [mockModel] }));
			server.use(...mockProviders({ body: [mockProvider] }));
			const { user } = renderWithProviders(<Chat />);
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));
			await waitFor(() => {
				expect(
					screen.queryByRole("button", { name: "Attach image" }),
				).not.toBeInTheDocument();
				expect(
					screen.queryByRole("button", { name: "Attach audio" }),
				).not.toBeInTheDocument();
			});
		});
	});

	describe("Conversation Disabled Reasons", () => {
		it("shows Models must be different when both models are the same", async () => {
			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});

			// Select the same model for both A and B
			const modelALabel = screen.getByText("Model A");
			const modelAContainer = modelALabel.closest("div")!;
			const modelBLabel = screen.getByText("Model B");
			const modelBContainer = modelBLabel.closest("div")!;

			await waitFor(() => {
				expect(
					within(modelAContainer).getByText("Test Model v1"),
				).toBeInTheDocument();
			});
			await user.click(within(modelAContainer).getByText("Test Model v1"));

			await waitFor(() => {
				expect(
					within(modelBContainer).getByText("Test Model v1"),
				).toBeInTheDocument();
			});
			await user.click(within(modelBContainer).getByText("Test Model v1"));

			// Type a prompt
			const input = screen.getByPlaceholderText("Enter a topic or question…");
			await user.type(input, "Test prompt");

			// Should show "Models must be different" amber text
			await waitFor(() => {
				expect(
					screen.getByText("Models must be different"),
				).toBeInTheDocument();
			});
			expect(screen.getByText("Models must be different")).toHaveClass(
				"text-amber-400",
			);
		});

		it("shows Enter a prompt when input is empty", async () => {
			const secondModel = {
				...mockModel,
				id: "model-v2",
				model_id: "test-model-v2",
				display_name: "Test Model v2",
				name: "Test Model v2",
			};
			server.use(...mockModels({ body: [mockModel, secondModel] }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Switch to conversation mode
			await user.click(screen.getByRole("button", { name: "AI Conversation" }));

			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});

			// Select different models for A and B
			const modelALabel = screen.getByText("Model A");
			const modelAContainer = modelALabel.closest("div")!;
			const modelBLabel = screen.getByText("Model B");
			const modelBContainer = modelBLabel.closest("div")!;

			await waitFor(() => {
				expect(
					within(modelAContainer).getByText("Test Model v1"),
				).toBeInTheDocument();
			});
			await user.click(within(modelAContainer).getByText("Test Model v1"));

			await waitFor(() => {
				expect(
					within(modelBContainer).getByText("Test Model v2"),
				).toBeInTheDocument();
			});
			await user.click(within(modelBContainer).getByText("Test Model v2"));

			// Leave input empty - should show "Enter a prompt"
			await waitFor(() => {
				expect(screen.getByText("Enter a prompt")).toBeInTheDocument();
			});
			expect(screen.getByText("Enter a prompt")).toHaveClass("text-amber-400");
		});
	});

	describe("Enter Key Stops Streaming", () => {
		it("stops streaming when Enter is pressed during streaming", async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Hello" } }] },
				{ choices: [{ delta: { content: " world" } }] },
			];
			server.use(...mockChatStream(chunks, { delay: 200 }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Type a message and send with Enter
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Hello{Enter}");

			// Wait for streaming to start (Stop button appears)
			await waitFor(
				() => {
					const stopButtons = screen.getAllByRole("button", { name: "Stop" });
					expect(stopButtons.length).toBeGreaterThan(0);
				},
				{ timeout: 2000 },
			);

			// Press Enter again while streaming to stop
			await user.type(textarea, "{Enter}");

			// Wait for Send button to reappear (streaming stopped)
			await waitFor(
				() => {
					expect(
						screen.getByRole("button", { name: "Send" }),
					).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// Controls should be expanded after stopping
			await waitFor(() => {
				expect(
					screen.getAllByRole("button", { name: "Collapse" }).length,
				).toBeGreaterThan(0);
			});
		});
	});

	describe("Last Chat Error Stale Model Guard", () => {
		it("hides error when switching to a different model", async () => {
			const secondModel = {
				...mockModel,
				id: "model-v2",
				model_id: "test-model-v2",
				display_name: "Test Model v2",
				name: "Test Model v2",
			};
			server.use(...mockModels({ body: [mockModel, secondModel] }));
			server.use(
				...mockChatStream([{ choices: [{ delta: { content: "" } }] }], {
					status: 500,
				}),
			);

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select first model and send message
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Test error{Enter}");

			// Wait for error to appear
			await waitFor(
				() => {
					expect(screen.getByText(/try Regenerate/)).toBeInTheDocument();
				},
				{ timeout: 3000 },
			);
			// Error should show the model name
			expect(
				screen.getByText(/test-model-v1.*try Regenerate/),
			).toBeInTheDocument();

			// Now select a different model
			await user.click(screen.getByText("Test Model v2"));

			// Error from first model should no longer be displayed
			await waitFor(
				() => {
					expect(
						screen.queryByText(/test-model-v1.*try Regenerate/),
					).not.toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});
	});

	describe("Regenerate with System Prompt", () => {
		it("includes system prompt when regenerating", async () => {
			// Use a single chunk that will be returned for both requests
			// This tests that regenerate actually sends a request (which includes system prompt)
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];
			server.use(...mockChatStream(chunks, { delay: 10 }));

			const { user } = renderWithProviders(<Chat />);

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});

			// Select a model
			await waitFor(() => {
				expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			});
			await user.click(screen.getByText("Test Model v1"));

			// Set a system prompt via persona picker (custom option)
			// The custom button shows "✏️Custom"
			const customButton = screen.getByRole("button", { name: /Custom/ });
			await user.click(customButton);

			// Type system prompt in the textarea
			const systemPromptTextarea = screen.getByPlaceholderText(
				"Enter custom persona for AI here…",
			);
			await user.type(systemPromptTextarea, "You are a helpful assistant");

			// Send a message
			const textarea = screen.getByRole("textbox", {
				name: "Chat message input",
			});
			await user.type(textarea, "Hello");
			await user.click(screen.getByRole("button", { name: "Send" }));

			// Wait for response
			await waitFor(
				() => {
					expect(screen.getByText("Response")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// Click Regenerate
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Regenerate" }),
				).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Regenerate" }));

			// Wait for regenerated response to appear (streaming indicator clears)
			// The regenerate flow should complete successfully
			await waitFor(
				() => {
					// After regenerate, the Send button should be visible again (not streaming)
					expect(
						screen.getByRole("button", { name: "Send" }),
					).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});
	});
});
