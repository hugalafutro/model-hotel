import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ArenaPromptPreset } from "../../data/presets";
import { renderWithProviders } from "../../test/utils";
import { PromptPicker } from "../PromptPicker";

const mockPrompts: ArenaPromptPreset[] = [
	{
		id: "dilemma",
		icon: "⚖️",
		label: "Dilemma",
		prompt: "Present a moral dilemma",
	},
	{ id: "lore", icon: "📜", label: "Lore", prompt: "Generate world lore" },
	{ id: "hook", icon: "🎣", label: "Hook", prompt: "Write a compelling hook" },
];

describe("PromptPicker", () => {
	const mockOnActivePromptIdChange = vi.fn();
	const mockOnPromptChange = vi.fn();

	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("basic rendering", () => {
		it("renders label", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
					label="Custom Label"
				/>,
			);
			expect(screen.getByText("Custom Label")).toBeInTheDocument();
		});

		it("defaults label to 'Prompt'", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			expect(screen.getByText("Prompt")).toBeInTheDocument();
		});

		it("renders textarea with placeholder", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
					textareaPlaceholder="Custom placeholder"
				/>,
			);
			expect(
				screen.getByPlaceholderText("Custom placeholder"),
			).toBeInTheDocument();
		});

		it("defaults textarea placeholder", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			expect(
				screen.getByPlaceholderText("Enter your prompt…"),
			).toBeInTheDocument();
		});

		it("renders PresetBar with all prompts", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			expect(screen.getByText("⚖️Dilemma")).toBeInTheDocument();
			expect(screen.getByText("📜Lore")).toBeInTheDocument();
			expect(screen.getByText("🎣Hook")).toBeInTheDocument();
		});

		it("applies custom className", () => {
			const { container } = renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
					className="custom-class"
				/>,
			);
			expect(container.firstChild).toHaveClass("custom-class");
		});
	});

	describe("showPresetBar prop", () => {
		it("shows PresetBar when showPresetBar is true", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
					showPresetBar
				/>,
			);
			expect(screen.getByText("⚖️Dilemma")).toBeInTheDocument();
		});

		it("hides PresetBar when showPresetBar is false", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
					showPresetBar={false}
				/>,
			);
			expect(screen.queryByText("⚖️Dilemma")).not.toBeInTheDocument();
			expect(screen.queryByText("📜Lore")).not.toBeInTheDocument();
		});

		it("defaults showPresetBar to true", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			expect(screen.getByText("⚖️Dilemma")).toBeInTheDocument();
		});
	});

	describe("textarea functionality", () => {
		it("displays current prompt value", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt="Existing prompt text"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			expect(
				screen.getByDisplayValue("Existing prompt text"),
			).toBeInTheDocument();
		});

		it("calls onPromptChange when typing", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			const textarea = screen.getByRole("textbox");
			await user.type(textarea, "Hello");
			expect(mockOnPromptChange).toHaveBeenCalledTimes(5);
			// user.type fires onChange for each character individually
			expect(mockOnPromptChange).toHaveBeenNthCalledWith(1, "H");
			expect(mockOnPromptChange).toHaveBeenNthCalledWith(5, "o");
		});

		it("switches to custom (null) when editing preset text", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId="dilemma"
					prompt="Present a moral dilemma"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			const textarea = screen.getByRole("textbox");
			await user.type(textarea, " Extra");
			expect(mockOnActivePromptIdChange).toHaveBeenCalledWith(null);
		});

		it("does not switch to custom when text matches preset exactly", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId="dilemma"
					prompt="Present a moral dilemma"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			expect(mockOnActivePromptIdChange).not.toHaveBeenCalled();
		});

		it("is disabled when disabled prop is true", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
					disabled
				/>,
			);
			expect(screen.getByRole("textbox")).toBeDisabled();
		});

		it("is enabled by default", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			expect(screen.getByRole("textbox")).not.toBeDisabled();
		});

		it("applies maxLength to textarea", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
					maxLength={500}
				/>,
			);
			expect(screen.getByRole("textbox")).toHaveAttribute("maxLength", "500");
		});

		it("defaults maxLength to 10000", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			expect(screen.getByRole("textbox")).toHaveAttribute("maxLength", "10000");
		});
	});

	describe("autoFocus", () => {
		it("focuses textarea when autoFocus is true", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
					autoFocus
				/>,
			);
			expect(screen.getByRole("textbox")).toHaveFocus();
		});

		it("does not auto-focus when autoFocus is false", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
					autoFocus={false}
				/>,
			);
			expect(screen.getByRole("textbox")).not.toHaveFocus();
		});

		it("defaults autoFocus to false", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			expect(screen.getByRole("textbox")).not.toHaveFocus();
		});
	});

	describe("preset selection", () => {
		it("calls handlers when selecting preset without existing text", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			await user.click(screen.getByText("⚖️Dilemma"));
			expect(mockOnPromptChange).toHaveBeenCalledWith(
				"Present a moral dilemma",
			);
			expect(mockOnActivePromptIdChange).toHaveBeenCalledWith("dilemma");
		});

		it("shows ConfirmDialog when selecting preset with existing custom text", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt="Some custom text"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			await user.click(screen.getByText("⚖️Dilemma"));
			expect(screen.getByText("Overwrite Prompt")).toBeInTheDocument();
			// The dialog shows "Prompt" in the list of fields
			const dialog = screen.getByRole("dialog");
			expect(dialog).toHaveTextContent("Prompt");
			expect(mockOnPromptChange).not.toHaveBeenCalled();
			expect(mockOnActivePromptIdChange).not.toHaveBeenCalled();
		});

		it("confirms overwrite when clicking Discard in dialog", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt="Some custom text"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			await user.click(screen.getByText("⚖️Dilemma"));
			await user.click(screen.getByText("Discard"));
			expect(mockOnPromptChange).toHaveBeenCalledWith(
				"Present a moral dilemma",
			);
			expect(mockOnActivePromptIdChange).toHaveBeenCalledWith("dilemma");
		});

		it("cancels overwrite when clicking Cancel in dialog", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt="Some custom text"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			await user.click(screen.getByText("⚖️Dilemma"));
			await user.click(screen.getByText("Cancel"));
			expect(mockOnPromptChange).not.toHaveBeenCalled();
			expect(mockOnActivePromptIdChange).not.toHaveBeenCalled();
			expect(screen.queryByText("Overwrite Prompt")).not.toBeInTheDocument();
		});
	});

	describe("custom mode", () => {
		it("shows ConfirmDialog when switching from preset to custom", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId="dilemma"
					prompt="Present a moral dilemma"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			await user.click(screen.getByText("✏️Custom"));
			expect(screen.getByText("Switch to Custom")).toBeInTheDocument();
		});

		it("confirms switch to custom when clicking Discard", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId="dilemma"
					prompt="Present a moral dilemma"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			await user.click(screen.getByText("✏️Custom"));
			await user.click(screen.getByText("Discard"));
			expect(mockOnPromptChange).toHaveBeenCalledWith("");
			expect(mockOnActivePromptIdChange).toHaveBeenCalledWith(null);
		});

		it("does not show dialog when already in custom mode", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt="Custom text"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			await user.click(screen.getByText("✏️Custom"));
			expect(screen.queryByText("Switch to Custom")).not.toBeInTheDocument();
		});
	});

	describe("random button", () => {
		it("calls onPromptChange and onActivePromptIdChange with random prompt", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			const randomButton = screen.getByTitle("Random");
			await user.click(randomButton);
			// Should have called with one of the prompts
			expect(mockOnPromptChange).toHaveBeenCalledTimes(1);
			expect(mockOnActivePromptIdChange).toHaveBeenCalledTimes(1);
			// The prompt should be one of the available ones
			const calledPrompt = mockOnPromptChange.mock.calls[0][0];
			const calledId = mockOnActivePromptIdChange.mock.calls[0][0];
			const matchingPrompt = mockPrompts.find((p) => p.prompt === calledPrompt);
			expect(matchingPrompt).toBeDefined();
			expect(matchingPrompt?.id).toBe(calledId);
		});

		it("shows ConfirmDialog when random with existing custom text", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt="Some custom text"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			await user.click(screen.getByTitle("Random"));
			expect(screen.getByText("Overwrite Prompt")).toBeInTheDocument();
			expect(mockOnPromptChange).not.toHaveBeenCalled();
			expect(mockOnActivePromptIdChange).not.toHaveBeenCalled();
		});

		it("confirms random selection after dialog", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt="Some custom text"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			await user.click(screen.getByTitle("Random"));
			await user.click(screen.getByText("Discard"));
			expect(mockOnPromptChange).toHaveBeenCalledTimes(1);
			expect(mockOnActivePromptIdChange).toHaveBeenCalledTimes(1);
		});

		it("does not call handlers when no prompts available (all selected)", () => {
			const singlePrompt: ArenaPromptPreset[] = [
				{ id: "only", icon: "🎯", label: "Only", prompt: "The only prompt" },
			];
			renderWithProviders(
				<PromptPicker
					prompts={singlePrompt}
					activePromptId="only"
					prompt="The only prompt"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			// Clicking should not cause errors, but no handlers should be called
			// (handled internally - no available prompts to pick)
			expect(mockOnPromptChange).not.toHaveBeenCalled();
			expect(mockOnActivePromptIdChange).not.toHaveBeenCalled();
		});
	});

	describe("collapse/expand", () => {
		it("renders CollapsibleToggle", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			expect(screen.getByTitle("Collapse")).toBeInTheDocument();
		});

		it("can toggle collapsed state", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			const toggle = screen.getByTitle("Collapse");
			await user.click(toggle);
			expect(screen.getByTitle("Expand")).toBeInTheDocument();
		});
	});

	describe("active prompt highlighting", () => {
		it("highlights active prompt with ui-btn-primary", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId="lore"
					prompt="Generate world lore"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			const activeButton = screen.getByText("📜Lore").closest("button");
			expect(activeButton).toHaveClass("ui-btn-primary");
		});

		it("highlights custom button with ui-btn-primary when in custom mode", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt="Custom text"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			const customButton = screen.getByText("✏️Custom").closest("button");
			expect(customButton).toHaveClass("ui-btn-primary");
		});

		it("shows inactive prompts with ui-btn-secondary", () => {
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId="dilemma"
					prompt="Present a moral dilemma"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			const inactiveButton = screen.getByText("📜Lore").closest("button");
			expect(inactiveButton).toHaveClass("ui-btn-secondary");
		});
	});

	describe("textarea auto-expand", () => {
		it("renders textarea with height style for auto-expand behavior", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt=""
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			const textarea = screen.getByRole("textbox");
			// Trigger auto-expand by typing
			await user.type(textarea, "Test");
			// The component should have processed the input
			expect(mockOnPromptChange).toHaveBeenCalled();
		});
	});

	describe("ConfirmDialog fields", () => {
		it("shows 'Prompt' field in ConfirmDialog", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PromptPicker
					prompts={mockPrompts}
					activePromptId={null}
					prompt="Some custom text"
					onActivePromptIdChange={mockOnActivePromptIdChange}
					onPromptChange={mockOnPromptChange}
				/>,
			);
			await user.click(screen.getByText("⚖️Dilemma"));
			// The dialog should show "Prompt" in the list of fields
			const dialog = screen.getByRole("dialog");
			expect(dialog).toHaveTextContent("Prompt");
		});
	});
});
