import { fireEvent, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CandidateModel, FailoverGroup } from "../../../api/types";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { CreateGroupModal } from "../CreateGroupModal";

const mockCandidates: CandidateModel[] = [
	{
		model_uuid: "uuid-1",
		model_id: "gemma3:4b",
		provider_id: "provider-001",
		provider_name: "Ollama Cloud",
		display_name: "Gemma 3 4B",
		context_length: 8192,
		owned_by: "google",
	},
	{
		model_uuid: "uuid-2",
		model_id: "gemma3:4b",
		provider_id: "provider-002",
		provider_name: "NanoGPT",
		display_name: "Gemma 3",
		context_length: 8192,
		owned_by: "google",
	},
	{
		model_uuid: "uuid-3",
		model_id: "deepseek-chat",
		provider_id: "provider-003",
		provider_name: "DeepSeek",
		display_name: "DeepSeek Chat",
		context_length: 32768,
		owned_by: "deepseek",
	},
	{
		model_uuid: "uuid-4",
		model_id: "glm-5",
		provider_id: "provider-004",
		provider_name: "Z.ai",
		display_name: "GLM-5",
		context_length: 128000,
		owned_by: "zai",
	},
];

const mockEditGroup: FailoverGroup = {
	id: "fg-001",
	display_model: "test-model",
	display_name: "Test Failover Group",
	description: "A test failover group",
	group_enabled: true,
	auto_created: false,
	entries: [
		{
			model_uuid: "uuid-1",
			model_id: "gemma3:4b",
			provider_id: "provider-001",
			provider_name: "Ollama Cloud",
			display_name: "Gemma 3 4B",
			enabled: true,
			model_enabled: true,
			provider_enabled: true,
			context_length: 8192,
			owned_by: "google",
		},
		{
			model_uuid: "uuid-2",
			model_id: "gemma3:4b",
			provider_id: "provider-002",
			provider_name: "NanoGPT",
			display_name: "Gemma 3",
			enabled: true,
			model_enabled: true,
			provider_enabled: true,
			context_length: 8192,
			owned_by: "google",
		},
	],
	total_tokens: 0,
	created_at: "2026-04-01T10:00:00Z",
	updated_at: "2026-05-10T12:00:00Z",
};

const mockOnClose = vi.fn();
const mockOnCreated = vi.fn();
const mockOnUpdated = vi.fn();

describe("CreateGroupModal", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("rendering modal", () => {
		it("renders modal with create title", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(screen.getByText("Create Failover Group")).toBeInTheDocument();
		});

		it("renders display model name input", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(screen.getByLabelText("Display Model Name")).toBeInTheDocument();
			expect(screen.getByPlaceholderText("e.g., glm-5")).toBeInTheDocument();
		});

		it("renders display name input", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(
				screen.getByLabelText("Display Name (optional)"),
			).toBeInTheDocument();
			expect(
				screen.getByPlaceholderText("e.g., GLM-5 Failover"),
			).toBeInTheDocument();
		});

		it("renders description input", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(
				screen.getByLabelText("Description (optional)"),
			).toBeInTheDocument();
			expect(
				screen.getByPlaceholderText("e.g., Failover group for GLM-5 models"),
			).toBeInTheDocument();
		});

		it("renders ModelPicker with label", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(screen.getByLabelText("Model Entries")).toBeInTheDocument();
		});

		it("renders model pills for each candidate", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(screen.getByText("Gemma 3 4B")).toBeInTheDocument();
			expect(screen.getByText("Gemma 3")).toBeInTheDocument();
			expect(screen.getByText("DeepSeek Chat")).toBeInTheDocument();
			expect(screen.getByText("GLM-5")).toBeInTheDocument();
		});

		it("renders provider group buttons", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			// Provider names are shown in the collapse buttons
			expect(screen.getByText("Ollama Cloud")).toBeInTheDocument();
			expect(screen.getByText("NanoGPT")).toBeInTheDocument();
			expect(screen.getByText("DeepSeek")).toBeInTheDocument();
			expect(screen.getByText("Z.ai")).toBeInTheDocument();
		});

		it("renders selected count", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(screen.getByText("0 selected")).toBeInTheDocument();
		});

		it("renders filter input with placeholder", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(screen.getByPlaceholderText("Filter models…")).toBeInTheDocument();
		});

		it("renders cancel button", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(
				screen.getByRole("button", { name: "Cancel" }),
			).toBeInTheDocument();
		});

		it("renders create group button", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(
				screen.getByRole("button", { name: "Create Group" }),
			).toBeInTheDocument();
		});

		it("shows helper text for display model name", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(
				screen.getByText(`This becomes hotel/model-name in the model list`),
			).toBeInTheDocument();
		});
	});

	describe("form inputs", () => {
		it("types in display model name", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const input = screen.getByLabelText("Display Model Name");
			await user.type(input, "glm-5");

			expect(input).toHaveValue("glm-5");
		});

		it("types in display name", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const input = screen.getByLabelText("Display Name (optional)");
			await user.type(input, "GLM-5 Failover");

			expect(input).toHaveValue("GLM-5 Failover");
		});

		it("types in description", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const input = screen.getByLabelText("Description (optional)");
			await user.type(input, "A test failover group");

			expect(input).toHaveValue("A test failover group");
		});

		it("updates helper text when typing display model name", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const input = screen.getByLabelText("Display Model Name");
			await user.type(input, "my-model");

			expect(
				screen.getByText(`This becomes hotel/my-model in the model list`),
			).toBeInTheDocument();
		});
	});

	describe("search/filter functionality", () => {
		it("filters models by typing in filter input", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const filterInput = screen.getByPlaceholderText("Filter models…");
			await user.type(filterInput, "deepseek");

			// DeepSeek Chat should be visible
			expect(screen.getByText("DeepSeek Chat")).toBeInTheDocument();
			// Others should be filtered out
			expect(screen.queryByText("Gemma 3 4B")).not.toBeInTheDocument();
			expect(screen.queryByText("Gemma 3")).not.toBeInTheDocument();
			expect(screen.queryByText("GLM-5")).not.toBeInTheDocument();
		});

		it("filter is case-insensitive", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const filterInput = screen.getByPlaceholderText("Filter models…");
			await user.type(filterInput, "DEEPSEEK");

			expect(screen.getByText("DeepSeek Chat")).toBeInTheDocument();
		});

		it("clearing filter shows all models", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const filterInput = screen.getByPlaceholderText("Filter models…");
			await user.type(filterInput, "deepseek");
			await user.clear(filterInput);

			// All should be visible again
			expect(screen.getByText("Gemma 3 4B")).toBeInTheDocument();
			expect(screen.getByText("Gemma 3")).toBeInTheDocument();
			expect(screen.getByText("DeepSeek Chat")).toBeInTheDocument();
			expect(screen.getByText("GLM-5")).toBeInTheDocument();
		});
	});

	describe("model selection via pills", () => {
		it("selects model when pill is clicked", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const pill = screen.getByText("Gemma 3 4B");
			await user.click(pill);

			expect(screen.getByText("1 selected")).toBeInTheDocument();
		});

		it("deselects model when pill is clicked again", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const pill = screen.getByText("Gemma 3 4B");
			await user.click(pill);
			await user.click(pill);

			expect(screen.getByText("0 selected")).toBeInTheDocument();
		});

		it("selects multiple models", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			await user.click(screen.getByText("Gemma 3 4B"));
			await user.click(screen.getByText("Gemma 3"));
			await user.click(screen.getByText("DeepSeek Chat"));

			expect(screen.getByText("3 selected")).toBeInTheDocument();
		});

		it("selected pill gets accent class", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const pill = screen.getByText("Gemma 3 4B");
			await user.click(pill);

			const pillContainer = pill.closest("div");
			expect(pillContainer).toHaveClass("bg-(--accent)/15");
		});
	});

	describe("form validation", () => {
		it("shows error toast when display model name is empty (JS guard)", async () => {
			// Bypass HTML5 required validation by submitting the form directly
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			// Select 2 models but leave display model name empty
			await screen.getByText("Gemma 3 4B").click();
			await screen.getByText("Gemma 3").click();

			// Submit form directly (bypasses browser required check)
			const form = screen
				.getByLabelText("Display Model Name")
				.closest("form") as HTMLFormElement;
			fireEvent.submit(form);

			await waitFor(() => {
				expect(
					screen.getByText("Display model name is required"),
				).toBeInTheDocument();
			});

			expect(mockOnCreated).not.toHaveBeenCalled();
		});

		it("prevents submission when display model name is empty", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			// Select 2 models
			await user.click(screen.getByText("Gemma 3 4B"));
			await user.click(screen.getByText("Gemma 3"));

			// Submit without display model name
			const submitButton = screen.getByRole("button", {
				name: "Create Group",
			});
			await user.click(submitButton);

			// onCreated should NOT be called (form validation prevented submission)
			expect(mockOnCreated).not.toHaveBeenCalled();
		});

		it("shows error toast when less than 2 models selected", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			// Type display model name
			const displayModelInput = screen.getByLabelText("Display Model Name");
			await user.type(displayModelInput, "glm-5");

			// Select only 1 model
			await user.click(screen.getByText("Gemma 3 4B"));

			// Submit
			const submitButton = screen.getByRole("button", {
				name: "Create Group",
			});
			await user.click(submitButton);

			// Error toast should appear
			await waitFor(() => {
				expect(
					screen.getByText("At least 2 entries required"),
				).toBeInTheDocument();
			});

			expect(mockOnCreated).not.toHaveBeenCalled();
		});

		it("shows error toast when no models selected", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			// Type display model name
			const displayModelInput = screen.getByLabelText("Display Model Name");
			await user.type(displayModelInput, "glm-5");

			// Submit without selecting any models
			const submitButton = screen.getByRole("button", {
				name: "Create Group",
			});
			await user.click(submitButton);

			// Error toast should appear
			await waitFor(() => {
				expect(
					screen.getByText("At least 2 entries required"),
				).toBeInTheDocument();
			});

			expect(mockOnCreated).not.toHaveBeenCalled();
		});

		it("allows submission with valid form data", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			// Fill display model name
			const displayModelInput = screen.getByLabelText("Display Model Name");
			await user.type(displayModelInput, "glm-5");

			// Select 2 models
			await user.click(screen.getByText("Gemma 3 4B"));
			await user.click(screen.getByText("Gemma 3"));

			// Submit
			const submitButton = screen.getByRole("button", {
				name: "Create Group",
			});
			await user.click(submitButton);

			// Should not show validation errors
			await waitFor(() => {
				expect(
					screen.queryByText("Display model name is required"),
				).not.toBeInTheDocument();
				expect(
					screen.queryByText("At least 2 entries required"),
				).not.toBeInTheDocument();
			});
		});
	});

	describe("cancel button", () => {
		it("calls onClose when cancel button is clicked", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const cancelButton = screen.getByRole("button", { name: "Cancel" });
			await user.click(cancelButton);

			expect(mockOnClose).toHaveBeenCalled();
		});
	});

	describe("successful creation", () => {
		it("calls onCreated and shows success toast on successful creation", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			// Fill form
			const displayModelInput = screen.getByLabelText("Display Model Name");
			await user.type(displayModelInput, "glm-5");

			const displayNameInput = screen.getByLabelText("Display Name (optional)");
			await user.type(displayNameInput, "GLM-5 Failover");

			const descriptionInput = screen.getByLabelText("Description (optional)");
			await user.type(descriptionInput, "Failover group for GLM-5");

			// Select 2 models
			await user.click(screen.getByText("Gemma 3 4B"));
			await user.click(screen.getByText("Gemma 3"));

			// Submit
			const submitButton = screen.getByRole("button", {
				name: "Create Group",
			});
			await user.click(submitButton);

			// Should call onCreated after successful mutation
			await waitFor(() => {
				expect(mockOnCreated).toHaveBeenCalled();
			});

			// Success toast should appear
			await waitFor(() => {
				expect(screen.getByText("Failover group created")).toBeInTheDocument();
			});
		});
	});

	describe("input constraints", () => {
		it("enforces maxLength of 128 on display model name", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const input = screen.getByLabelText("Display Model Name");
			expect(input).toHaveAttribute("maxLength", "128");
		});

		it("enforces maxLength of 128 on display name", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const input = screen.getByLabelText("Display Name (optional)");
			expect(input).toHaveAttribute("maxLength", "128");
		});

		it("enforces maxLength of 256 on description", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const input = screen.getByLabelText("Description (optional)");
			expect(input).toHaveAttribute("maxLength", "256");
		});

		it("marks display model name as required", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const input = screen.getByLabelText("Display Model Name");
			expect(input).toHaveAttribute("required");
		});

		it("does not mark display name as required", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const input = screen.getByLabelText("Display Name (optional)");
			expect(input).not.toHaveAttribute("required");
		});

		it("does not mark description as required", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const input = screen.getByLabelText("Description (optional)");
			expect(input).not.toHaveAttribute("required");
		});
	});

	describe("model display", () => {
		it("shows provider names in provider group buttons", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			// Provider names are shown in the collapse buttons
			expect(screen.getByText("Ollama Cloud")).toBeInTheDocument();
			expect(screen.getByText("NanoGPT")).toBeInTheDocument();
			expect(screen.getByText("DeepSeek")).toBeInTheDocument();
			expect(screen.getByText("Z.ai")).toBeInTheDocument();
		});

		it("shows display names in pill buttons", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(screen.getByText("Gemma 3 4B")).toBeInTheDocument();
			expect(screen.getByText("Gemma 3")).toBeInTheDocument();
			expect(screen.getByText("DeepSeek Chat")).toBeInTheDocument();
			expect(screen.getByText("GLM-5")).toBeInTheDocument();
		});

		it("shows model_id as fallback when display_name is empty", () => {
			const candidatesWithEmptyDisplayName: CandidateModel[] = [
				{
					...mockCandidates[0],
					display_name: "",
				},
			];

			renderWithProviders(
				<CreateGroupModal
					candidates={candidatesWithEmptyDisplayName}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			// Should show model_id as fallback
			expect(screen.getByText("gemma3:4b")).toBeInTheDocument();
		});

		it("shows model_id as pill label when candidate has no display_name", () => {
			const candidatesNoDisplay: CandidateModel[] = [
				{
					model_uuid: "uuid-no-display",
					model_id: "no-display-model",
					provider_id: "provider-005",
					provider_name: "Test Provider",
					display_name: "",
					context_length: 4096,
					owned_by: "test",
				},
				{
					model_uuid: "uuid-no-display-2",
					model_id: "no-display-model-2",
					provider_id: "provider-006",
					provider_name: "Test Provider",
					display_name: "",
					context_length: 4096,
					owned_by: "test",
				},
			];

			renderWithProviders(
				<CreateGroupModal
					candidates={candidatesNoDisplay}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			expect(screen.getByText("no-display-model")).toBeInTheDocument();
			expect(screen.getByText("no-display-model-2")).toBeInTheDocument();
		});
	});

	describe("edit mode", () => {
		it("renders edit title", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			expect(screen.getByText("Edit Failover Group")).toBeInTheDocument();
		});

		it("enables display model name input in edit mode", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			const input = screen.getByLabelText("Display Model Name");
			expect(input).not.toBeDisabled();
			expect(input).toHaveValue("test-model");
		});

		it("shows hotel/ pattern helper text in edit mode", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			expect(
				screen.getByText(`This becomes hotel/test-model in the model list`),
			).toBeInTheDocument();
		});

		it("pre-fills display name with group value", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			const input = screen.getByLabelText("Display Name (optional)");
			expect(input).toHaveValue("Test Failover Group");
		});

		it("pre-fills description with group value", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			const input = screen.getByLabelText("Description (optional)");
			expect(input).toHaveValue("A test failover group");
		});

		it("pre-selects pills from group entries", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			// Should show 2 selected (from group.entries)
			expect(screen.getByText("2 selected")).toBeInTheDocument();

			// The pills should have the selected class
			const gemma4bPill = screen.getByText("Gemma 3 4B");
			const gemma3Pill = screen.getByText("Gemma 3");

			expect(gemma4bPill.closest("div")).toHaveClass("bg-(--accent)/15");
			expect(gemma3Pill.closest("div")).toHaveClass("bg-(--accent)/15");
		});

		it("shows save changes button", () => {
			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			expect(
				screen.getByRole("button", { name: "Save Changes" }),
			).toBeInTheDocument();
		});

		it("calls onUpdated and shows success toast on successful update", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			// Change display name
			const displayNameInput = screen.getByLabelText("Display Name (optional)");
			await user.clear(displayNameInput);
			await user.type(displayNameInput, "Updated Failover Group");

			// Submit
			const submitButton = screen.getByRole("button", {
				name: "Save Changes",
			});
			await user.click(submitButton);

			// Should call onUpdated after successful mutation
			await waitFor(() => {
				expect(mockOnUpdated).toHaveBeenCalled();
			});

			// Success toast should appear
			await waitFor(() => {
				expect(screen.getByText("Failover group updated")).toBeInTheDocument();
			});
		});

		it("shows error toast when less than 2 entries selected in edit mode", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			// Deselect one of the pre-selected models
			await user.click(screen.getByText("Gemma 3 4B"));

			// Now only 1 is selected
			expect(screen.getByText("1 selected")).toBeInTheDocument();

			// Submit
			const submitButton = screen.getByRole("button", {
				name: "Save Changes",
			});
			await user.click(submitButton);

			// Error toast should appear
			await waitFor(() => {
				expect(
					screen.getByText("At least 2 entries required"),
				).toBeInTheDocument();
			});

			expect(mockOnUpdated).not.toHaveBeenCalled();
		});

		it("preserves entries from group not present in candidates", () => {
			// Simulate a group with an entry whose provider is no longer in candidates
			const groupWithUnavailableEntry: FailoverGroup = {
				...mockEditGroup,
				entries: [
					...mockEditGroup.entries,
					{
						model_uuid: "uuid-unavailable",
						model_id: "old-model",
						provider_id: "p-old",
						provider_name: "Old Provider",
						display_name: "Old Model",
						enabled: true,
						model_enabled: true,
						provider_enabled: true,
						context_length: 4096,
						owned_by: "old",
					},
				],
			};

			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={groupWithUnavailableEntry}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			// All 3 entries should show as selected (2 from candidates + 1 unavailable)
			expect(screen.getByText("3 selected")).toBeInTheDocument();

			// The unavailable entry's pill is suffixed so it stays
			// distinguishable from a same-named available candidate
			expect(screen.getByText("Old Model (unavailable)")).toBeInTheDocument();
		});

		it("keeps same-named stale and available pills distinguishable", () => {
			// The group references an old model whose display name collides with
			// an available candidate (the rename scenario): only the stale one
			// gets the unavailable suffix.
			const groupWithRenamedEntry: FailoverGroup = {
				...mockEditGroup,
				entries: [
					...mockEditGroup.entries,
					{
						model_uuid: "uuid-renamed-away",
						model_id: "gemma3:4b-old",
						provider_id: "provider-005",
						provider_name: "Renamed Provider",
						display_name: "Gemma 3 4B",
						enabled: true,
						model_enabled: false,
						provider_enabled: true,
						context_length: 8192,
						owned_by: "google",
					},
				],
			};

			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={groupWithRenamedEntry}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			// Available candidate keeps its plain label; stale entry is suffixed.
			expect(screen.getByText("Gemma 3 4B")).toBeInTheDocument();
			expect(screen.getByText("Gemma 3 4B (unavailable)")).toBeInTheDocument();
		});

		it("sends empty description when cleared in edit mode", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			// Clear the description field
			const descInput = screen.getByLabelText("Description (optional)");
			await user.clear(descInput);
			expect(descInput).toHaveValue("");

			// Submit
			await user.click(screen.getByRole("button", { name: "Save Changes" }));

			await waitFor(() => {
				expect(mockOnUpdated).toHaveBeenCalled();
			});
		});

		it("sends empty display_name when cleared in edit mode", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			// Clear the display name field
			const nameInput = screen.getByLabelText("Display Name (optional)");
			await user.clear(nameInput);
			expect(nameInput).toHaveValue("");

			// Submit - should send empty display_name (not omit it)
			await user.click(screen.getByRole("button", { name: "Save Changes" }));

			await waitFor(() => {
				expect(mockOnUpdated).toHaveBeenCalled();
			});
		});

		it("shows model_id as pill label for group entry with no display_name", () => {
			const groupNoDisplayEntry: FailoverGroup = {
				...mockEditGroup,
				entries: [
					{
						model_uuid: "uuid-unavailable-1",
						model_id: "old-model-no-display",
						provider_id: "p-old-1",
						provider_name: "Old Provider No Display",
						display_name: "",
						enabled: true,
						model_enabled: true,
						provider_enabled: true,
						context_length: 4096,
						owned_by: "old",
					},
					{
						model_uuid: "uuid-2",
						model_id: "gemma3:4b",
						provider_id: "provider-002",
						provider_name: "NanoGPT",
						display_name: "Gemma 3",
						enabled: true,
						model_enabled: true,
						provider_enabled: true,
						context_length: 8192,
						owned_by: "google",
					},
				],
			};

			renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={groupNoDisplayEntry}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			// The unavailable entry with empty display_name should show model_id
			// (with the unavailable suffix, since it is absent from candidates)
			expect(
				screen.getByText("old-model-no-display (unavailable)"),
			).toBeInTheDocument();
		});

		it("updates display_model when changed in edit mode", async () => {
			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			// Change the display_model field
			const displayModelInput = screen.getByLabelText("Display Model Name");
			await user.clear(displayModelInput);
			await user.type(displayModelInput, "new-model-name");

			// Submit
			await user.click(screen.getByRole("button", { name: "Save Changes" }));

			await waitFor(() => {
				expect(mockOnUpdated).toHaveBeenCalled();
			});
		});
	});

	describe("error handling", () => {
		it("shows error toast on failed creation", async () => {
			server.use(
				http.post("/api/failover-groups", () =>
					HttpResponse.json({ error: "Create failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			// Fill display model name
			const displayModelInput = screen.getByLabelText("Display Model Name");
			await user.type(displayModelInput, "glm-5");

			// Select 2 models
			await user.click(screen.getByText("Gemma 3 4B"));
			await user.click(screen.getByText("Gemma 3"));

			// Submit
			const submitButton = screen.getByRole("button", {
				name: "Create Group",
			});
			await user.click(submitButton);

			// Error toast should appear
			await waitFor(() => {
				expect(screen.getByText(/Failed to create group/)).toBeInTheDocument();
			});

			expect(mockOnCreated).not.toHaveBeenCalled();
		});

		it("shows error toast on failed update", async () => {
			server.use(
				http.put("/api/failover-groups/:id", () =>
					HttpResponse.json({ error: "Update failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			// Submit
			const submitButton = screen.getByRole("button", {
				name: "Save Changes",
			});
			await user.click(submitButton);

			// Error toast should appear
			await waitFor(() => {
				expect(screen.getByText(/Failed to update group/)).toBeInTheDocument();
			});

			expect(mockOnUpdated).not.toHaveBeenCalled();
		});

		it("shows create collision toast on 409 with already exists", async () => {
			server.use(
				http.post("/api/failover-groups", () =>
					HttpResponse.json(
						{ error: "A failover group already exists" },
						{ status: 409 },
					),
				),
			);

			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			// Fill display model name
			const displayModelInput = screen.getByLabelText("Display Model Name");
			await user.type(displayModelInput, "glm-5");

			// Select 2 models
			await user.click(screen.getByText("Gemma 3 4B"));
			await user.click(screen.getByText("Gemma 3"));

			// Submit
			const submitButton = screen.getByRole("button", {
				name: "Create Group",
			});
			await user.click(submitButton);

			// Collision toast should appear with model name interpolated
			await waitFor(() => {
				expect(
					screen.getByText(/A failover group for 'glm-5' already exists/),
				).toBeInTheDocument();
			});

			expect(mockOnCreated).not.toHaveBeenCalled();
		});

		it("shows generic error toast on non-409 creation failure", async () => {
			server.use(
				http.post("/api/failover-groups", () =>
					HttpResponse.json({ error: "Internal error" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					onClose={mockOnClose}
					onCreated={mockOnCreated}
				/>,
			);

			const displayModelInput = screen.getByLabelText("Display Model Name");
			await user.type(displayModelInput, "glm-5");

			await user.click(screen.getByText("Gemma 3 4B"));
			await user.click(screen.getByText("Gemma 3"));

			await user.click(screen.getByRole("button", { name: "Create Group" }));

			await waitFor(() => {
				expect(screen.getByText(/Failed to create group/)).toBeInTheDocument();
			});

			expect(mockOnCreated).not.toHaveBeenCalled();
		});

		it("shows update collision toast on 409 with already exists", async () => {
			server.use(
				http.put("/api/failover-groups/:id", () =>
					HttpResponse.json(
						{ error: "A failover group already exists" },
						{ status: 409 },
					),
				),
			);

			const { user } = renderWithProviders(
				<CreateGroupModal
					candidates={mockCandidates}
					group={mockEditGroup}
					onClose={mockOnClose}
					onUpdated={mockOnUpdated}
				/>,
			);

			// Submit
			const submitButton = screen.getByRole("button", {
				name: "Save Changes",
			});
			await user.click(submitButton);

			// Collision toast should appear
			await waitFor(() => {
				expect(
					screen.getByText(/A failover group with this name already exists/),
				).toBeInTheDocument();
			});

			expect(mockOnUpdated).not.toHaveBeenCalled();
		});
	});
});
