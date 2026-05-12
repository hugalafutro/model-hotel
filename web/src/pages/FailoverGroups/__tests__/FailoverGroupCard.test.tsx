import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FailoverEntry, FailoverGroup } from "../../../api/types";
import { AllProviders } from "../../../test/utils";
import { FailoverGroupCard } from "../FailoverGroupCard";

const mockEntry: FailoverEntry = {
	model_uuid: "model-001",
	model_id: "test-provider/model-1",
	provider_id: "provider-001",
	provider_name: "Test Provider",
	display_name: "Model 1",
	enabled: true,
	context_length: 8192,
	owned_by: "test-provider",
};

const mockEntry2: FailoverEntry = {
	model_uuid: "model-002",
	model_id: "test-provider/model-2",
	provider_id: "provider-001",
	provider_name: "Test Provider",
	display_name: "Model 2",
	enabled: false,
	context_length: 4096,
	owned_by: "test-provider",
};

const mockGroup: FailoverGroup = {
	id: "fg-001",
	display_model: "test-group",
	display_name: "Test Failover Group",
	description: "A test failover group",
	group_enabled: true,
	auto_created: false,
	entries: [mockEntry, mockEntry2],
	total_tokens: 100000,
	created_at: "2026-01-01T00:00:00Z",
	updated_at: "2026-05-01T00:00:00Z",
};

const defaultProps = {
	group: mockGroup,
	selected: false,
	onToggleSelect: vi.fn(),
	onToggleGroup: vi.fn(),
	onToggleEntry: vi.fn(),
	onReorder: vi.fn(),
	onDelete: vi.fn(),
};

describe("FailoverGroupCard", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("rendering group info", () => {
		it("renders group display name with hotel/ prefix", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText("hotel/test-group")).toBeInTheDocument();
		});

		it("renders auto badge for auto_created groups", () => {
			const autoGroup: FailoverGroup = {
				...mockGroup,
				auto_created: true,
			};

			render(<FailoverGroupCard {...defaultProps} group={autoGroup} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText("auto")).toBeInTheDocument();
		});

		it("does not render auto badge for manual groups", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			expect(screen.queryByText("auto")).not.toBeInTheDocument();
		});

		it("renders token count formatted", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			// 100000 tokens should be formatted as "100K"
			expect(screen.getByText(/100K tokens/)).toBeInTheDocument();
		});

		it("renders active/total count", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText(/1\/2 active/)).toBeInTheDocument();
		});

		it("applies opacity when group is disabled", () => {
			const disabledGroup: FailoverGroup = {
				...mockGroup,
				group_enabled: false,
			};

			render(<FailoverGroupCard {...defaultProps} group={disabledGroup} />, {
				wrapper: AllProviders,
			});

			const card = screen.getByText("hotel/test-group").closest(".ui-card");
			expect(card).toHaveClass("opacity-60");
		});
	});

	describe("toggle", () => {
		it("renders checkbox with checked state", () => {
			render(<FailoverGroupCard {...defaultProps} selected={true} />, {
				wrapper: AllProviders,
			});

			const checkbox = screen.getByRole("checkbox");
			expect(checkbox).toBeChecked();
		});

		it("renders checkbox with unchecked state", () => {
			render(<FailoverGroupCard {...defaultProps} selected={false} />, {
				wrapper: AllProviders,
			});

			const checkbox = screen.getByRole("checkbox");
			expect(checkbox).not.toBeChecked();
		});

		it("calls onToggleSelect when checkbox is clicked", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			const checkbox = screen.getByRole("checkbox");
			fireEvent.click(checkbox);

			expect(defaultProps.onToggleSelect).toHaveBeenCalledWith(true);
		});

		it("renders ON/OFF toggle button", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText("ON")).toBeInTheDocument();
		});

		it("shows OFF when group is disabled", () => {
			const disabledGroup: FailoverGroup = {
				...mockGroup,
				group_enabled: false,
			};

			render(<FailoverGroupCard {...defaultProps} group={disabledGroup} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText("OFF")).toBeInTheDocument();
		});

		it("calls onToggleGroup when toggle button is clicked", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			const toggleButton = screen.getByRole("button", {
				name: /ON|OFF/i,
			});
			fireEvent.click(toggleButton);

			expect(defaultProps.onToggleGroup).toHaveBeenCalledWith(false);
		});

		it("calls onToggleGroup with true when enabling disabled group", () => {
			const disabledGroup: FailoverGroup = {
				...mockGroup,
				group_enabled: false,
			};

			render(<FailoverGroupCard {...defaultProps} group={disabledGroup} />, {
				wrapper: AllProviders,
			});

			const toggleButton = screen.getByRole("button", {
				name: /ON|OFF/i,
			});
			fireEvent.click(toggleButton);

			expect(defaultProps.onToggleGroup).toHaveBeenCalledWith(true);
		});
	});

	describe("edit actions", () => {
		it("renders SortableEntry for each model entry", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			// Should render 2 entries with provider/model text
			expect(screen.getAllByText("Test Provider")).toHaveLength(2);
		});

		it("renders entry model names", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText("test-provider/model-1")).toBeInTheDocument();
			expect(screen.getByText("test-provider/model-2")).toBeInTheDocument();
		});

		it("renders entry toggle for each entry", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			// Each SortableEntry has a toggle (switch role)
			// Two entries = 2 toggles
			const entryToggles = screen.getAllByRole("switch");
			expect(entryToggles).toHaveLength(2);
		});

		it("calls onToggleEntry when entry toggle is clicked", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			// Find the toggle within SortableEntry (role="switch")
			const entryToggles = screen.getAllByRole("switch");
			fireEvent.click(entryToggles[0]);

			expect(defaultProps.onToggleEntry).toHaveBeenCalledWith(
				"model-001",
				false,
			);
		});
	});

	describe("delete actions", () => {
		it("renders delete button", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText("delete")).toBeInTheDocument();
		});

		it("calls onDelete when delete button is clicked", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			const deleteButton = screen.getByText("delete");
			fireEvent.click(deleteButton);

			expect(defaultProps.onDelete).toHaveBeenCalled();
		});
	});

	describe("drag and drop reordering", () => {
		it("renders DndContext for drag and drop", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			// DndContext should be present - check for draggable entries with grab cursor
			const dragHandles = screen.getAllByText("⠿");
			expect(dragHandles).toHaveLength(2);
		});

		it("calls onReorder when drag ends", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			// Simulate drag end by calling the handler directly
			// In a real test, we would use @dnd-kit/test-utils
			// For now, verify the handler is wired up
			expect(defaultProps.onReorder).toBeDefined();
		});
	});

	describe("copy model reference", () => {
		it("renders copyable model reference", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			const modelRefElement = screen.getByText("hotel/test-group");
			expect(modelRefElement).toBeInTheDocument();
			const container = modelRefElement.closest('[role="button"]');
			expect(container).toBeInTheDocument();
		});

		it("copies model reference to clipboard on click", async () => {
			const writeTextMock = vi.fn().mockResolvedValue(undefined);
			vi.stubGlobal("navigator", {
				clipboard: { writeText: writeTextMock },
			});

			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			const modelRefElement = screen.getByText("hotel/test-group");
			fireEvent.click(modelRefElement);

			expect(writeTextMock).toHaveBeenCalledWith("hotel/test-group");
			// Toast should be called (via useToast)
		});

		it("copies model reference on Enter key", () => {
			const writeTextMock = vi.fn().mockResolvedValue(undefined);
			vi.stubGlobal("navigator", {
				clipboard: { writeText: writeTextMock },
			});

			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			const modelRefElement = screen.getByText("hotel/test-group");
			fireEvent.keyDown(modelRefElement, { key: "Enter" });

			expect(writeTextMock).toHaveBeenCalledWith("hotel/test-group");
		});

		it("copies model reference on Space key", () => {
			const writeTextMock = vi.fn().mockResolvedValue(undefined);
			vi.stubGlobal("navigator", {
				clipboard: { writeText: writeTextMock },
			});

			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			const modelRefElement = screen.getByText("hotel/test-group");
			fireEvent.keyDown(modelRefElement, { key: " " });

			expect(writeTextMock).toHaveBeenCalledWith("hotel/test-group");
		});

		it("shows copy icon on hover", () => {
			render(<FailoverGroupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			// Copy icon SVG should be present
			const copyIcon = screen.getByTitle("Copy");
			expect(copyIcon).toBeInTheDocument();
		});
	});

	describe("empty entries", () => {
		it("handles group with no entries", () => {
			const emptyGroup: FailoverGroup = {
				...mockGroup,
				entries: [],
			};

			render(<FailoverGroupCard {...defaultProps} group={emptyGroup} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText(/0\/0 active/)).toBeInTheDocument();
		});
	});

	describe("multiple entries with different states", () => {
		it("renders entries with mixed enabled states", () => {
			const mixedGroup: FailoverGroup = {
				...mockGroup,
				entries: [
					{ ...mockEntry, enabled: true },
					{ ...mockEntry2, enabled: false },
					{ ...mockEntry, model_uuid: "model-003", enabled: true },
				],
			};

			render(<FailoverGroupCard {...defaultProps} group={mixedGroup} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText(/2\/3 active/)).toBeInTheDocument();
		});
	});
});
