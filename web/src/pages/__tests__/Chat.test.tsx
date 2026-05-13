import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { mockAllDefaults, mockModels, mockProviders } from "../../test/helpers";
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
				{ timeout: 3000 },
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
});
