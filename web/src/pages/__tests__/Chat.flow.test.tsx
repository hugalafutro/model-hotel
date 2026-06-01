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
		localStorage.clear();
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
					expect(screen.getByText(/generation failed/i)).toBeInTheDocument();
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
					expect(screen.getByText(/generation failed/i)).toBeInTheDocument();
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
			timeout: 15000,
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
			const modelAContainer = modelALabel.closest("div") as HTMLElement;
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
});
