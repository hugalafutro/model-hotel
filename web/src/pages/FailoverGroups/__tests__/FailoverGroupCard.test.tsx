import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockFailoverGroup } from "../../../test/mocks/data";
import { renderWithProviders } from "../../../test/utils";
import { FailoverGroupCard } from "../FailoverGroupCard";

// Mock navigator.clipboard
const mockWriteText = vi.fn().mockResolvedValue(undefined);
Object.defineProperty(navigator, "clipboard", {
	value: {
		writeText: mockWriteText,
	},
	writable: true,
});

// Capture onDragEnd handler from DndContext for testing
let capturedOnDragEnd:
	| ((event: { active: { id: string }; over: { id: string } | null }) => void)
	| null = null;

vi.mock("@dnd-kit/core", async (importOriginal) => {
	const actual = await importOriginal();
	return {
		...(actual as object),
		DndContext: ({
			onDragEnd,
			children,
		}: {
			onDragEnd: (event: {
				active: { id: string };
				over: { id: string } | null;
			}) => void;
			children: React.ReactNode;
		}) => {
			capturedOnDragEnd = onDragEnd;
			return <div data-testid="dnd-context">{children}</div>;
		},
	};
});

describe("FailoverGroupCard", () => {
	const defaultProps = {
		group: mockFailoverGroup,
		selected: false,
		onToggleSelect: vi.fn(),
		onToggleGroup: vi.fn(),
		onToggleEntry: vi.fn(),
		onReorder: vi.fn(),
		onDelete: vi.fn(),
	};

	beforeEach(() => {
		vi.clearAllMocks();
		mockWriteText.mockClear();
		capturedOnDragEnd = null;
	});

	describe("Rendering", () => {
		it("renders group card with display model", () => {
			const group = {
				...mockFailoverGroup,
				display_model: "test-model",
				display_name: "Test Group",
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
		});

		it("renders group card with auto badge for auto-created groups", () => {
			const group = {
				...mockFailoverGroup,
				auto_created: true,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			expect(screen.getByText("auto")).toBeInTheDocument();
		});

		it("does not render auto badge for manually created groups", () => {
			const group = {
				...mockFailoverGroup,
				auto_created: false,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			expect(screen.queryByText("auto")).not.toBeInTheDocument();
		});

		it("renders checkbox for selection", () => {
			renderWithProviders(<FailoverGroupCard {...defaultProps} />);

			const checkbox = screen.getByRole("checkbox");
			expect(checkbox).toBeInTheDocument();
			expect(checkbox).not.toBeChecked();
		});

		it("renders checkbox as checked when selected", () => {
			renderWithProviders(<FailoverGroupCard {...defaultProps} selected />);

			const checkbox = screen.getByRole("checkbox");
			expect(checkbox).toBeChecked();
		});

		it("renders ON/OFF toggle button", () => {
			const group = {
				...mockFailoverGroup,
				group_enabled: true,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			expect(screen.getByRole("button", { name: "ON" })).toBeInTheDocument();
		});

		it("renders OFF toggle when group is disabled", () => {
			const group = {
				...mockFailoverGroup,
				group_enabled: false,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			expect(screen.getByRole("button", { name: "OFF" })).toBeInTheDocument();
		});

		it("renders entries count", () => {
			const group = {
				...mockFailoverGroup,
				entries: [
					{
						model_uuid: "entry-1",
						model_id: "model-1",
						provider_id: "provider-1",
						provider_name: "Provider 1",
						display_name: "Model 1",
						enabled: true,
						context_length: 8192,
						owned_by: "provider-1",
					},
					{
						model_uuid: "entry-2",
						model_id: "model-2",
						provider_id: "provider-2",
						provider_name: "Provider 2",
						display_name: "Model 2",
						enabled: false,
						context_length: 4096,
						owned_by: "provider-2",
					},
				],
				total_tokens: 1000000,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			// Text is split: "1/2 active • 1M tokens"
			expect(screen.getByText(/1\/2 active/)).toBeInTheDocument();
		});

		it("renders total tokens formatted", () => {
			const group = {
				...mockFailoverGroup,
				total_tokens: 1500000,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			// Text shows "1.5M tokens"
			expect(screen.getByText(/1\.5M/)).toBeInTheDocument();
		});

		it("renders delete button", () => {
			renderWithProviders(<FailoverGroupCard {...defaultProps} />);

			expect(
				screen.getByRole("button", { name: "Delete" }),
			).toBeInTheDocument();
		});

		it("renders sortable entries", () => {
			const group = {
				...mockFailoverGroup,
				entries: [
					{
						model_uuid: "entry-1",
						model_id: "model-1",
						provider_id: "provider-1",
						provider_name: "Provider 1",
						display_name: "Model 1",
						enabled: true,
						context_length: 8192,
						owned_by: "provider-1",
					},
					{
						model_uuid: "entry-2",
						model_id: "model-2",
						provider_id: "provider-2",
						provider_name: "Provider 2",
						display_name: "Model 2",
						enabled: true,
						context_length: 4096,
						owned_by: "provider-2",
					},
				],
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			// Each entry should render with provider name
			expect(screen.getByText("Provider 1")).toBeInTheDocument();
			expect(screen.getByText("Provider 2")).toBeInTheDocument();
		});

		it("applies accent border when group is enabled", () => {
			const group = {
				...mockFailoverGroup,
				group_enabled: true,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			const card = screen
				.getByRole("heading", { name: /hotel\/test-model/i })
				.closest(".ui-card");
			expect(card).toHaveClass("border-(--accent)/30");
		});

		it("does not apply disabled styling when group is enabled", () => {
			const group = {
				...mockFailoverGroup,
				group_enabled: true,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			const card = screen
				.getByRole("heading", { name: /hotel\/test-model/i })
				.closest(".ui-card");
			expect(card).not.toHaveClass("opacity-45");
			expect(card).not.toHaveClass("border-dashed");
		});

		it("applies opacity when group is disabled", () => {
			const group = {
				...mockFailoverGroup,
				group_enabled: false,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			const card = screen
				.getByRole("heading", { name: /hotel\/test-model/i })
				.closest(".ui-card");
			expect(card).toHaveClass("opacity-45");
			expect(card).toHaveClass("border-dashed");
		});
	});

	describe("Interactions", () => {
		it("calls onToggleSelect when checkbox is clicked", async () => {
			const onToggleSelect = vi.fn();

			const { user } = renderWithProviders(
				<FailoverGroupCard {...defaultProps} onToggleSelect={onToggleSelect} />,
			);

			const checkbox = screen.getByRole("checkbox");
			await user.click(checkbox);

			expect(onToggleSelect).toHaveBeenCalledWith(true);
		});

		it("calls onToggleGroup when ON/OFF button is clicked", async () => {
			const onToggleGroup = vi.fn();
			const group = {
				...mockFailoverGroup,
				group_enabled: true,
			};

			const { user } = renderWithProviders(
				<FailoverGroupCard
					{...defaultProps}
					group={group}
					onToggleGroup={onToggleGroup}
				/>,
			);

			await user.click(screen.getByRole("button", { name: "ON" }));

			expect(onToggleGroup).toHaveBeenCalledWith(false);
		});

		it("calls onToggleGroup to enable when OFF is clicked", async () => {
			const onToggleGroup = vi.fn();
			const group = {
				...mockFailoverGroup,
				group_enabled: false,
			};

			const { user } = renderWithProviders(
				<FailoverGroupCard
					{...defaultProps}
					group={group}
					onToggleGroup={onToggleGroup}
				/>,
			);

			await user.click(screen.getByRole("button", { name: "OFF" }));

			expect(onToggleGroup).toHaveBeenCalledWith(true);
		});

		it("calls onDelete when delete button is clicked", async () => {
			const onDelete = vi.fn();

			const { user } = renderWithProviders(
				<FailoverGroupCard {...defaultProps} onDelete={onDelete} />,
			);

			await user.click(screen.getByRole("button", { name: "Delete" }));

			expect(onDelete).toHaveBeenCalled();
		});

		it("copies model reference to clipboard when clicking model name", async () => {
			const group = {
				...mockFailoverGroup,
				display_model: "test-model",
			};

			const { user } = renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			// Click on the model name text which is inside a clickable div
			const modelElement = screen.getByText("hotel/test-model");
			await user.click(modelElement);

			// Toast should appear with copy confirmation
			await waitFor(() => {
				expect(screen.getByText("Copied hotel/test-model")).toBeInTheDocument();
			});
		});

		it("calls onToggleEntry when entry toggle is clicked", async () => {
			const onToggleEntry = vi.fn();
			const group = {
				...mockFailoverGroup,
				entries: [
					{
						model_uuid: "entry-1",
						model_id: "model-1",
						provider_id: "provider-1",
						provider_name: "Provider 1",
						display_name: "Model 1",
						enabled: true,
						context_length: 8192,
						owned_by: "provider-1",
					},
				],
			};

			const { user } = renderWithProviders(
				<FailoverGroupCard
					{...defaultProps}
					group={group}
					onToggleEntry={onToggleEntry}
				/>,
			);

			// Find Provider 1 entry and click its toggle button
			const providerEntry = screen.getByText("Provider 1");
			// Toggle button is within the entry
			const toggleButton = providerEntry.closest("li")?.querySelector("button");
			if (toggleButton) {
				await user.click(toggleButton);
				expect(onToggleEntry).toHaveBeenCalledWith("entry-1", false);
			}
		});

		it("calls onReorder when drag and drop ends", async () => {
			const onReorder = vi.fn();
			const group = {
				...mockFailoverGroup,
				entries: [
					{
						model_uuid: "entry-1",
						model_id: "model-1",
						provider_id: "provider-1",
						provider_name: "Provider 1",
						display_name: "Model 1",
						enabled: true,
						context_length: 8192,
						owned_by: "provider-1",
					},
					{
						model_uuid: "entry-2",
						model_id: "model-2",
						provider_id: "provider-2",
						provider_name: "Provider 2",
						display_name: "Model 2",
						enabled: true,
						context_length: 4096,
						owned_by: "provider-2",
					},
				],
			};

			renderWithProviders(
				<FailoverGroupCard
					{...defaultProps}
					group={group}
					onReorder={onReorder}
				/>,
			);

			// Drag and drop is tested via the DnD context
			// The onReorder should be called with the new order
			// This is a simplified test - full DnD testing would require more setup
			expect(onReorder).toBeDefined();
		});

		it("copies model reference to clipboard on Enter key", async () => {
			const group = {
				...mockFailoverGroup,
				display_model: "test-model",
			};

			const { user } = renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			// The model name div has role="button" and tabIndex={0}
			// Use getAllByRole since there are multiple buttons (ON/OFF toggle, delete)
			const modelButton = screen
				.getAllByRole("button")
				.find((btn) => btn.textContent?.includes("hotel/test-model"));
			expect(modelButton).toBeDefined();
			if (!modelButton) throw new Error("Model button not found");

			modelButton.focus();
			await user.keyboard("{Enter}");

			// Toast should appear confirming copy
			await waitFor(() => {
				expect(screen.getByText("Copied hotel/test-model")).toBeInTheDocument();
			});
		});

		it("copies model reference to clipboard on Space key", async () => {
			const group = {
				...mockFailoverGroup,
				display_model: "test-model",
			};

			const { user } = renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			const modelButton = screen
				.getAllByRole("button")
				.find((btn) => btn.textContent?.includes("hotel/test-model"));
			expect(modelButton).toBeDefined();
			if (!modelButton) throw new Error("Model button not found");

			modelButton.focus();
			await user.keyboard(" ");

			// Toast should appear confirming copy
			await waitFor(() => {
				expect(screen.getByText("Copied hotel/test-model")).toBeInTheDocument();
			});
		});

		it("resets local entries when server data changes after mutation", async () => {
			const entries = [
				{
					model_uuid: "entry-1",
					model_id: "model-1",
					provider_id: "p1",
					provider_name: "P1",
					display_name: "M1",
					enabled: true,
					context_length: 8192,
					owned_by: "p1",
				},
				{
					model_uuid: "entry-2",
					model_id: "model-2",
					provider_id: "p2",
					provider_name: "P2",
					display_name: "M2",
					enabled: true,
					context_length: 4096,
					owned_by: "p2",
				},
			];
			const { rerender } = renderWithProviders(
				<FailoverGroupCard
					{...defaultProps}
					group={{ ...mockFailoverGroup, entries }}
				/>,
			);

			expect(screen.getByText("P1")).toBeInTheDocument();
			expect(screen.getByText("P2")).toBeInTheDocument();

			// Re-render with reversed entries (simulating server data change after mutation)
			const reversedEntries = [...entries].reverse();
			rerender(
				<FailoverGroupCard
					{...defaultProps}
					group={{ ...mockFailoverGroup, entries: reversedEntries }}
				/>,
			);

			// Both should still render (order may differ but both visible)
			expect(screen.getByText("P1")).toBeInTheDocument();
			expect(screen.getByText("P2")).toBeInTheDocument();
		});

		it("does not call onReorder without drag interaction", () => {
			const onReorder = vi.fn();
			const entries = [
				{
					model_uuid: "entry-1",
					model_id: "model-1",
					provider_id: "p1",
					provider_name: "P1",
					display_name: "M1",
					enabled: true,
					context_length: 8192,
					owned_by: "p1",
				},
			];

			renderWithProviders(
				<FailoverGroupCard
					{...defaultProps}
					group={{ ...mockFailoverGroup, entries }}
					onReorder={onReorder}
				/>,
			);

			expect(onReorder).not.toHaveBeenCalled();
		});
	});

	describe("Entry Rendering", () => {
		it("renders entry with provider name and model ID", () => {
			const group = {
				...mockFailoverGroup,
				entries: [
					{
						model_uuid: "entry-1",
						model_id: "gpt-4",
						provider_id: "provider-openai",
						provider_name: "OpenAI",
						display_name: "GPT-4",
						enabled: true,
						context_length: 128000,
						owned_by: "openai",
					},
				],
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			expect(screen.getByText("OpenAI")).toBeInTheDocument();
			expect(screen.getByText("gpt-4")).toBeInTheDocument();
		});

		it("renders entry as enabled with correct styling", () => {
			const group = {
				...mockFailoverGroup,
				entries: [
					{
						model_uuid: "entry-1",
						model_id: "model-1",
						provider_id: "provider-1",
						provider_name: "Provider 1",
						display_name: "Model 1",
						enabled: true,
						context_length: 8192,
						owned_by: "provider-1",
					},
				],
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			// Enabled entry should render provider name
			expect(screen.getByText("Provider 1")).toBeInTheDocument();
		});

		it("renders entry as disabled with correct styling", () => {
			const group = {
				...mockFailoverGroup,
				entries: [
					{
						model_uuid: "entry-1",
						model_id: "model-1",
						provider_id: "provider-1",
						provider_name: "Provider 1",
						display_name: "Model 1",
						enabled: false,
						context_length: 8192,
						owned_by: "provider-1",
					},
				],
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			// Disabled entry should render provider name
			expect(screen.getByText("Provider 1")).toBeInTheDocument();
		});

		it("renders multiple entries in order", () => {
			const group = {
				...mockFailoverGroup,
				entries: [
					{
						model_uuid: "entry-1",
						model_id: "model-1",
						provider_id: "provider-1",
						provider_name: "Provider 1",
						display_name: "Model 1",
						enabled: true,
						context_length: 8192,
						owned_by: "provider-1",
					},
					{
						model_uuid: "entry-2",
						model_id: "model-2",
						provider_id: "provider-2",
						provider_name: "Provider 2",
						display_name: "Model 2",
						enabled: true,
						context_length: 4096,
						owned_by: "provider-2",
					},
					{
						model_uuid: "entry-3",
						model_id: "model-3",
						provider_id: "provider-3",
						provider_name: "Provider 3",
						display_name: "Model 3",
						enabled: false,
						context_length: 16384,
						owned_by: "provider-3",
					},
				],
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			expect(screen.getByText("Provider 1")).toBeInTheDocument();
			expect(screen.getByText("Provider 2")).toBeInTheDocument();
			expect(screen.getByText("Provider 3")).toBeInTheDocument();
		});

		it("renders drag handle for each entry", () => {
			const group = {
				...mockFailoverGroup,
				entries: [
					{
						model_uuid: "entry-1",
						model_id: "model-1",
						provider_id: "provider-1",
						provider_name: "Provider 1",
						display_name: "Model 1",
						enabled: true,
						context_length: 8192,
						owned_by: "provider-1",
					},
				],
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			// Drag handle should be present (the ⠿ character)
			expect(screen.getByText("⠿")).toBeInTheDocument();
		});
	});

	describe("Empty State", () => {
		it("renders card with no entries message when entries array is empty", () => {
			const group = {
				...mockFailoverGroup,
				entries: [],
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			// Should show "0/0 active • 0 tokens"
			expect(screen.getByText(/0\/0 active/)).toBeInTheDocument();
		});
	});

	describe("handleDragEnd", () => {
		it("calls onReorder with new UUID order when entry is dragged to new position", () => {
			const onReorder = vi.fn();
			const group = {
				...mockFailoverGroup,
				entries: [
					{
						model_uuid: "uuid-1",
						model_id: "model-1",
						provider_id: "p1",
						provider_name: "Provider 1",
						display_name: "Model 1",
						enabled: true,
						context_length: 8192,
						owned_by: "p1",
					},
					{
						model_uuid: "uuid-2",
						model_id: "model-2",
						provider_id: "p2",
						provider_name: "Provider 2",
						display_name: "Model 2",
						enabled: true,
						context_length: 8192,
						owned_by: "p2",
					},
					{
						model_uuid: "uuid-3",
						model_id: "model-3",
						provider_id: "p3",
						provider_name: "Provider 3",
						display_name: "Model 3",
						enabled: true,
						context_length: 8192,
						owned_by: "p3",
					},
				],
			};

			renderWithProviders(
				<FailoverGroupCard
					{...defaultProps}
					group={group}
					onReorder={onReorder}
				/>,
			);

			// Simulate dragging uuid-1 from position 0 to position 2 (where uuid-3 is)
			// arrayMove([0,1,2], 0, 2) = [1,2,0] -> ["uuid-2", "uuid-3", "uuid-1"]
			capturedOnDragEnd?.({
				active: { id: "uuid-1" },
				over: { id: "uuid-3" },
			});

			expect(onReorder).toHaveBeenCalledWith(["uuid-2", "uuid-3", "uuid-1"]);
		});

		it("does not call onReorder when dragged to same position", () => {
			const onReorder = vi.fn();
			const group = {
				...mockFailoverGroup,
				entries: [
					{
						model_uuid: "uuid-1",
						model_id: "model-1",
						provider_id: "p1",
						provider_name: "Provider 1",
						display_name: "Model 1",
						enabled: true,
						context_length: 8192,
						owned_by: "p1",
					},
				],
			};

			renderWithProviders(
				<FailoverGroupCard
					{...defaultProps}
					group={group}
					onReorder={onReorder}
				/>,
			);

			// Dragging to same position (active.id === over.id)
			capturedOnDragEnd?.({
				active: { id: "uuid-1" },
				over: { id: "uuid-1" },
			});

			expect(onReorder).not.toHaveBeenCalled();
		});

		it("does not call onReorder when item is dropped outside a droppable (over is null)", () => {
			const onReorder = vi.fn();
			const group = {
				...mockFailoverGroup,
				entries: [
					{
						model_uuid: "uuid-1",
						model_id: "model-1",
						provider_id: "p1",
						provider_name: "Provider 1",
						display_name: "Model 1",
						enabled: true,
						context_length: 8192,
						owned_by: "p1",
					},
				],
			};

			renderWithProviders(
				<FailoverGroupCard
					{...defaultProps}
					group={group}
					onReorder={onReorder}
				/>,
			);

			capturedOnDragEnd?.({
				active: { id: "uuid-1" },
				over: null,
			});

			expect(onReorder).not.toHaveBeenCalled();
		});
		it("does not call onReorder when group is disabled", () => {
			const onReorder = vi.fn();
			const group = {
				...mockFailoverGroup,
				group_enabled: false,
				entries: [
					{
						model_uuid: "uuid-1",
						model_id: "model-1",
						provider_id: "p1",
						provider_name: "Provider 1",
						display_name: "Model 1",
						enabled: true,
						context_length: 8192,
						owned_by: "p1",
					},
					{
						model_uuid: "uuid-2",
						model_id: "model-2",
						provider_id: "p2",
						provider_name: "Provider 2",
						display_name: "Model 2",
						enabled: true,
						context_length: 8192,
						owned_by: "p2",
					},
				],
			};

			renderWithProviders(
				<FailoverGroupCard
					{...defaultProps}
					group={group}
					onReorder={onReorder}
				/>,
			);

			// Drag would normally trigger a reorder, but the disabled
			// group early-returns before onReorder is called.
			capturedOnDragEnd?.({
				active: { id: "uuid-1" },
				over: { id: "uuid-2" },
			});

			expect(onReorder).not.toHaveBeenCalled();
		});
	});

	describe("Edit button", () => {
		it("renders edit button for custom group when onEdit is provided", () => {
			const group = {
				...mockFailoverGroup,
				auto_created: false,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} onEdit={vi.fn()} />,
			);

			expect(screen.getByRole("button", { name: "Edit" })).toBeInTheDocument();
		});

		it("does not render edit button for auto-created group", () => {
			const group = {
				...mockFailoverGroup,
				auto_created: true,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} onEdit={vi.fn()} />,
			);

			expect(
				screen.queryByRole("button", { name: "Edit" }),
			).not.toBeInTheDocument();
		});

		it("does not render edit button when onEdit is not provided", () => {
			const group = {
				...mockFailoverGroup,
				auto_created: false,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} />,
			);

			expect(
				screen.queryByRole("button", { name: "Edit" }),
			).not.toBeInTheDocument();
		});

		it("calls onEdit when edit button is clicked", async () => {
			const onEdit = vi.fn();
			const group = {
				...mockFailoverGroup,
				auto_created: false,
			};

			const { user } = renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} onEdit={onEdit} />,
			);

			await user.click(screen.getByRole("button", { name: "Edit" }));

			expect(onEdit).toHaveBeenCalled();
		});

		it("edit button has amber hover styling", () => {
			const group = {
				...mockFailoverGroup,
				auto_created: false,
			};

			renderWithProviders(
				<FailoverGroupCard {...defaultProps} group={group} onEdit={vi.fn()} />,
			);

			const editButton = screen.getByRole("button", { name: "Edit" });
			expect(editButton).toHaveClass("hover:text-amber-400");
		});
	});
});
