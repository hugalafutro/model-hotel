import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import {
	createSSEStream,
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
					).toHaveAttribute(
						"title",
						"Models are generating - click Stop to cancel",
					);
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

			// Select the same model for both A and B — scope via label's htmlFor
			const modelALabel = document.querySelector(
				'label[for="model-a-picker"]',
			) as HTMLLabelElement;
			const modelAContainer = modelALabel.parentElement as HTMLElement;
			const modelBLabel = document.querySelector(
				'label[for="model-b-picker"]',
			) as HTMLLabelElement;
			const modelBContainer = modelBLabel.parentElement as HTMLElement;

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

			// Select different models for A and B — scope via label's htmlFor
			const modelALabel = document.querySelector(
				'label[for="model-a-picker"]',
			) as HTMLLabelElement;
			const modelAContainer = modelALabel.parentElement as HTMLElement;
			const modelBLabel = document.querySelector(
				'label[for="model-b-picker"]',
			) as HTMLLabelElement;
			const modelBContainer = modelBLabel.parentElement as HTMLElement;

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
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];
			let regenerateRequestMessages: Array<{ role: string }> = [];
			server.use(
				http.post("/api/chat/chat", async ({ request }) => {
					const body = (await request.json()) as {
						messages?: Array<{ role: string }>;
					};
					// Capture messages from requests that include a system prompt
					if (body.messages?.some((m) => m.role === "system")) {
						regenerateRequestMessages = body.messages as Array<{
							role: string;
						}>;
					}
					const stream = createSSEStream(chunks, { delay: 10 });
					return new HttpResponse(stream, {
						status: 200,
						headers: {
							"Content-Type": "text/event-stream",
							"Cache-Control": "no-cache",
						},
					});
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

			// Wait for regenerated response and verify system prompt was included
			await waitFor(
				() => {
					expect(
						screen.getByRole("button", { name: "Send" }),
					).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
			expect(regenerateRequestMessages.some((m) => m.role === "system")).toBe(
				true,
			);
		});
	});
});
