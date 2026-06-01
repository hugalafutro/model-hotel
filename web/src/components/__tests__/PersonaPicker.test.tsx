import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { PersonaPreset } from "../../data/presets";
import { renderWithProviders } from "../../test/utils";
import { PersonaPicker } from "../PersonaPicker";

const mockPersonas: PersonaPreset[] = [
	{
		id: "merlin",
		icon: "🧙",
		label: "Merlin",
		systemPrompt: "You are a wise wizard",
	},
	{
		id: "sarge",
		icon: "🎖️",
		label: "Sarge",
		systemPrompt: "You are a drill sergeant",
	},
	{
		id: "buddy",
		icon: "🤖",
		label: "Buddy",
		systemPrompt: "You are a friendly robot",
	},
];

describe("PersonaPicker", () => {
	const mockOnActivePersonaChange = vi.fn();
	const mockOnSystemPromptChange = vi.fn();

	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("basic rendering", () => {
		it("renders label", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
					label="Custom Label"
				/>,
			);
			expect(screen.getByText("Custom Label")).toBeInTheDocument();
		});

		it("defaults label to 'Persona'", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			expect(screen.getByText("Persona")).toBeInTheDocument();
		});

		it("renders textarea with placeholder", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
					textareaPlaceholder="Custom placeholder"
				/>,
			);
			expect(
				screen.getByPlaceholderText("Custom placeholder"),
			).toBeInTheDocument();
		});

		it("defaults textarea placeholder", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			expect(
				screen.getByPlaceholderText("Enter custom persona for AI here…"),
			).toBeInTheDocument();
		});

		it("renders PresetBar with all personas", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			expect(screen.getByText("🧙Merlin")).toBeInTheDocument();
			expect(screen.getByText("🎖️Sarge")).toBeInTheDocument();
			expect(screen.getByText("🤖Buddy")).toBeInTheDocument();
		});

		it("applies custom className", () => {
			const { container } = renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
					className="custom-class"
				/>,
			);
			expect(container.firstChild).toHaveClass("custom-class");
		});
	});

	describe("textarea functionality", () => {
		it("displays current systemPrompt value", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt="Existing prompt text"
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			expect(
				screen.getByDisplayValue("Existing prompt text"),
			).toBeInTheDocument();
		});

		it("calls onSystemPromptChange when typing", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			const textarea = screen.getByRole("textbox");
			await user.type(textarea, "Hello");
			expect(mockOnSystemPromptChange).toHaveBeenCalledTimes(5);
			// user.type fires onChange for each character individually
			expect(mockOnSystemPromptChange).toHaveBeenNthCalledWith(1, "H");
			expect(mockOnSystemPromptChange).toHaveBeenNthCalledWith(5, "o");
		});

		it("switches to custom (null) when editing preset text", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId="merlin"
					systemPrompt="You are a wise wizard"
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			const textarea = screen.getByRole("textbox");
			await user.type(textarea, " Extra");
			expect(mockOnActivePersonaChange).toHaveBeenCalledWith(null);
		});

		it("does not switch to custom when text matches preset exactly", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId="merlin"
					systemPrompt="You are a wise wizard"
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			// Just rendering - no change should be called
			expect(mockOnActivePersonaChange).not.toHaveBeenCalled();
		});

		it("is disabled when disabled prop is true", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
					disabled
				/>,
			);
			expect(screen.getByRole("textbox")).toBeDisabled();
		});

		it("is enabled by default", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			expect(screen.getByRole("textbox")).not.toBeDisabled();
		});
	});

	describe("preset selection", () => {
		it("calls handlers when selecting preset without existing text", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			await user.click(screen.getByText("🧙Merlin"));
			expect(mockOnSystemPromptChange).toHaveBeenCalledWith(
				"You are a wise wizard",
			);
			expect(mockOnActivePersonaChange).toHaveBeenCalledWith("merlin");
		});

		it("shows ConfirmDialog when selecting preset with existing custom text", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt="Some custom text"
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			await user.click(screen.getByText("🧙Merlin"));
			expect(screen.getByText("Overwrite Prompt")).toBeInTheDocument();
			expect(screen.getByText("System prompt")).toBeInTheDocument();
			// Handlers should NOT be called yet
			expect(mockOnSystemPromptChange).not.toHaveBeenCalled();
			expect(mockOnActivePersonaChange).not.toHaveBeenCalled();
		});

		it("confirms overwrite when clicking Discard in dialog", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt="Some custom text"
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			await user.click(screen.getByText("🧙Merlin"));
			await user.click(screen.getByText("Delete"));
			expect(mockOnSystemPromptChange).toHaveBeenCalledWith(
				"You are a wise wizard",
			);
			expect(mockOnActivePersonaChange).toHaveBeenCalledWith("merlin");
		});

		it("cancels overwrite when clicking Cancel in dialog", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt="Some custom text"
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			await user.click(screen.getByText("🧙Merlin"));
			await user.click(screen.getByText("Cancel"));
			expect(mockOnSystemPromptChange).not.toHaveBeenCalled();
			expect(mockOnActivePersonaChange).not.toHaveBeenCalled();
			// Dialog should be closed
			expect(screen.queryByText("Overwrite Prompt")).not.toBeInTheDocument();
		});
	});

	describe("custom mode", () => {
		it("shows ConfirmDialog when switching from preset to custom", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId="merlin"
					systemPrompt="You are a wise wizard"
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			await user.click(screen.getByText("✏️Custom"));
			expect(screen.getByText("Switch to Custom")).toBeInTheDocument();
			expect(
				screen.getByText(
					"This will clear the current persona prompt. Continue?",
				),
			).toBeInTheDocument();
		});

		it("confirms switch to custom when clicking Discard", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId="merlin"
					systemPrompt="You are a wise wizard"
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			await user.click(screen.getByText("✏️Custom"));
			await user.click(screen.getByText("Delete"));
			expect(mockOnSystemPromptChange).toHaveBeenCalledWith("");
			expect(mockOnActivePersonaChange).toHaveBeenCalledWith(null);
		});

		it("does not show dialog when already in custom mode", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt="Custom text"
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			await user.click(screen.getByText("✏️Custom"));
			// No dialog should appear
			expect(screen.queryByText("Switch to Custom")).not.toBeInTheDocument();
		});
	});

	describe("random button", () => {
		it("calls onRandom when provided and random button clicked", async () => {
			const user = userEvent.setup();
			const mockOnRandom = vi.fn();
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
					onRandom={mockOnRandom}
				/>,
			);
			const randomButton = screen.getByTitle("Random");
			await user.click(randomButton);
			expect(mockOnRandom).toHaveBeenCalledTimes(1);
		});

		it("does not show random button when onRandom not provided", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			expect(screen.queryByTitle("Random")).not.toBeInTheDocument();
		});
	});

	describe("collapse/expand", () => {
		it("renders CollapsibleToggle", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			// CollapsibleToggle renders a button with title "Collapse"
			expect(screen.getByTitle("Collapse")).toBeInTheDocument();
		});

		it("can toggle collapsed state", async () => {
			const user = userEvent.setup();
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			const toggle = screen.getByTitle("Collapse");
			await user.click(toggle);
			// After clicking, title changes to "Expand"
			expect(screen.getByTitle("Expand")).toBeInTheDocument();
		});
	});

	describe("active persona highlighting", () => {
		it("highlights active persona with ui-btn-primary", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId="sarge"
					systemPrompt="You are a drill sergeant"
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			const activeButton = screen.getByText("🎖️Sarge").closest("button");
			expect(activeButton).toHaveClass("ui-btn-primary");
		});

		it("highlights custom button with ui-btn-primary when in custom mode", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt="Custom text"
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			const customButton = screen.getByText("✏️Custom").closest("button");
			expect(customButton).toHaveClass("ui-btn-primary");
		});

		it("shows inactive personas with ui-btn-secondary", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId="merlin"
					systemPrompt="You are a wise wizard"
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			const inactiveButton = screen.getByText("🎖️Sarge").closest("button");
			expect(inactiveButton).toHaveClass("ui-btn-secondary");
		});
	});

	describe("textarea auto-expand", () => {
		it("renders textarea with auto height style for auto-expand", () => {
			renderWithProviders(
				<PersonaPicker
					personas={mockPersonas}
					activePersonaId={null}
					systemPrompt=""
					onActivePersonaChange={mockOnActivePersonaChange}
					onSystemPromptChange={mockOnSystemPromptChange}
				/>,
			);
			const textarea = screen.getByRole("textbox");
			// Component initializes with height: auto for auto-expand behavior
			expect(textarea.style.height).toBe("auto");
		});
	});
});
